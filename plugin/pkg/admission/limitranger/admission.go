/*
Copyright 2014 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package limitranger

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/admission"
	genericadmissioninitailizer "k8s.io/apiserver/pkg/admission/initializer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/utils/lru"
)

const (
	limitRangerAnnotation = "kubernetes.io/limit-ranger"
	// PluginName indicates name of admission plugin.
	PluginName = "LimitRanger"
)

// Register registers a plugin
func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {
		return NewLimitRanger(&DefaultLimitRangerActions{})
	})
}

// LimitRanger enforces usage limits on a per resource basis in the namespace
type LimitRanger struct {
	*admission.Handler
	client  kubernetes.Interface
	actions LimitRangerActions
	lister  corev1listers.LimitRangeLister

	// liveLookups holds the last few live lookups we've done to help ammortize cost on repeated lookup failures.
	// This let's us handle the case of latent caches, by looking up actual results for a namespace on cache miss/no results.
	// We track the lookup result here so that for repeated requests, we don't look it up very often.
	liveLookupCache *lru.Cache
	group           singleflight.Group
	liveTTL         time.Duration
}

var _ admission.MutationInterface = &LimitRanger{}
var _ admission.ValidationInterface = &LimitRanger{}

var _ genericadmissioninitailizer.WantsExternalKubeInformerFactory = &LimitRanger{}
var _ genericadmissioninitailizer.WantsExternalKubeClientSet = &LimitRanger{}

type liveLookupEntry struct {
	expiry time.Time
	items  []*corev1.LimitRange
}

// SetExternalKubeInformerFactory registers an informer factory into the LimitRanger
func (l *LimitRanger) SetExternalKubeInformerFactory(f informers.SharedInformerFactory) {
	limitRangeInformer := f.Core().V1().LimitRanges()
	l.SetReadyFunc(limitRangeInformer.Informer().HasSynced)
	l.lister = limitRangeInformer.Lister()
}

// SetExternalKubeLister registers a limit range lister into the LimitRanger
func (l *LimitRanger) SetExternalKubeLister(lister corev1listers.LimitRangeLister) {
	l.lister = lister
}

// SetExternalKubeClientSet registers the client into LimitRanger
func (l *LimitRanger) SetExternalKubeClientSet(client kubernetes.Interface) {
	l.client = client
}

// ValidateInitialization verifies the LimitRanger object has been properly initialized
func (l *LimitRanger) ValidateInitialization() error {
	if l.lister == nil {
		return fmt.Errorf("missing limitRange lister")
	}
	if l.client == nil {
		return fmt.Errorf("missing client")
	}
	return nil
}

// Admit admits resources into cluster that do not violate any defined LimitRange in the namespace
func (l *LimitRanger) Admit(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) (err error) {
	return l.runLimitFunc(a, l.actions.MutateLimit)
}

// Validate admits resources into cluster that do not violate any defined LimitRange in the namespace
func (l *LimitRanger) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) (err error) {
	return l.runLimitFunc(a, l.actions.ValidateLimit)
}

func (l *LimitRanger) runLimitFunc(a admission.Attributes, limitFn func(limitRange *corev1.LimitRange, kind string, obj runtime.Object) error) (err error) {
	if !l.actions.SupportsAttributes(a) {
		return nil
	}

	// ignore all objects marked for deletion
	oldObj := a.GetOldObject()
	if oldObj != nil {
		oldAccessor, err := meta.Accessor(oldObj)
		if err != nil {
			return admission.NewForbidden(a, err)
		}
		if oldAccessor.GetDeletionTimestamp() != nil {
			return nil
		}
	}

	items, err := l.GetLimitRanges(a)
	if err != nil {
		return err
	}

	// ensure it meets each prescribed min/max
	for i := range items {
		limitRange := items[i]

		if !l.actions.SupportsLimit(limitRange) {
			continue
		}

		err = limitFn(limitRange, a.GetResource().Resource, a.GetObject())
		if err != nil {
			return admission.NewForbidden(a, err)
		}
	}
	return nil
}

// GetLimitRanges returns a LimitRange object with the items held in
// the indexer if available, or do alive lookup of the value.
func (l *LimitRanger) GetLimitRanges(a admission.Attributes) ([]*corev1.LimitRange, error) {
	items, err := l.lister.LimitRanges(a.GetNamespace()).List(labels.Everything())
	if err != nil {
		return nil, admission.NewForbidden(a, fmt.Errorf("unable to %s %v at this time because there was an error enforcing limit ranges", a.GetOperation(), a.GetResource()))
	}

	// if there are no items held in our indexer, check our live-lookup LRU, if that misses, do the live lookup to prime it.
	if len(items) == 0 {
		lruItemObj, ok := l.liveLookupCache.Get(a.GetNamespace())
		if !ok || lruItemObj.(liveLookupEntry).expiry.Before(time.Now()) {
			// Fixed: #22422
			// use singleflight to alleviate simultaneous calls to
			lruItemObj, err, _ = l.group.Do(a.GetNamespace(), func() (interface{}, error) {
				liveList, err := l.client.CoreV1().LimitRanges(a.GetNamespace()).List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return nil, admission.NewForbidden(a, err)
				}
				newEntry := liveLookupEntry{expiry: time.Now().Add(l.liveTTL)}
				for i := range liveList.Items {
					newEntry.items = append(newEntry.items, &liveList.Items[i])
				}
				l.liveLookupCache.Add(a.GetNamespace(), newEntry)
				return newEntry, nil
			})
			if err != nil {
				return nil, err
			}
		}
		lruEntry := lruItemObj.(liveLookupEntry)

		for i := range lruEntry.items {
			items = append(items, lruEntry.items[i])
		}

	}

	return items, nil
}

// NewLimitRanger returns an object that enforces limits based on the supplied limit function
func NewLimitRanger(actions LimitRangerActions) (*LimitRanger, error) {
	liveLookupCache := lru.New(10000)

	if actions == nil {
		actions = &DefaultLimitRangerActions{}
	}

	return &LimitRanger{
		Handler:         admission.NewHandler(admission.Create, admission.Update),
		actions:         actions,
		liveLookupCache: liveLookupCache,
		liveTTL:         time.Duration(30 * time.Second),
	}, nil
}

// defaultContainerResourceRequirements returns the default requirements for a container
// the requirement.Limits are taken from the LimitRange defaults (if specified)
// the requirement.Requests are taken from the LimitRange default request (if specified)
func defaultContainerResourceRequirements(limitRange *corev1.LimitRange) api.ResourceRequirements {
	requirements := api.ResourceRequirements{}
	requirements.Requests = api.ResourceList{}
	requirements.Limits = api.ResourceList{}

	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		if limit.Type == corev1.LimitTypeContainer {
			for k, v := range limit.DefaultRequest {
				requirements.Requests[api.ResourceName(k)] = v.DeepCopy()
			}
			for k, v := range limit.Default {
				requirements.Limits[api.ResourceName(k)] = v.DeepCopy()
			}
		}
	}
	return requirements
}

// mergeContainerResources handles defaulting all of the resources on a container.
func mergeContainerResources(container *api.Container, defaultRequirements *api.ResourceRequirements, annotationPrefix string, annotations []string) []string {
	setRequests := []string{}
	setLimits := []string{}
	if container.Resources.Limits == nil {
		container.Resources.Limits = api.ResourceList{}
	}
	if container.Resources.Requests == nil {
		container.Resources.Requests = api.ResourceList{}
	}
	for k, v := range defaultRequirements.Limits {
		_, found := container.Resources.Limits[k]
		if !found {
			container.Resources.Limits[k] = v.DeepCopy()
			setLimits = append(setLimits, string(k))
		}
	}
	for k, v := range defaultRequirements.Requests {
		_, found := container.Resources.Requests[k]
		if !found {
			container.Resources.Requests[k] = v.DeepCopy()
			setRequests = append(setRequests, string(k))
		}
	}
	if len(setRequests) > 0 {
		sort.Strings(setRequests)
		a := strings.Join(setRequests, ", ") + fmt.Sprintf(" request for %s %s", annotationPrefix, container.Name)
		annotations = append(annotations, a)
	}
	if len(setLimits) > 0 {
		sort.Strings(setLimits)
		a := strings.Join(setLimits, ", ") + fmt.Sprintf(" limit for %s %s", annotationPrefix, container.Name)
		annotations = append(annotations, a)
	}
	return annotations
}

// mergePodResourceRequirements merges enumerated requirements with default requirements
// it annotates the pod with information about what requirements were modified
func mergePodResourceRequirements(pod *api.Pod, defaultRequirements *api.ResourceRequirements) {
	annotations := []string{}

	for i := range pod.Spec.Containers {
		annotations = mergeContainerResources(&pod.Spec.Containers[i], defaultRequirements, "container", annotations)
	}

	for i := range pod.Spec.InitContainers {
		annotations = mergeContainerResources(&pod.Spec.InitContainers[i], defaultRequirements, "init container", annotations)
	}

	if len(annotations) > 0 {
		if pod.ObjectMeta.Annotations == nil {
			pod.ObjectMeta.Annotations = make(map[string]string)
		}
		val := "LimitRanger plugin set: " + strings.Join(annotations, "; ")
		pod.ObjectMeta.Annotations[limitRangerAnnotation] = val
	}
}

// requestLimitEnforcedValues returns the specified values at a common precision to support comparability
func requestLimitEnforcedValues(requestQuantity, limitQuantity, enforcedQuantity resource.Quantity) (request, limit, enforced int64) {
	request = requestQuantity.Value()
	limit = limitQuantity.Value()
	enforced = enforcedQuantity.Value()
	// do a more precise comparison if possible (if the value won't overflow)
	if request <= resource.MaxMilliValue && limit <= resource.MaxMilliValue && enforced <= resource.MaxMilliValue {
		request = requestQuantity.MilliValue()
		limit = limitQuantity.MilliValue()
		enforced = enforcedQuantity.MilliValue()
	}
	return
}

// minConstraint enforces the min constraint over the specified resource
func minConstraint(limitType string, resourceName string, enforced resource.Quantity, request api.ResourceList, limit api.ResourceList) error {
	req, reqExists := request[api.ResourceName(resourceName)]
	lim, limExists := limit[api.ResourceName(resourceName)]
	observedReqValue, observedLimValue, enforcedValue := requestLimitEnforcedValues(req, lim, enforced)

	if !reqExists {
		return fmt.Errorf("minimum %s usage per %s is %s.  No request is specified", resourceName, limitType, enforced.String())
	}
	if observedReqValue < enforcedValue {
		return fmt.Errorf("minimum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	if limExists && (observedLimValue < enforcedValue) {
		return fmt.Errorf("minimum %s usage per %s is %s, but limit is %s", resourceName, limitType, enforced.String(), lim.String())
	}
	return nil
}

// maxRequestConstraint enforces the max constraint over the specified resource
// use when specify LimitType resource doesn't recognize limit values
func maxRequestConstraint(limitType string, resourceName string, enforced resource.Quantity, request api.ResourceList) error {
	req, reqExists := request[api.ResourceName(resourceName)]
	observedReqValue, _, enforcedValue := requestLimitEnforcedValues(req, resource.Quantity{}, enforced)

	if !reqExists {
		return fmt.Errorf("maximum %s usage per %s is %s.  No request is specified", resourceName, limitType, enforced.String())
	}
	if observedReqValue > enforcedValue {
		return fmt.Errorf("maximum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	return nil
}

// maxConstraint enforces the max constraint over the specified resource
func maxConstraint(limitType string, resourceName string, enforced resource.Quantity, request api.ResourceList, limit api.ResourceList) error {
	req, reqExists := request[api.ResourceName(resourceName)]
	lim, limExists := limit[api.ResourceName(resourceName)]
	observedReqValue, observedLimValue, enforcedValue := requestLimitEnforcedValues(req, lim, enforced)

	if !limExists {
		return fmt.Errorf("maximum %s usage per %s is %s.  No limit is specified", resourceName, limitType, enforced.String())
	}
	if observedLimValue > enforcedValue {
		return fmt.Errorf("maximum %s usage per %s is %s, but limit is %s", resourceName, limitType, enforced.String(), lim.String())
	}
	if reqExists && (observedReqValue > enforcedValue) {
		return fmt.Errorf("maximum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	return nil
}

// limitRequestRatioConstraint enforces the limit to request ratio over the specified resource
func limitRequestRatioConstraint(limitType string, resourceName string, enforced resource.Quantity, request api.ResourceList, limit api.ResourceList) error {
	req, reqExists := request[api.ResourceName(resourceName)]
	lim, limExists := limit[api.ResourceName(resourceName)]
	observedReqValue, observedLimValue, _ := requestLimitEnforcedValues(req, lim, enforced)

	if !reqExists || (observedReqValue == int64(0)) {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but no request is specified or request is 0", resourceName, limitType, enforced.String())
	}
	if !limExists || (observedLimValue == int64(0)) {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but no limit is specified or limit is 0", resourceName, limitType, enforced.String())
	}

	observedRatio := float64(observedLimValue) / float64(observedReqValue)
	displayObservedRatio := observedRatio
	maxLimitRequestRatio := float64(enforced.Value())
	if enforced.Value() <= resource.MaxMilliValue {
		observedRatio = observedRatio * 1000
		maxLimitRequestRatio = float64(enforced.MilliValue())
	}

	if observedRatio > maxLimitRequestRatio {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but provided ratio is %f", resourceName, limitType, enforced.String(), displayObservedRatio)
	}

	return nil
}

// sum takes the total of each named resource across all inputs
// if a key is not in each input, then the output resource list will omit the key
func sum(inputs []api.ResourceList) api.ResourceList {
	result := api.ResourceList{}
	keys := []api.ResourceName{}
	for i := range inputs {
		for k := range inputs[i] {
			keys = append(keys, k)
		}
	}
	for _, key := range keys {
		total, isSet := int64(0), true

		for i := range inputs {
			input := inputs[i]
			v, exists := input[key]
			if exists {
				if key == api.ResourceCPU {
					total = total + v.MilliValue()
				} else {
					total = total + v.Value()
				}
			} else {
				isSet = false
			}
		}

		if isSet {
			if key == api.ResourceCPU {
				result[key] = *(resource.NewMilliQuantity(total, resource.DecimalSI))
			} else {
				result[key] = *(resource.NewQuantity(total, resource.DecimalSI))
			}

		}
	}
	return result
}

// DefaultLimitRangerActions is the default implementation of LimitRangerActions.
type DefaultLimitRangerActions struct{}

// ensure DefaultLimitRangerActions implements the LimitRangerActions interface.
var _ LimitRangerActions = &DefaultLimitRangerActions{}

// MutateLimit enforces resource requirements of incoming resources
// against enumerated constraints on the LimitRange.  It may modify
// the incoming object to apply default resource requirements if not
// specified, and enumerated on the LimitRange
func (d *DefaultLimitRangerActions) MutateLimit(limitRange *corev1.LimitRange, resourceName string, obj runtime.Object) error {
	switch resourceName {
	case "pods":
		return PodMutateLimitFunc(limitRange, obj.(*api.Pod))
	}
	return nil
}

// ValidateLimit verifies the resource requirements of incoming
// resources against enumerated constraints on the LimitRange are
// valid
func (d *DefaultLimitRangerActions) ValidateLimit(limitRange *corev1.LimitRange, resourceName string, obj runtime.Object) error {
	switch resourceName {
	case "pods":
		return PodValidateLimitFunc(limitRange, obj.(*api.Pod))
	case "persistentvolumeclaims":
		return PersistentVolumeClaimValidateLimitFunc(limitRange, obj.(*api.PersistentVolumeClaim))
	}
	return nil
}

// SupportsAttributes ignores all calls that do not deal with pod resources or storage requests (PVCs).
// Also ignores any call that has a subresource defined.
func (d *DefaultLimitRangerActions) SupportsAttributes(a admission.Attributes) bool {
	if a.GetSubresource() != "" {
		return false
	}

	// Since containers and initContainers cannot currently be added, removed, or updated, it is unnecessary
	// to mutate and validate limitrange on pod updates. Trying to mutate containers or initContainers on a pod
	// update request will always fail pod validation because those fields are immutable once the object is created.
	if a.GetKind().GroupKind() == api.Kind("Pod") && a.GetOperation() == admission.Update {
		return false
	}

	return a.GetKind().GroupKind() == api.Kind("Pod") || a.GetKind().GroupKind() == api.Kind("PersistentVolumeClaim")
}

// SupportsLimit always returns true.
func (d *DefaultLimitRangerActions) SupportsLimit(limitRange *corev1.LimitRange) bool {
	return true
}

// PersistentVolumeClaimValidateLimitFunc enforces storage limits for PVCs.
// Users request storage via pvc.Spec.Resources.Requests.  Min/Max is enforced by an admin with LimitRange.
// Claims will not be modified with default values because storage is a required part of pvc.Spec.
// All storage enforced values *only* apply to pvc.Spec.Resources.Requests.
func PersistentVolumeClaimValidateLimitFunc(limitRange *corev1.LimitRange, pvc *api.PersistentVolumeClaim) error {
	var errs []error
	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		limitType := limit.Type
		if limitType == corev1.LimitTypePersistentVolumeClaim {
			for k, v := range limit.Min {
				// normal usage of minConstraint. pvc.Spec.Resources.Limits is not recognized as user input
				if err := minConstraint(string(limitType), string(k), v, pvc.Spec.Resources.Requests, api.ResourceList{}); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.Max {
				// We want to enforce the max of the LimitRange against what
				// the user requested.
				if err := maxRequestConstraint(string(limitType), string(k), v, pvc.Spec.Resources.Requests); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

// PodMutateLimitFunc sets resource requirements enumerated by the pod against
// the specified LimitRange.  The pod may be modified to apply default resource
// requirements if not specified, and enumerated on the LimitRange
func PodMutateLimitFunc(limitRange *corev1.LimitRange, pod *api.Pod) error {
	defaultResources := defaultContainerResourceRequirements(limitRange)
	mergePodResourceRequirements(pod, &defaultResources)
	return nil
}

// PodValidateLimitFunc enforces resource requirements enumerated by the pod against
// the specified LimitRange.
func PodValidateLimitFunc(limitRange *corev1.LimitRange, pod *api.Pod) error {
	var errs []error

	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		limitType := limit.Type
		// enforce container limits
		if limitType == corev1.LimitTypeContainer {
			for j := range pod.Spec.Containers {
				container := &pod.Spec.Containers[j]
				for k, v := range limit.Min {
					if err := minConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.Max {
					if err := maxConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.MaxLimitRequestRatio {
					if err := limitRequestRatioConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
			}
			for j := range pod.Spec.InitContainers {
				container := &pod.Spec.InitContainers[j]
				for k, v := range limit.Min {
					if err := minConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.Max {
					if err := maxConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.MaxLimitRequestRatio {
					if err := limitRequestRatioConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}

		// enforce pod limits on init containers
		if limitType == corev1.LimitTypePod {
			containerRequests, containerLimits := []api.ResourceList{}, []api.ResourceList{}
			for j := range pod.Spec.Containers {
				container := &pod.Spec.Containers[j]
				containerRequests = append(containerRequests, container.Resources.Requests)
				containerLimits = append(containerLimits, container.Resources.Limits)
			}
			podRequests := sum(containerRequests)
			podLimits := sum(containerLimits)
			for j := range pod.Spec.InitContainers {
				container := &pod.Spec.InitContainers[j]
				// take max(sum_containers, any_init_container)
				for k, v := range container.Resources.Requests {
					if v2, ok := podRequests[k]; ok {
						if v.Cmp(v2) > 0 {
							podRequests[k] = v
						}
					} else {
						podRequests[k] = v
					}
				}
				for k, v := range container.Resources.Limits {
					if v2, ok := podLimits[k]; ok {
						if v.Cmp(v2) > 0 {
							podLimits[k] = v
						}
					} else {
						podLimits[k] = v
					}
				}
			}
			for k, v := range limit.Min {
				if err := minConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.Max {
				if err := maxConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.MaxLimitRequestRatio {
				if err := limitRequestRatioConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}
