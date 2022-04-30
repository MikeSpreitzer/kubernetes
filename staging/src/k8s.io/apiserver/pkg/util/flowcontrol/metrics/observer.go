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

package metrics

const (
	labelNamePhase      = "phase"
	labelValueWaiting   = "waiting"
	labelValueExecuting = "executing"
)

// Observer is something that can be given numeric observations.
type Observer interface {
	// Observe takes an observation
	Observe(float64)
}

// RatioedObserver tracks ratios.
// The numerator is set through the Observer methods,
// and the denominator can be updated through the SetDenominator method.
// A ratio is tracked whenever the numerator is set.
type RatioedObserver interface {
	Observer

	// SetDenominator sets the denominator to use until it is changed again
	SetDenominator(float64)
}

// RatioedbserverVec creates related observers that are
// differentiated by a series of label values.
type RatioedObserverVec interface {
	// WithLabelValues will return an error if this vec is not hidden and not yet registtered
	WithLabelValues(initialDenominator float64, labelValues ...string) (RatioedObserver, error)

	// WithLabelValuesSafe returns a RatioedObserver that, if not hidden, noop until registered
	// and always be relatively expensive to use.
	WithLabelValuesSafe(initialDenominator float64, labelValues ...string) RatioedObserver
}

// RatioedObserverPair is a corresponding pair of observers, one for the
// number of requests waiting in queue(s) and one for the number of
// requests being executed
type RatioedObserverPair struct {
	// RequestsWaiting is given observations of the number of currently queued requests
	RequestsWaiting RatioedObserver

	// RequestsExecuting is given observations of the number of requests currently executing
	RequestsExecuting RatioedObserver
}

// RatioedObserverPairVec generates pairs
type RatioedObserverPairVec interface {
	// WithLabelValues will return an error if this pair is not hidden and not yet registtered
	WithLabelValues(initialWaitingDenominator, initialExecutingDenominator float64, labelValues ...string) (RatioedObserverPair, error)

	// WithLabelValuesSafe returns a RatioedObserverPair that, if not hidden, noop until registered
	// and always be relatively expensive to use.
	WithLabelValuesSafe(initialWaitingDenominator, initialExecutingDenominator float64, labelValues ...string) RatioedObserverPair
}
