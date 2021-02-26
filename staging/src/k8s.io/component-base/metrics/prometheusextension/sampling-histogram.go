/*
Copyright 2020 Mike Spreitzer.

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
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"k8s.io/utils/clock"
)

// A SamplingHistogram samples the values of a float64 variable at a
// configured rate.  The samples contribute to a Histogram.
type SamplingHistogram interface {
	prometheus.Metric
	prometheus.Collector

	// Set the variable to the given value.
	Set(float64)

	// Add the given change to the variable
	Add(float64)
}

// SamplingHistogramOpts bundles the options for creating a
// SamplingHistogram metric.  This builds on the options for creating
// a Histogram metric.
type SamplingHistogramOpts struct {
	Namespace   string
	Subsystem   string
	Name        string
	Help        string
	ConstLabels prometheus.Labels

	// Buckets defines the buckets into which observations are counted. Each
	// element in the slice is the upper inclusive bound of a bucket. The
	// values must be sorted in strictly increasing order. There is no need
	// to add a highest bucket with +Inf bound, it will be added
	// implicitly. The default value is DefBuckets.
	Buckets []float64

	// The initial value of the variable.
	InitialValue float64

	// The variable is sampled once every this often.
	// Must be set to a positive value.
	SamplingPeriod time.Duration
}

// NewSamplingHistogram creates a new SamplingHistogram
func NewSamplingHistogram(opts SamplingHistogramOpts) (SamplingHistogram, error) {
	return NewTestableSamplingHistogram(clock.RealClock{}, opts)
}

// NewTestableSamplingHistogram creates a SamplingHistogram that uses a mockable clock
func NewTestableSamplingHistogram(clock clock.Clock, opts SamplingHistogramOpts) (SamplingHistogram, error) {
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		nil,
		opts.ConstLabels,
	)
	return newSamplingHistogram(clock, desc, opts)
}

func newSamplingHistogram(clock clock.Clock, desc *prometheus.Desc, opts SamplingHistogramOpts) (SamplingHistogram, error) {
	if opts.SamplingPeriod <= 0 {
		return nil, fmt.Errorf("the given sampling period was %v but must be positive", opts.SamplingPeriod)
	}
	if len(opts.Buckets) == 0 {
		opts.Buckets = prometheus.DefBuckets
	}

	for i, upperBound := range opts.Buckets {
		if i < len(opts.Buckets)-1 {
			if upperBound >= opts.Buckets[i+1] {
				return nil, fmt.Errorf(
					"histogram buckets must be in increasing order: %f >= %f",
					upperBound, opts.Buckets[i+1],
				)
			}
		} else {
			if math.IsInf(upperBound, +1) {
				// The +Inf bucket is implicit. Remove it here.
				opts.Buckets = opts.Buckets[:i]
			}
		}
	}

	return &samplingHistogram{
		samplingPeriod:  opts.SamplingPeriod,
		desc:            desc,
		clock:           clock,
		lastSampleIndex: clock.Now().UnixNano() / int64(opts.SamplingPeriod),
		value:           opts.InitialValue,
		buckets:         make([]uint64, len(opts.Buckets)),
		upperBounds:     opts.Buckets,
	}, nil
}

type samplingHistogram struct {
	samplingPeriod time.Duration
	desc           *prometheus.Desc
	clock          clock.Clock
	lock           sync.Mutex

	// identifies the last sampling period completed
	lastSampleIndex int64
	value           float64

	sum         float64
	count       uint64
	buckets     []uint64
	upperBounds []float64
}

var _ SamplingHistogram = &samplingHistogram{}

func (sh *samplingHistogram) Set(newValue float64) {
	sh.Update(func(float64) float64 { return newValue })
}

func (sh *samplingHistogram) Add(delta float64) {
	sh.Update(func(oldValue float64) float64 { return oldValue + delta })
}

func (sh *samplingHistogram) Update(updateFn func(float64) float64) {
	sh.lock.Lock()
	defer sh.lock.Unlock()

	newSampleIndex := sh.clock.Now().UnixNano() / int64(sh.samplingPeriod)
	deltaIndex := uint64(newSampleIndex - sh.lastSampleIndex)
	sh.lastSampleIndex = newSampleIndex
	oldValue := sh.value
	sh.value = updateFn(sh.value)

	// Increment the actual histogram parts.
	sh.count += deltaIndex
	sh.sum += oldValue * float64(deltaIndex)
	i := sort.SearchFloat64s(sh.upperBounds, oldValue)
	if i < len(sh.buckets) {
		sh.buckets[i] += deltaIndex
	}
}

func (sh *samplingHistogram) Desc() *prometheus.Desc {
	return sh.desc
}

func (sh *samplingHistogram) Write(dest *dto.Metric) error {
	sh.lock.Lock()
	defer sh.lock.Unlock()

	buckets := make(map[float64]uint64, len(sh.buckets))
	var cumCount uint64
	for i, count := range sh.buckets {
		cumCount += count
		buckets[sh.upperBounds[i]] = cumCount
	}
	metric, err := prometheus.NewConstHistogram(sh.desc, sh.count, sh.sum, buckets)
	if err != nil {
		return err
	}
	return metric.Write(dest)
}

func (sh *samplingHistogram) Describe(ch chan<- *prometheus.Desc) {
	ch <- sh.desc
}

func (sh *samplingHistogram) Collect(ch chan<- prometheus.Metric) {
	sh.Add(0)
	ch <- sh
}
