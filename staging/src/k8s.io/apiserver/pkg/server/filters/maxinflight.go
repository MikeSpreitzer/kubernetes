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

package filters

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/metrics"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	fq "k8s.io/apiserver/pkg/util/flowcontrol/fairqueuing"

	"k8s.io/klog/v2"
)

const (
	// Constant for the retry-after interval on rate limiting.
	// TODO: maybe make this dynamic? or user-adjustable?
	retryAfter = "1"

	// How often inflight usage metric should be updated. Because
	// the metrics tracks maximal value over period making this
	// longer will increase the metric value.
	inflightUsageMetricUpdatePeriod = time.Second
)

var nonMutatingRequestVerbs = sets.NewString("get", "list", "watch")

func handleError(w http.ResponseWriter, r *http.Request, err error) {
	errorMsg := fmt.Sprintf("Internal Server Error: %#v", r.RequestURI)
	http.Error(w, errorMsg, http.StatusInternalServerError)
	klog.Errorf(err.Error())
}

// requestWatermark is used to trak maximal usage of inflight requests.
type requestWatermark struct {
	readOnlyIntegrator, mutatingIntegrator fq.Integrator
}

func (w *requestWatermark) recordMutating(mutatingVal int) {
	w.mutatingIntegrator.Set(float64(mutatingVal))
}

func (w *requestWatermark) recordReadOnly(readOnlyVal int) {
	w.readOnlyIntegrator.Set(float64(readOnlyVal))
}

var watermark = &requestWatermark{
	readOnlyIntegrator: fq.NewIntegrator(clock.RealClock{}),
	mutatingIntegrator: fq.NewIntegrator(clock.RealClock{}),
}

func startRecordingUsage() {
	go func() {
		wait.Forever(func() {
			readOnlyResults := watermark.readOnlyIntegrator.Reset()
			mutatingResults := watermark.mutatingIntegrator.Reset()
			metrics.UpdateInflightRequestMetrics(convertWindowResults(readOnlyResults), convertWindowResults(mutatingResults))
		}, inflightUsageMetricUpdatePeriod)
	}()
}

func convertWindowResults(results fq.IntegratorResults) metrics.WindowStats {
	if results.Max < 0 {
		results.Min = 0
		results.Max = 0
	}
	return metrics.WindowStats{
		Min:               results.Min,
		Max:               results.Max,
		Average:           results.Average,
		StandardDeviation: results.Deviation,
	}
}

var startOnce sync.Once

// WithMaxInFlightLimit limits the number of in-flight requests to buffer size of the passed in channel.
func WithMaxInFlightLimit(
	handler http.Handler,
	nonMutatingLimit int,
	mutatingLimit int,
	longRunningRequestCheck apirequest.LongRunningRequestCheck,
) http.Handler {
	startOnce.Do(startRecordingUsage)
	if nonMutatingLimit == 0 && mutatingLimit == 0 {
		return handler
	}
	var nonMutatingChan chan bool
	var mutatingChan chan bool
	if nonMutatingLimit != 0 {
		nonMutatingChan = make(chan bool, nonMutatingLimit)
	}
	if mutatingLimit != 0 {
		mutatingChan = make(chan bool, mutatingLimit)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestInfo, ok := apirequest.RequestInfoFrom(ctx)
		if !ok {
			handleError(w, r, fmt.Errorf("no RequestInfo found in context, handler chain must be wrong"))
			return
		}

		// Skip tracking long running events.
		if longRunningRequestCheck != nil && longRunningRequestCheck(r, requestInfo) {
			handler.ServeHTTP(w, r)
			return
		}

		var c chan bool
		isMutatingRequest := !nonMutatingRequestVerbs.Has(requestInfo.Verb)
		if isMutatingRequest {
			c = mutatingChan
		} else {
			c = nonMutatingChan
		}

		if c == nil {
			handler.ServeHTTP(w, r)
		} else {

			select {
			case c <- true:
				defer func() {
					<-c
					if isMutatingRequest {
						watermark.recordMutating(len(c))
					} else {
						watermark.recordReadOnly(len(c))
					}
				}()
				if isMutatingRequest {
					watermark.recordMutating(len(c))
				} else {
					watermark.recordReadOnly(len(c))
				}
				handler.ServeHTTP(w, r)

			default:
				// at this point we're about to return a 429, BUT not all actors should be rate limited.  A system:master is so powerful
				// that they should always get an answer.  It's a super-admin or a loopback connection.
				if currUser, ok := apirequest.UserFrom(ctx); ok {
					for _, group := range currUser.GetGroups() {
						if group == user.SystemPrivilegedGroup {
							handler.ServeHTTP(w, r)
							return
						}
					}
				}
				// We need to split this data between buckets used for throttling.
				if isMutatingRequest {
					metrics.DroppedRequests.WithLabelValues(metrics.MutatingKind).Inc()
				} else {
					metrics.DroppedRequests.WithLabelValues(metrics.ReadOnlyKind).Inc()
				}
				metrics.RecordRequestTermination(r, requestInfo, metrics.APIServerComponent, http.StatusTooManyRequests)
				tooManyRequests(r, w)
			}
		}
	})
}

func tooManyRequests(req *http.Request, w http.ResponseWriter) {
	// Return a 429 status indicating "Too Many Requests"
	w.Header().Set("Retry-After", retryAfter)
	http.Error(w, "Too many requests, please try again later.", http.StatusTooManyRequests)
}
