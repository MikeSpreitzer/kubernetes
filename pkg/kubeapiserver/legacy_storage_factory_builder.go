/*
Copyright 2016 The Kubernetes Authors.

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

package kubeapiserver

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/server/resourceconfig"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/apis/apps"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/apis/events"
	"k8s.io/kubernetes/pkg/apis/extensions"
	"k8s.io/kubernetes/pkg/apis/networking"
	"k8s.io/kubernetes/pkg/apis/policy"
	apisstorage "k8s.io/kubernetes/pkg/apis/storage"
)

// SpecialDefaultResourcePrefixes are prefixes compiled into Kubernetes.
var SpecialDefaultResourcePrefixes = map[schema.GroupResource]string{
	{Group: "", Resource: "replicationcontrollers"}:        "controllers",
	{Group: "", Resource: "endpoints"}:                     "services/endpoints",
	{Group: "", Resource: "nodes"}:                         "minions",
	{Group: "", Resource: "services"}:                      "services/specs",
	{Group: "extensions", Resource: "ingresses"}:           "ingress",
	{Group: "networking.k8s.io", Resource: "ingresses"}:    "ingress",
	{Group: "extensions", Resource: "podsecuritypolicies"}: "podsecuritypolicy",
	{Group: "policy", Resource: "podsecuritypolicies"}:     "podsecuritypolicy",
}

// DefaultWatchCacheSizes defines default resources for which watchcache
// should be disabled.
func DefaultWatchCacheSizes() map[schema.GroupResource]int {
	return map[schema.GroupResource]int{
		{Resource: "events"}:                         0,
		{Group: "events.k8s.io", Resource: "events"}: 0,
	}
}

// LegacyStorageFactoryConfig returns a new StorageFactoryConfig set up with necessary resource overrides.
func LegacyStorageFactoryConfig() *StorageFactoryConfig {

	resources := []schema.GroupVersionResource{
		// TODO (https://github.com/kubernetes/kubernetes/issues/108451): remove the override in
		// 1.25.
		apisstorage.Resource("csistoragecapacities").WithVersion("v1beta1"),
	}

	return &StorageFactoryConfig{
		Serializer:                legacyscheme.Codecs,
		DefaultResourceEncoding:   serverstorage.NewDefaultResourceEncodingConfig(legacyscheme.Scheme),
		ResourceEncodingOverrides: resources,
	}
}

// New returns a new storage factory created from the completed storage factory configuration.
func (c *completedStorageFactoryConfig) Legacy() (*serverstorage.DefaultStorageFactory, error) {
	resourceEncodingConfig := resourceconfig.MergeResourceEncodingConfigs(c.DefaultResourceEncoding, c.ResourceEncodingOverrides)
	storageFactory := serverstorage.NewDefaultStorageFactory(
		c.StorageConfig,
		c.DefaultStorageMediaType,
		c.Serializer,
		resourceEncodingConfig,
		c.APIResourceConfig,
		SpecialDefaultResourcePrefixes)

	storageFactory.UseResourceAsPrefixDefault = true

	storageFactory.AddCohabitatingResources(networking.Resource("networkpolicies"), extensions.Resource("networkpolicies"))
	storageFactory.AddCohabitatingResources(apps.Resource("deployments"), extensions.Resource("deployments"))
	storageFactory.AddCohabitatingResources(apps.Resource("daemonsets"), extensions.Resource("daemonsets"))
	storageFactory.AddCohabitatingResources(apps.Resource("replicasets"), extensions.Resource("replicasets"))
	storageFactory.AddCohabitatingResources(api.Resource("events"), events.Resource("events"))
	storageFactory.AddCohabitatingResources(api.Resource("replicationcontrollers"), extensions.Resource("replicationcontrollers")) // to make scale subresources equivalent
	storageFactory.AddCohabitatingResources(policy.Resource("podsecuritypolicies"), extensions.Resource("podsecuritypolicies"))
	storageFactory.AddCohabitatingResources(networking.Resource("ingresses"), extensions.Resource("ingresses"))

	for _, override := range c.EtcdServersOverrides {
		tokens := strings.Split(override, "#")
		apiresource := strings.Split(tokens[0], "/")

		group := apiresource[0]
		resource := apiresource[1]
		groupResource := schema.GroupResource{Group: group, Resource: resource}

		servers := strings.Split(tokens[1], ";")
		storageFactory.SetEtcdLocation(groupResource, servers)
	}
	
	return storageFactory, nil
}
