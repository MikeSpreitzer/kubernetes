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
)

// NewTimingHistogram creates a new TimingHistogram
func NewTimingHistogramLayered(opts TimingHistogramOpts) (TimingHistogram, error) {
	return NewTestableTimingHistogramLayered(realNow, opts)
}

// NewTestableTimingHistogram creates a TimingHistogram that uses a mockable clock
func NewTestableTimingHistogramLayered(nowFunc func() time.Time, opts TimingHistogramOpts) (TimingHistogram, error) {
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		nil,
		opts.ConstLabels,
	)
	return newTimingHistogramLayered(nowFunc, desc, opts)
}

func newTimingHistogramLayered(nowFunc func() time.Time, desc *prometheus.Desc, opts TimingHistogramOpts, variableLabelValues ...string) (TimingHistogram, error) {
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
	return &timingHistogramLayered{
		nowFunc:     nowFunc,
		weighted:    weighted,
		lastSetTime: nowFunc(),
		value:       opts.InitialValue,
	}, nil
}

type timingHistogramLayered struct {
	nowFunc  func() time.Time
	weighted WeightedHistogram

	lock sync.Mutex // applies to all the following

	// identifies when value was last set
	lastSetTime time.Time
	value       float64
}

var _ TimingHistogram = &timingHistogramLayered{}

func (th *timingHistogramLayered) Set(newValue float64) {
	th.update(func(float64) float64 { return newValue })
}

func (th *timingHistogramLayered) Inc() {
	th.update(func(oldValue float64) float64 { return oldValue + 1 })
}

func (th *timingHistogramLayered) Dec() {
	th.update(func(oldValue float64) float64 { return oldValue - 1 })
}

func (th *timingHistogramLayered) Add(delta float64) {
	th.update(func(oldValue float64) float64 { return oldValue + delta })
}

func (th *timingHistogramLayered) Sub(delta float64) {
	th.update(func(oldValue float64) float64 { return oldValue - delta })
}

func (th *timingHistogramLayered) SetToCurrentTime() {
	th.update(func(oldValue float64) float64 { return th.nowFunc().Sub(time.Unix(0, 0)).Seconds() })
}

func (th *timingHistogramLayered) update(updateFn func(float64) float64) {
	value, delta := func(th *timingHistogramLayered) (float64, time.Duration) {
		th.lock.Lock()
		defer th.lock.Unlock()
		now := th.nowFunc()
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

func (th *timingHistogramLayered) Desc() *prometheus.Desc {
	return th.weighted.Desc()
}

func (th *timingHistogramLayered) Write(dest *dto.Metric) error {
	th.Add(0) // account for time since last update
	return th.weighted.Write(dest)
}

func (th *timingHistogramLayered) Describe(ch chan<- *prometheus.Desc) {
	ch <- th.weighted.Desc()
}

func (th *timingHistogramLayered) Collect(ch chan<- prometheus.Metric) {
	ch <- th
}
