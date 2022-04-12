/*
Copyright 2022 The Kubernetes Authors.

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

package prometheusextension

import (
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"k8s.io/utils/clock"
)

// GaugeOps is the part of `prometheus.Gauge` that is relevant to
// instrumented code.
// This factoring should be in prometheus, analogous to the way
// it already factors out the Observer interface for histograms and summaries.
type GaugeOps interface {
	// Set is the same as Gauge.Set
	Set(float64)
	// Inc is the same as Gauge.inc
	Inc()
	// Dec is the same as Gauge.Dec
	Dec()
	// Add is the same as Gauge.Add
	Add(float64)
	// Sub is the same as Gauge.Sub
	Sub(float64)

	// SetToCurrentTime the same as Gauge.SetToCurrentTime
	SetToCurrentTime()
}

// A TimingHistogram tracks how long a `float64` variable spends in
// ranges defined by buckets.  Time is counted in nanoseconds.  The
// histogram's sum is the integral over time (in nanoseconds, from
// creation of the histogram) of the variable's value.
type TimingHistogram interface {
	prometheus.Metric
	prometheus.Collector
	GaugeOps
}

// TimingHistogramOpts is the parameters of the TimingHistogram constructor
type TimingHistogramOpts struct {
	Namespace   string
	Subsystem   string
	Name        string
	Help        string
	ConstLabels prometheus.Labels

	// Buckets defines the buckets into which observations are
	// accumulated. Each element in the slice is the upper
	// inclusive bound of a bucket. The values must be sorted in
	// strictly increasing order. There is no need to add a
	// highest bucket with +Inf bound. The default value is
	// prometheus.DefBuckets.
	Buckets []float64

	// The initial value of the variable.
	InitialValue float64
}

// NewTimingHistogram creates a new TimingHistogram
func NewTimingHistogram(opts TimingHistogramOpts) (TimingHistogram, error) {
	return NewTestableTimingHistogram(clock.RealClock{}, opts)
}

// NewTestableTimingHistogram creates a TimingHistogram that uses a mockable clock
func NewTestableTimingHistogram(clock clock.PassiveClock, opts TimingHistogramOpts) (TimingHistogram, error) {
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		nil,
		opts.ConstLabels,
	)
	return newTimingHistogram(clock, desc, opts)
}

func newTimingHistogram(clock clock.PassiveClock, desc *prometheus.Desc, opts TimingHistogramOpts, variableLabelValues ...string) (TimingHistogram, error) {
	allLabelsM := prometheus.Labels{}
	allLabelsS := prometheus.MakeLabelPairs(desc, variableLabelValues)
	for _, pair := range allLabelsS {
		if pair == nil || pair.Name == nil || pair.Value == nil {
			return nil, errors.New("prometheus.MakeLabelPairs returned a nil")
		}
		allLabelsM[*pair.Name] = *pair.Value
	}
	weighted, err := NewWeightedHistogram(WeightedHistogramOpts{
		Namespace:   opts.Namespace,
		Subsystem:   opts.Subsystem,
		Name:        opts.Name,
		Help:        opts.Help,
		ConstLabels: allLabelsM,
		Buckets:     opts.Buckets,
	})
	if err != nil {
		return nil, err
	}
	return &timingHistogram{
		clock:       clock,
		weighted:    weighted,
		lastSetTime: clock.Now(),
		value:       opts.InitialValue,
	}, nil
}

type timingHistogram struct {
	clock    clock.PassiveClock
	weighted WeightedHistogram

	lock sync.Mutex // applies to all the following

	// identifies when value was last set
	lastSetTime time.Time
	value       float64
}

var _ TimingHistogram = &timingHistogram{}

func (th *timingHistogram) Set(newValue float64) {
	th.update(func(float64) float64 { return newValue })
}

func (th *timingHistogram) Inc() {
	th.update(func(oldValue float64) float64 { return oldValue + 1 })
}

func (th *timingHistogram) Dec() {
	th.update(func(oldValue float64) float64 { return oldValue - 1 })
}

func (th *timingHistogram) Add(delta float64) {
	th.update(func(oldValue float64) float64 { return oldValue + delta })
}

func (th *timingHistogram) Sub(delta float64) {
	th.update(func(oldValue float64) float64 { return oldValue - delta })
}

func (th *timingHistogram) SetToCurrentTime() {
	th.update(func(oldValue float64) float64 { return th.clock.Since(time.Unix(0, 0)).Seconds() })
}

func (th *timingHistogram) update(updateFn func(float64) float64) {
	value, delta := func(th *timingHistogram) (float64, time.Duration) {
		th.lock.Lock()
		defer th.lock.Unlock()
		now := th.clock.Now()
		delta := now.Sub(th.lastSetTime)
		value := th.value
		if delta > 0 {
			th.lastSetTime = now
		}
		th.value = updateFn(value)
		return value, delta
	}(th)
	if delta > 0 {
		th.weighted.ObserveWithWeight(value, uint64(delta))
	}
}

func (th *timingHistogram) Desc() *prometheus.Desc {
	return th.weighted.Desc()
}

func (th *timingHistogram) Write(dest *dto.Metric) error {
	th.Add(0) // account for time since last update
	return th.weighted.Write(dest)
}

func (th *timingHistogram) Describe(ch chan<- *prometheus.Desc) {
	ch <- th.weighted.Desc()
}

func (th *timingHistogram) Collect(ch chan<- prometheus.Metric) {
	ch <- th
}
