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
	"fmt"
	"math"
	"sync/atomic"
	"time"

	compbasemetrics "k8s.io/component-base/metrics"
)

// TimingRatioHistogram is our internal representation for our wrapping struct around timing
// histograms. It implements both kubeCollector and GaugeMetric
type TimingRatioHistogram struct {
	compbasemetrics.Registerable
	TimingRatioHistogramInner
}

// TimingRatioHistogramInner is the aspect of instrumentation
type TimingRatioHistogramInner struct {
	under           settable
	denominatorBits uint64 // math.Float64bits(the denominator), accessed via sync/atomic
}

type settable interface {
	Set(float64)
}

var _ RatioedObserver = &TimingRatioHistogram{}
var _ compbasemetrics.Registerable = &TimingRatioHistogram{}

// NewTimingHistogram returns an object which is TimingHistogram-like. However, nothing
// will be measured until the histogram is registered somewhere.
func NewTimingRatioHistogram(initialDenominator float64, opts *compbasemetrics.TimingHistogramOpts) *TimingRatioHistogram {
	return NewTestableTimingRatioHistogram(time.Now, initialDenominator, opts)
}

// NewTestableTimingHistogram adds injection of the clock
func NewTestableTimingRatioHistogram(nowFunc func() time.Time, initialDenominator float64, opts *compbasemetrics.TimingHistogramOpts) *TimingRatioHistogram {
	ratioedOpts := *opts
	ratioedOpts.InitialValue /= initialDenominator
	th := compbasemetrics.NewTestableTimingHistogram(nowFunc, &ratioedOpts)
	return &TimingRatioHistogram{
		Registerable: th,
		TimingRatioHistogramInner: TimingRatioHistogramInner{
			under:           th,
			denominatorBits: math.Float64bits(initialDenominator),
		}}
}

func (trh *TimingRatioHistogramInner) Observe(numerator float64) {
	denominator := math.Float64frombits(atomic.LoadUint64(&trh.denominatorBits))
	trh.under.Set(numerator / denominator)
}

func (trh *TimingRatioHistogramInner) SetDenominator(denominator float64) {
	atomic.StoreUint64(&trh.denominatorBits, math.Float64bits(denominator))
}

// WithContext allows the normal TimingHistogram metric to pass in context. The context is no-op now.
func (trh *TimingRatioHistogramInner) WithContext(ctx context.Context) RatioedObserver {
	return trh
}

// TimingRatioHistogramVec is the internal representation of our wrapping struct around prometheus
// TimingHistogramVecs.
type TimingRatioHistogramVec struct {
	compbasemetrics.Registerable // promote only the Registerable methods
	under                        compbasemetrics.GaugeMetricVec
}

var _ RatioedObserverVec = &TimingRatioHistogramVec{}
var _ compbasemetrics.Registerable = &TimingRatioHistogramVec{}

// NewTimingHistogramVec returns an object which satisfies kubeCollector and wraps the
// promext.TimingHistogramVec object. Note well: before the vector is registered in
// at least one registry, calls on WithLabels, WithLabelValues, Delete, and Reset will
// not fail but will have no effect that appears in scrapes.
func NewTimingRatioHistogramVec(opts *compbasemetrics.TimingHistogramOpts, labels ...string) *TimingRatioHistogramVec {
	return NewTestableTimingRatioHistogramVec(time.Now, opts, labels...)
}

// NewTestableTimingHistogramVec adds injection of the clock.
func NewTestableTimingRatioHistogramVec(nowFunc func() time.Time, opts *compbasemetrics.TimingHistogramOpts, labels ...string) *TimingRatioHistogramVec {
	under := compbasemetrics.NewTestableTimingHistogramVec(nowFunc, opts, labels)
	return &TimingRatioHistogramVec{under, under}
}

func (v *TimingRatioHistogramVec) metrics() Registerables {
	return Registerables{v}
}

func (v *TimingRatioHistogramVec) WithLabelValues(initialDenominator float64, lvs ...string) (RatioedObserver, error) {
	elt, err := v.under.WithLabelValues(lvs...)
	if err != nil {
		return noopRatioed{}, err
	}
	switch typed := elt.(type) {
	case settable:
		return &TimingRatioHistogramInner{typed, math.Float64bits(initialDenominator)}, nil
	default:
		return noopRatioed{}, fmt.Errorf("got a %T", elt)
	}
}

func (v *TimingRatioHistogramVec) WithLabelValuesSafe(initialDenominator float64, lvs ...string) RatioedObserver {
	tro, err := v.WithLabelValues(initialDenominator, lvs...)
	if err == nil {
		return tro
	}
	return &timingRatioHistogramVecElt{
		vec:                v,
		initialDenominator: initialDenominator,
		labelValues:        lvs,
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

// With returns the ObserverMetric for the given Labels map (the label names
// must match those of the VariableLabels in Desc). If that label map is
// accessed for the first time, a new ObserverMetric is created IFF the HistogramVec has
// been registered to a metrics registry.
func (v *TimingRatioHistogramVec) With(initialDenominator float64, labels map[string]string) (RatioedObserver, error) {
	elt, err := v.under.With(labels)
	if err != nil {
		return noopRatioed{}, err
	}
	switch typed := elt.(type) {
	case settable:
		return &TimingRatioHistogramInner{typed, math.Float64bits(initialDenominator)}, nil
	default:
		return noopRatioed{}, fmt.Errorf("got a %T", elt)
	}
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
func NewTimingRatioHistogramPairVec(opts *compbasemetrics.TimingHistogramOpts, labelNames ...string) TimingRatioHistogramPairVec {
	return NewTestableTimingRatioHistogramPairVec(time.Now, opts, labelNames...)
}

// NewTimedRatioObserverPairVec makes a new pair generator
func NewTestableTimingRatioHistogramPairVec(nowFunc func() time.Time, opts *compbasemetrics.TimingHistogramOpts, labelNames ...string) TimingRatioHistogramPairVec {
	return TimingRatioHistogramPairVec{
		urVec: NewTestableTimingRatioHistogramVec(nowFunc, opts, append([]string{labelNamePhase}, labelNames...)...),
	}
}

// WithLabelValues makes a new pair
func (pv TimingRatioHistogramPairVec) WithLabelValues(initialWaitingDenominator, initialExecutingDenominator float64, labelValues ...string) (RatioedObserverPair, error) {
	RequestsWaiting, err := pv.urVec.WithLabelValues(initialWaitingDenominator, append([]string{labelValueWaiting}, labelValues...)...)
	if err != nil {
		return RatioedObserverPair{}, err
	}
	RequestsExecuting, err := pv.urVec.WithLabelValues(initialExecutingDenominator, append([]string{labelValueExecuting}, labelValues...)...)
	return RatioedObserverPair{RequestsWaiting: RequestsWaiting, RequestsExecuting: RequestsExecuting}, err
}

// WithLabelValues makes a new pair
func (pv TimingRatioHistogramPairVec) WithLabelValuesSafe(initialWaitingDenominator, initialExecutingDenominator float64, labelValues ...string) RatioedObserverPair {
	return RatioedObserverPair{
		RequestsWaiting:   pv.urVec.WithLabelValuesSafe(initialWaitingDenominator, append([]string{labelValueWaiting}, labelValues...)...),
		RequestsExecuting: pv.urVec.WithLabelValuesSafe(initialExecutingDenominator, append([]string{labelValueExecuting}, labelValues...)...),
	}
}

func (pv TimingRatioHistogramPairVec) metrics() Registerables {
	return pv.urVec.metrics()
}
