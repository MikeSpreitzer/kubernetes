/*
Copyright 2019 The Kubernetes Authors.

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

package filters

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	fcv1a1 "k8s.io/api/flowcontrol/v1alpha1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	epmetrics "k8s.io/apiserver/pkg/endpoints/metrics"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	utilflowcontrol "k8s.io/apiserver/pkg/util/flowcontrol"
	fq "k8s.io/apiserver/pkg/util/flowcontrol/fairqueuing"
	metrics "k8s.io/apiserver/pkg/util/flowcontrol/metrics"
	"k8s.io/klog/v2"
)

type priorityAndFairnessKeyType int

const priorityAndFairnessKey priorityAndFairnessKeyType = iota

const (
	responseHeaderMatchedPriorityLevelConfigurationUID = "X-Kubernetes-PF-PriorityLevel-UID"
	responseHeaderMatchedFlowSchemaUID                 = "X-Kubernetes-PF-FlowSchema-UID"
)

// PriorityAndFairnessClassification identifies the results of
// classification for API Priority and Fairness
type PriorityAndFairnessClassification struct {
	FlowSchemaName    string
	FlowSchemaUID     apitypes.UID
	PriorityLevelName string
	PriorityLevelUID  apitypes.UID
}

// GetClassification returns the classification associated with the
// given context, if any, otherwise nil
func GetClassification(ctx context.Context) *PriorityAndFairnessClassification {
	return ctx.Value(priorityAndFairnessKey).(*PriorityAndFairnessClassification)
}

// waitingMark tracks requests waiting rather than being executed
var waitingMark = &requestWatermark{
	phase:              epmetrics.WaitingPhase,
	readOnlyIntegrator: fq.NewWindowedIntegrator(clock.RealClock{}, inflightUsageMetricUpdatePeriod, inflightMetricsWindows),
	mutatingIntegrator: fq.NewWindowedIntegrator(clock.RealClock{}, inflightUsageMetricUpdatePeriod, inflightMetricsWindows),
}

var atomicMutatingExecuting, atomicReadOnlyExecuting int32
var atomicMutatingWaiting, atomicReadOnlyWaiting int32
var startWindowedOnce sync.Once

// WithPriorityAndFairness limits the number of in-flight
// requests in a fine-grained way.
func WithPriorityAndFairness(
	handler http.Handler,
	longRunningRequestCheck apirequest.LongRunningRequestCheck,
	fcIfc utilflowcontrol.Interface,
) http.Handler {
	if fcIfc == nil {
		klog.Warningf("priority and fairness support not found, skipping")
		return handler
	}
	startOnce.Do(func() {
		startRecordingUsage(watermark)
		startRecordingUsage(waitingMark)
	})
	startWindowedOnce.Do(func() {
		go reportWindowedStats(fcIfc)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestInfo, ok := apirequest.RequestInfoFrom(ctx)
		if !ok {
			handleError(w, r, fmt.Errorf("no RequestInfo found in context"))
			return
		}
		user, ok := apirequest.UserFrom(ctx)
		if !ok {
			handleError(w, r, fmt.Errorf("no User found in context"))
			return
		}

		// Skip tracking long running requests.
		if longRunningRequestCheck != nil && longRunningRequestCheck(r, requestInfo) {
			klog.V(6).Infof("Serving RequestInfo=%#+v, user.Info=%#+v as longrunning\n", requestInfo, user)
			handler.ServeHTTP(w, r)
			return
		}

		var classification *PriorityAndFairnessClassification
		note := func(fs *fcv1a1.FlowSchema, pl *fcv1a1.PriorityLevelConfiguration) {
			classification = &PriorityAndFairnessClassification{
				FlowSchemaName:    fs.Name,
				FlowSchemaUID:     fs.UID,
				PriorityLevelName: pl.Name,
				PriorityLevelUID:  pl.UID}
		}

		var served bool
		isMutatingRequest := !nonMutatingRequestVerbs.Has(requestInfo.Verb)
		noteExecutingDelta := func(delta int32) {
			if isMutatingRequest {
				watermark.recordMutating(int(atomic.AddInt32(&atomicMutatingExecuting, delta)))
			} else {
				watermark.recordReadOnly(int(atomic.AddInt32(&atomicReadOnlyExecuting, delta)))
			}
		}
		noteWaitingDelta := func(delta int32) {
			if isMutatingRequest {
				waitingMark.recordMutating(int(atomic.AddInt32(&atomicMutatingWaiting, delta)))
			} else {
				waitingMark.recordReadOnly(int(atomic.AddInt32(&atomicReadOnlyWaiting, delta)))
			}
		}
		execute := func() {
			noteExecutingDelta(1)
			defer noteExecutingDelta(-1)
			served = true
			innerCtx := context.WithValue(ctx, priorityAndFairnessKey, classification)
			innerReq := r.Clone(innerCtx)
			w.Header().Set(responseHeaderMatchedPriorityLevelConfigurationUID, string(classification.PriorityLevelUID))
			w.Header().Set(responseHeaderMatchedFlowSchemaUID, string(classification.FlowSchemaUID))
			handler.ServeHTTP(w, innerReq)
		}
		digest := utilflowcontrol.RequestDigest{requestInfo, user}
		fcIfc.Handle(ctx, digest, note, func(inQueue bool) {
			if inQueue {
				noteWaitingDelta(1)
			} else {
				noteWaitingDelta(-1)
			}
		}, execute)
		if !served {
			tooManyRequests(r, w)
		}

	})
}

func reportWindowedStats(fcIfc utilflowcontrol.Interface) {
	ints := make(map[string]*fq.WindowedIntegratorPair)
	statmm := make(map[string]map[string]*metrics.WindowedIntegratorResultsStep)
	wait.Forever(func() {
		fcIfc.ExtractIntegrators(ints)
		for plName, ip := range ints {
			statm := statmm[plName]
			if statm == nil {
				statm = map[string]*metrics.WindowedIntegratorResultsStep{
					epmetrics.WaitingPhase:   {},
					epmetrics.ExecutingPhase: {},
				}
				statmm[plName] = statm
			}
			w := statm[epmetrics.WaitingPhase]
			w.Previous = w.Current
			w.Current = ip.RequestsWaiting.GetResults(w.Previous.Min, w.Previous.Max)
			e := statm[epmetrics.ExecutingPhase]
			e.Previous = e.Current
			e.Current = ip.RequestsExecuting.GetResults(e.Previous.Min, e.Previous.Max)
		}
		metrics.SetWindowedRequestStats(statmm)
	}, fcIfc.GetWindowWidth())
}
