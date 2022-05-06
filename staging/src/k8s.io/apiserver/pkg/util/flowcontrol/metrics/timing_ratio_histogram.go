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

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	compbasemetrics "k8s.io/component-base/metrics"
)

// TimingRatioHistogram is essentially a gauge for a ratio where the client
// independently controls the numerator and denominator.
// When scraped it produces a histogram of samples of the ratio
// taken at the end of every nanosecond.
// `*TimingRatioHistogram` implements both Registerable and RatioedObserver.
type TimingRatioHistogram struct {
	// The implementation is layered on TimingHistogram,
	// adding the division by an occasionally adjusted denominator.

	// Registerable is the registerable aspect.
	// That is the registerable aspect of the underlying TimingHistogram.
	compbasemetrics.Registerable

	// timingRatioHistogramInner implements the RatioedObsserver aspect.
	timingRatioHistogramInner
}

// TimingRatioHistogramOpts is the constructor parameters of a TimingRatioHistogram.
// The `TimingHistogramOpts.InitialValue` is the initial numerator.
type TimingRatioHistogramOpts struct {
	compbasemetrics.TimingHistogramOpts
	InitialDenominator float64
}

// timingRatioHistogramInner implements the instrumentation aspect
type timingRatioHistogramInner struct {
	under           compbasemetrics.GaugeMetric
	denominatorBits uint64 // math.Float64bits(the denominator), accessed via sync/atomic
}

var _ RatioedObserver = &timingRatioHistogramInner{}
var _ RatioedObserver = &TimingRatioHistogram{}
var _ compbasemetrics.Registerable = &TimingRatioHistogram{}

// NewTimingHistogram returns an object which is TimingHistogram-like. However, nothing
// will be measured until the histogram is registered in at least one registry.
func NewTimingRatioHistogram(opts TimingRatioHistogramOpts) *TimingRatioHistogram {
	return NewTestableTimingRatioHistogram(time.Now, opts)
}

// NewTestableTimingHistogram adds injection of the clock
func NewTestableTimingRatioHistogram(nowFunc func() time.Time, opts TimingRatioHistogramOpts) *TimingRatioHistogram {
	ratioedOpts := opts.TimingHistogramOpts
	ratioedOpts.InitialValue /= opts.InitialDenominator
	th := compbasemetrics.NewTestableTimingHistogram(nowFunc, ratioedOpts)
	return &TimingRatioHistogram{
		Registerable: th,
		timingRatioHistogramInner: timingRatioHistogramInner{
			under:           th,
			denominatorBits: math.Float64bits(opts.InitialDenominator),
		}}
}

func (trh *timingRatioHistogramInner) Observe(numerator float64) {
	denominator := math.Float64frombits(atomic.LoadUint64(&trh.denominatorBits))
	trh.under.Set(numerator / denominator)
}

func (trh *timingRatioHistogramInner) SetDenominator(denominator float64) {
	atomic.StoreUint64(&trh.denominatorBits, math.Float64bits(denominator))
}

// WithContext allows the normal TimingHistogram metric to pass in context.
// The context is no-op at the current level of development.
func (trh *timingRatioHistogramInner) WithContext(ctx context.Context) RatioedObserver {
	return trh
}

// TimingRatioHistogramVec is a collection of TimingRatioHistograms that differ
// only in label values.
// `*TimingRatioHistogramVec` implements both Registerable and RatioedObserverVec.
type TimingRatioHistogramVec struct {
	compbasemetrics.Registerable                                // promote only the Registerable methods
	under                        compbasemetrics.GaugeMetricVec // TimingHistograms of the ratio
	initialNumerator             float64
}

var _ RatioedObserverVec = &TimingRatioHistogramVec{}
var _ compbasemetrics.Registerable = &TimingRatioHistogramVec{}

// NewTimingHistogramVec constructs a new vector.
// `opts.InitialValue` is the initial numerator.
// The initial denominator can be different for each member of the vector
// and is supplied when extracting a member.
// Thus there is a tiny splinter of time during member construction when
// its underlying TimingHistogram is given the initial numerator rather than
// the initial ratio (which is obviously a non-issue when both are zero).
// Note the difficulties associated with extracting a member
// before registering the vector.
func NewTimingRatioHistogramVec(opts compbasemetrics.TimingHistogramOpts, labelNames ...string) *TimingRatioHistogramVec {
	return NewTestableTimingRatioHistogramVec(time.Now, opts, labelNames...)
}

// NewTestableTimingHistogramVec adds injection of the clock.
func NewTestableTimingRatioHistogramVec(nowFunc func() time.Time, opts compbasemetrics.TimingHistogramOpts, labelNames ...string) *TimingRatioHistogramVec {
	under := compbasemetrics.NewTestableTimingHistogramVec(nowFunc, opts, labelNames)
	return &TimingRatioHistogramVec{
		Registerable:     under,
		under:            under,
		initialNumerator: opts.InitialValue,
	}
}

func (v *TimingRatioHistogramVec) metrics() Registerables {
	return Registerables{v}
}

// WithLabelValues, if called after this vector has been
// registered in at least one registry and this vector is not
// hidden, will return a RatioedObserver that is NOT a noop along
// with nil error.  If called on a hidden vector then it will
// return a noop and a nil error.  Otherwise it returns a noop
// and an error that passes compbasemetrics.ErrIsNotRegistered.
func (v *TimingRatioHistogramVec) WithLabelValues(initialDenominator float64, labelValues ...string) (RatioedObserver, error) {
	underMember, err := v.under.WithLabelValues(labelValues...)
	if err != nil {
		return noopRatioed{}, err
	}
	underMember.Set(v.initialNumerator / initialDenominator)
	return &timingRatioHistogramInner{
			under:           underMember,
			denominatorBits: math.Float64bits(initialDenominator)},
		nil
}

// WithLabelValuesSafe returns a RatioedObserver that, if not hidden, will noop until registered
// and always be relatively expensive to use.
func (v *TimingRatioHistogramVec) WithLabelValuesSafe(initialDenominator float64, labelValues ...string) RatioedObserver {
	tro, err := v.WithLabelValues(initialDenominator, labelValues...)
	if err == nil {
		return tro
	}
	return &timingRatioHistogramVecElt{
		vec:                v,
		initialDenominator: initialDenominator,
		labelValues:        labelValues,
	}
}

type timingRatioHistogramVecElt struct {
	vec                *TimingRatioHistogramVec
	initialDenominator float64
	labelValues        []string
}

var _ RatioedObserver = &timingRatioHistogramVecElt{}

func (tve *timingRatioHistogramVecElt) Observe(numerator float64) {
	tro, _ := tve.vec.WithLabelValues(tve.initialDenominator, tve.labelValues...)
	tro.Observe(numerator)
}

func (tve *timingRatioHistogramVecElt) SetDenominator(denominator float64) {
	tve.initialDenominator = denominator
	tro, _ := tve.vec.WithLabelValues(denominator, tve.labelValues...)
	tro.SetDenominator(denominator)
}

// With, if called after this vector has been
// registered in at least one registry and this vector is not
// hidden, will return a RatioedObserver that is NOT a noop along
// with nil error.  If called on a hidden vector then it will
// return a noop and a nil error.  Otherwise it returns a noop
// and an error that passes compbasemetrics.ErrIsNotRegistered.
func (v *TimingRatioHistogramVec) With(initialDenominator float64, labels map[string]string) (RatioedObserver, error) {
	underMember, err := v.under.With(labels)
	if err != nil {
		return noopRatioed{}, err
	}
	return &timingRatioHistogramInner{underMember, math.Float64bits(initialDenominator)}, nil
}

type noopRatioed struct{}

func (noopRatioed) Observe(float64)        {}
func (noopRatioed) SetDenominator(float64) {}

func (v *TimingRatioHistogramVec) Reset() {
	v.under.Reset()
}

// WithContext returns wrapped TimingHistogramVec with context
func (v *TimingRatioHistogramVec) WithContext(ctx context.Context) *TimingRatioHistogramVecWithContext {
	return &TimingRatioHistogramVecWithContext{
		ctx:                     ctx,
		TimingRatioHistogramVec: v,
	}
}

// TimingHistogramVecWithContext is the wrapper of TimingHistogramVec with context.
type TimingRatioHistogramVecWithContext struct {
	*TimingRatioHistogramVec
	ctx context.Context
}

type TimingRatioHistogramPairVec struct {
	urVec *TimingRatioHistogramVec
}

var _ RatioedObserverPairVec = TimingRatioHistogramPairVec{}

// NewTimedRatioObserverPairVec makes a new pair generator
func NewTimingRatioHistogramPairVec(opts compbasemetrics.TimingHistogramOpts, labelNames ...string) TimingRatioHistogramPairVec {
	return NewTestableTimingRatioHistogramPairVec(time.Now, opts, labelNames...)
}

// NewTimedRatioObserverPairVec makes a new pair generator
func NewTestableTimingRatioHistogramPairVec(nowFunc func() time.Time, opts compbasemetrics.TimingHistogramOpts, labelNames ...string) TimingRatioHistogramPairVec {
	return TimingRatioHistogramPairVec{
		urVec: NewTestableTimingRatioHistogramVec(nowFunc, opts, append([]string{labelNamePhase}, labelNames...)...),
	}
}

// WithLabelValues extracts a member if it is not broken
func (pv TimingRatioHistogramPairVec) WithLabelValues(initialWaitingDenominator, initialExecutingDenominator float64, labelValues ...string) (RatioedObserverPair, error) {
	RequestsWaiting, err := pv.urVec.WithLabelValues(initialWaitingDenominator, append([]string{labelValueWaiting}, labelValues...)...)
	if err != nil {
		return RatioedObserverPair{}, err
	}
	RequestsExecuting, err := pv.urVec.WithLabelValues(initialExecutingDenominator, append([]string{labelValueExecuting}, labelValues...)...)
	return RatioedObserverPair{RequestsWaiting: RequestsWaiting, RequestsExecuting: RequestsExecuting}, err
}

// WithLabelValuesSafe extracts a member that will always work right and be more expensive
func (pv TimingRatioHistogramPairVec) WithLabelValuesSafe(initialWaitingDenominator, initialExecutingDenominator float64, labelValues ...string) RatioedObserverPair {
	return RatioedObserverPair{
		RequestsWaiting:   pv.urVec.WithLabelValuesSafe(initialWaitingDenominator, append([]string{labelValueWaiting}, labelValues...)...),
		RequestsExecuting: pv.urVec.WithLabelValuesSafe(initialExecutingDenominator, append([]string{labelValueExecuting}, labelValues...)...),
	}
}

func (pv TimingRatioHistogramPairVec) metrics() Registerables {
	return pv.urVec.metrics()
}
