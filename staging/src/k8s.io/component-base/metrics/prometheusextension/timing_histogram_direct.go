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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// NewTimingHistogram creates a new TimingHistogram
func NewTimingHistogramDirect(opts TimingHistogramOpts) (TimingHistogram, error) {
	return NewTestableTimingHistogramDirect(realNow, opts)
}

// NewTestableTimingHistogram creates a TimingHistogram that uses a mockable clock
func NewTestableTimingHistogramDirect(nowFunc func() time.Time, opts TimingHistogramOpts) (TimingHistogram, error) {
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		nil,
		opts.ConstLabels,
	)
	return newTimingHistogramDirect(nowFunc, desc, opts)
}

func newTimingHistogramDirect(nowFunc func() time.Time, desc *prometheus.Desc, opts TimingHistogramOpts, variableLabelValues ...string) (TimingHistogram, error) {
	allLabelsM := prometheus.Labels{}
	allLabelsS := prometheus.MakeLabelPairs(desc, variableLabelValues)
	for _, pair := range allLabelsS {
		if pair == nil || pair.Name == nil || pair.Value == nil {
			return nil, errors.New("prometheus.MakeLabelPairs returned a nil")
		}
		allLabelsM[*pair.Name] = *pair.Value
	}
	weighted, err := newWeightedHistogram(desc, WeightedHistogramOpts{
		Namespace:   opts.Namespace,
		Subsystem:   opts.Subsystem,
		Name:        opts.Name,
		Help:        opts.Help,
		ConstLabels: allLabelsM,
		Buckets:     opts.Buckets,
	}, variableLabelValues...)
	if err != nil {
		return nil, err
	}
	return &timingHistogramDirect{
		nowFunc:     nowFunc,
		weighted:    weighted,
		lastSetTime: nowFunc(),
		value:       opts.InitialValue,
	}, nil
}

type timingHistogramDirect struct {
	nowFunc  func() time.Time
	weighted *weightedHistogram

	// identifies when value was last set
	lastSetTime time.Time
	value       float64
}

var _ TimingHistogram = &timingHistogramDirect{}

func (th *timingHistogramDirect) Set(newValue float64) {
	th.update(func(float64) float64 { return newValue })
}

func (th *timingHistogramDirect) Inc() {
	th.update(func(oldValue float64) float64 { return oldValue + 1 })
}

func (th *timingHistogramDirect) Dec() {
	th.update(func(oldValue float64) float64 { return oldValue - 1 })
}

func (th *timingHistogramDirect) Add(delta float64) {
	th.update(func(oldValue float64) float64 { return oldValue + delta })
}

func (th *timingHistogramDirect) Sub(delta float64) {
	th.update(func(oldValue float64) float64 { return oldValue - delta })
}

func (th *timingHistogramDirect) SetToCurrentTime() {
	th.update(func(oldValue float64) float64 { return th.nowFunc().Sub(time.Unix(0, 0)).Seconds() })
}

func (th *timingHistogramDirect) update(updateFn func(float64) float64) {
	th.weighted.lock.Lock()
	defer th.weighted.lock.Unlock()
	now := th.nowFunc()
	delta := now.Sub(th.lastSetTime)
	value := th.value
	if delta > 0 {
		th.weighted.observeWithWeightLocked(value, uint64(delta))
		th.lastSetTime = now
	}
	th.value = updateFn(value)
}

func (th *timingHistogramDirect) Desc() *prometheus.Desc {
	return th.weighted.Desc()
}

func (th *timingHistogramDirect) Write(dest *dto.Metric) error {
	th.Add(0) // account for time since last update
	return th.weighted.Write(dest)
}

func (th *timingHistogramDirect) Describe(ch chan<- *prometheus.Desc) {
	ch <- th.weighted.Desc()
}

func (th *timingHistogramDirect) Collect(ch chan<- prometheus.Metric) {
	ch <- th
}
