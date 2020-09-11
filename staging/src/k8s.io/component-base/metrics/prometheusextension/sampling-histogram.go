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
	prometheus.HistogramOpts

	// The initial value of the variable.
	InitialValue float64

	// The variable is sampled once every this often.
	// Must be set to a positive value.
	SamplingPeriod time.Duration
}

// NewSamplingHistogram creates a new SamplingHistogram
func NewSamplingHistogram(opts SamplingHistogramOpts) (SamplingHistogram, error) {
	if opts.SamplingPeriod <= 0 {
		return nil, fmt.Errorf("the given sampling period was %v but must be positive", opts.SamplingPeriod)
	}
	return NewTestableSamplingHistogram(clock.RealClock{}, opts)
}

// NewTestableSamplingHistogram creates a SamplingHistogram that uses a mockable clock
func NewTestableSamplingHistogram(clock clock.Clock, opts SamplingHistogramOpts) (SamplingHistogram, error) {
	if opts.SamplingPeriod <= 0 {
		return nil, fmt.Errorf("the given sampling period was %v but must be positive", opts.SamplingPeriod)
	}
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		nil,
		opts.ConstLabels,
	)
	return newSamplingHistogram(clock, desc, opts), nil
}

func newSamplingHistogram(clock clock.Clock, desc *prometheus.Desc, opts SamplingHistogramOpts, labelValues ...string) SamplingHistogram {
	allLabels := prometheus.MakeLabelPairs(desc, labelValues)
	innerOpts := opts.HistogramOpts
	innerOpts.ConstLabels = make(map[string]string, len(allLabels))
	for _, nv := range allLabels {
		if nv == nil || nv.Name == nil || nv.Value == nil {
			continue
		}
		innerOpts.ConstLabels[*nv.Name] = *nv.Value
	}
	return &samplingHistogram{
		samplingPeriod:  opts.SamplingPeriod,
		histogram:       prometheus.NewHistogram(innerOpts),
		clock:           clock,
		lastSampleIndex: clock.Now().UnixNano() / int64(opts.SamplingPeriod),
		value:           opts.InitialValue,
	}
}

type samplingHistogram struct {
	samplingPeriod time.Duration
	histogram      prometheus.Histogram
	clock          clock.Clock
	lock           sync.Mutex

	// identifies the last sampling period completed
	lastSampleIndex int64
	value           float64
}

var _ SamplingHistogram = &samplingHistogram{}

func (sh *samplingHistogram) Set(newValue float64) {
	sh.Update(func(float64) float64 { return newValue })
}

func (sh *samplingHistogram) Add(delta float64) {
	sh.Update(func(oldValue float64) float64 { return oldValue + delta })
}

func (sh *samplingHistogram) Update(updateFn func(float64) float64) {
	oldValue, numSamples := func() (float64, int64) {
		sh.lock.Lock()
		defer sh.lock.Unlock()
		newSampleIndex := sh.clock.Now().UnixNano() / int64(sh.samplingPeriod)
		deltaIndex := newSampleIndex - sh.lastSampleIndex
		sh.lastSampleIndex = newSampleIndex
		oldValue := sh.value
		sh.value = updateFn(sh.value)
		return oldValue, deltaIndex
	}()
	for i := int64(0); i < numSamples; i++ {
		sh.histogram.Observe(oldValue)
	}
}

func (sh *samplingHistogram) Desc() *prometheus.Desc {
	return sh.histogram.Desc()
}

func (sh *samplingHistogram) Write(dest *dto.Metric) error {
	return sh.histogram.Write(dest)
}

func (sh *samplingHistogram) Describe(ch chan<- *prometheus.Desc) {
	sh.histogram.Describe(ch)
}

func (sh *samplingHistogram) Collect(ch chan<- prometheus.Metric) {
	sh.Add(0)
	sh.histogram.Collect(ch)
}

// SamplingHistogramVec is a Collector that bundles a set of SamplingHistograms that all share the
// same Desc, but have different values for their variable labels. This is used
// if you want to monitor a set of variables arrayed in various dimensions
// (e.g. HTTP server occupancy, partitioned by resource and method). Create
// instances with NewSamplingHistogramVec.
type SamplingHistogramVec struct {
	*prometheus.MetricVec
}

// NewSamplingHistogramVec creates a new SamplingHistogramVec based on the provided SamplingHistogramOpts and
// partitioned by the given label names.
func NewSamplingHistogramVec(opts SamplingHistogramOpts, labelNames []string) (*SamplingHistogramVec, error) {
	if opts.SamplingPeriod <= 0 {
		return nil, fmt.Errorf("the given sampling period was %v but must be positive", opts.SamplingPeriod)
	}
	return NewTestableSamplingHistogramVec(clock.RealClock{}, opts, labelNames)
}

// NewSamplingHistogramVec creates a new SamplingHistogramVec based on the provided SamplingHistogramOpts and
// partitioned by the given label names.
func NewTestableSamplingHistogramVec(clock clock.Clock, opts SamplingHistogramOpts, labelNames []string) (*SamplingHistogramVec, error) {
	if opts.SamplingPeriod <= 0 {
		return nil, fmt.Errorf("the given sampling period was %v but must be positive", opts.SamplingPeriod)
	}
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		labelNames,
		opts.ConstLabels,
	)
	return &SamplingHistogramVec{
		MetricVec: prometheus.NewMetricVec(desc, func(lvs ...string) prometheus.Metric {
			return newSamplingHistogram(clock, desc, opts, lvs...)
		}),
	}, nil
}

// FloatVar is the interface used by clients to add data to a SamplingHistogram
type FloatVar interface {
	Set(float64)
	Add(float64)
}

// FloatVarVec is an interface implemented by `SamplingHistogramVec`
type FloatVarVec interface {
	GetMetricWith(prometheus.Labels) (FloatVar, error)
	GetMetricWithLabelValues(lvs ...string) (FloatVar, error)
	With(prometheus.Labels) FloatVar
	WithLabelValues(...string) FloatVar
	CurryWith(prometheus.Labels) (FloatVarVec, error)
	MustCurryWith(prometheus.Labels) FloatVarVec

	prometheus.Collector
}

func (v *SamplingHistogramVec) GetMetricWithLabelValues(lvs ...string) (FloatVar, error) {
	metric, err := v.MetricVec.GetMetricWithLabelValues(lvs...)
	if metric != nil {
		return metric.(FloatVar), err
	}
	return nil, err
}

func (v *SamplingHistogramVec) GetMetricWith(labels prometheus.Labels) (FloatVar, error) {
	metric, err := v.MetricVec.GetMetricWith(labels)
	if metric != nil {
		return metric.(FloatVar), err
	}
	return nil, err
}

func (v *SamplingHistogramVec) WithLabelValues(lvs ...string) FloatVar {
	h, err := v.GetMetricWithLabelValues(lvs...)
	if err != nil {
		panic(err)
	}
	return h
}

func (v *SamplingHistogramVec) With(labels prometheus.Labels) FloatVar {
	h, err := v.GetMetricWith(labels)
	if err != nil {
		panic(err)
	}
	return h
}

func (v *SamplingHistogramVec) CurryWith(labels prometheus.Labels) (FloatVarVec, error) {
	vec, err := v.MetricVec.CurryWith(labels)
	if vec != nil {
		return &SamplingHistogramVec{vec}, err
	}
	return nil, err
}

func (v *SamplingHistogramVec) MustCurryWith(labels prometheus.Labels) FloatVarVec {
	vec, err := v.CurryWith(labels)
	if err != nil {
		panic(err)
	}
	return vec
}
