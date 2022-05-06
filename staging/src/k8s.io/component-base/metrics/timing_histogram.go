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
	"sync"
	"time"

	"github.com/blang/semver/v4"
	promext "k8s.io/component-base/metrics/prometheusextension"
)

// TimingHistogram is our internal representation for our wrapping struct around timing
// histograms. It implements both kubeCollector and GaugeMetric
type TimingHistogram struct {
	GaugeMetric
	opts    LatchingTimingHistogramOpts
	nowFunc func() time.Time
	lazyMetric
	selfCollector
}

type LatchingTimingHistogramOpts struct {
	TimingHistogramOpts
	deprecateOnce sync.Once
	annotateOnce  sync.Once
}

// Modify help description on the metric description.
func (o *LatchingTimingHistogramOpts) markDeprecated() {
	o.deprecateOnce.Do(func() {
		o.Help = fmt.Sprintf("(Deprecated since %v) %v", o.DeprecatedVersion, o.Help)
	})
}

// annotateStabilityLevel annotates help description on the metric description with the stability level
// of the metric
func (o *LatchingTimingHistogramOpts) annotateStabilityLevel() {
	o.annotateOnce.Do(func() {
		o.Help = fmt.Sprintf("[%v] %v", o.StabilityLevel, o.Help)
	})
}

// convenience function to allow easy transformation to the prometheus
// counterpart. This will do more once we have a proper label abstraction
func (o *TimingHistogramOpts) toPromHistogramOpts() promext.TimingHistogramOpts {
	return promext.TimingHistogramOpts{
		Namespace:    o.Namespace,
		Subsystem:    o.Subsystem,
		Name:         o.Name,
		Help:         o.Help,
		ConstLabels:  o.ConstLabels,
		Buckets:      o.Buckets,
		InitialValue: o.InitialValue,
	}
}

// NewTimingHistogram returns an object which is TimingHistogram-like. However, nothing
// will be measured until the histogram is registered somewhere.
func NewTimingHistogram(opts TimingHistogramOpts) *TimingHistogram {
	return NewTestableTimingHistogram(time.Now, opts)
}

// NewTestableTimingHistogram adds injection of the clock
func NewTestableTimingHistogram(nowFunc func() time.Time, opts TimingHistogramOpts) *TimingHistogram {
	opts.StabilityLevel.setDefaults()

	h := &TimingHistogram{
		opts:       LatchingTimingHistogramOpts{TimingHistogramOpts: opts},
		nowFunc:    nowFunc,
		lazyMetric: lazyMetric{},
	}
	h.setPrometheusHistogram(noopMetric{})
	h.lazyInit(h, BuildFQName(opts.Namespace, opts.Subsystem, opts.Name))
	return h
}

// setPrometheusHistogram sets the underlying KubeGauge object, i.e. the thing that does the measurement.
func (h *TimingHistogram) setPrometheusHistogram(histogram promext.TimingHistogram) {
	h.GaugeMetric = histogram
	h.initSelfCollection(histogram)
}

// DeprecatedVersion returns a pointer to the Version or nil
func (h *TimingHistogram) DeprecatedVersion() *semver.Version {
	return parseSemver(h.opts.DeprecatedVersion)
}

// initializeMetric invokes the actual prometheus.Histogram object instantiation
// and stores a reference to it
func (h *TimingHistogram) initializeMetric() {
	h.opts.annotateStabilityLevel()
	// this actually creates the underlying prometheus gauge.
	histogram, err := promext.NewTestableTimingHistogram(h.nowFunc, h.opts.toPromHistogramOpts())
	if err != nil {
		panic(err) // handle as for regular histograms
	}
	h.setPrometheusHistogram(histogram)
}

// initializeDeprecatedMetric invokes the actual prometheus.Histogram object instantiation
// but modifies the Help description prior to object instantiation.
func (h *TimingHistogram) initializeDeprecatedMetric() {
	h.opts.markDeprecated()
	h.initializeMetric()
}

// WithContext allows the normal TimingHistogram metric to pass in context. The context is no-op now.
func (h *TimingHistogram) WithContext(ctx context.Context) GaugeMetric {
	return h.GaugeMetric
}

// timingHistogramVec is the internal representation of our wrapping struct around prometheus
// TimingHistogramVecs.
type timingHistogramVec struct {
	*promext.TimingHistogramVec
	opts    LatchingTimingHistogramOpts
	nowFunc func() time.Time
	lazyMetric
	originalLabels []string
}

// NewTimingHistogramVec returns an object which satisfies kubeCollector and
// wraps the promext.timingHistogramVec object.  Note well the way that
// behavior depends on registration and whether this is hidden.
func NewTimingHistogramVec(opts TimingHistogramOpts, labels []string) PreContextAndRegisterableGaugeMetricVec {
	return NewTestableTimingHistogramVec(time.Now, opts, labels)
}

// NewTestableTimingHistogramVec adds injection of the clock.
func NewTestableTimingHistogramVec(nowFunc func() time.Time, opts TimingHistogramOpts, labels []string) PreContextAndRegisterableGaugeMetricVec {
	opts.StabilityLevel.setDefaults()

	fqName := BuildFQName(opts.Namespace, opts.Subsystem, opts.Name)
	allowListLock.RLock()
	if allowList, ok := labelValueAllowLists[fqName]; ok {
		opts.LabelValueAllowLists = allowList
	}
	allowListLock.RUnlock()

	v := &timingHistogramVec{
		TimingHistogramVec: noopTimingHistogramVec,
		opts:               LatchingTimingHistogramOpts{TimingHistogramOpts: opts},
		nowFunc:            nowFunc,
		originalLabels:     labels,
		lazyMetric:         lazyMetric{},
	}
	v.lazyInit(v, fqName)
	return v
}

// DeprecatedVersion returns a pointer to the Version or nil
func (v *timingHistogramVec) DeprecatedVersion() *semver.Version {
	return parseSemver(v.opts.DeprecatedVersion)
}

func (v *timingHistogramVec) initializeMetric() {
	v.opts.annotateStabilityLevel()
	v.TimingHistogramVec = promext.NewTestableTimingHistogramVec(v.nowFunc, v.opts.toPromHistogramOpts(), v.originalLabels...)
}

func (v *timingHistogramVec) initializeDeprecatedMetric() {
	v.opts.markDeprecated()
	v.initializeMetric()
}

func (v *timingHistogramVec) Set(value float64, labelValues ...string) {
	gm, _ := v.WithLabelValues(labelValues...)
	gm.Set(value)
}

func (v *timingHistogramVec) Inc(labelValues ...string) {
	gm, _ := v.WithLabelValues(labelValues...)
	gm.Inc()
}

func (v *timingHistogramVec) Dec(labelValues ...string) {
	gm, _ := v.WithLabelValues(labelValues...)
	gm.Dec()
}

func (v *timingHistogramVec) Add(delta float64, labelValues ...string) {
	gm, _ := v.WithLabelValues(labelValues...)
	gm.Add(delta)
}
func (v *timingHistogramVec) SetToCurrentTime(labelValues ...string) {
	gm, _ := v.WithLabelValues(labelValues...)
	gm.SetToCurrentTime()
}

func (v *timingHistogramVec) SetForLabels(value float64, labels map[string]string) {
	gm, _ := v.With(labels)
	gm.Set(value)
}

func (v *timingHistogramVec) IncForLabels(labels map[string]string) {
	gm, _ := v.With(labels)
	gm.Inc()
}

func (v *timingHistogramVec) DecForLabels(labels map[string]string) {
	gm, _ := v.With(labels)
	gm.Dec()
}

func (v *timingHistogramVec) AddForLabels(delta float64, labels map[string]string) {
	gm, _ := v.With(labels)
	gm.Add(delta)
}
func (v *timingHistogramVec) SetToCurrentTimeForLabels(labels map[string]string) {
	gm, _ := v.With(labels)
	gm.SetToCurrentTime()
}

// WithLabelValues, if called after this vector has been
// registered in at least one registry and this vector is not
// hidden, will return a GaugeMetric that is NOT a noop along
// with nil error.  If called on a hidden vector then it will
// return a noop and a nil error.  Otherwise it returns a noop
// and an error that passes ErrIsNotRegistered.
func (v *timingHistogramVec) WithLabelValues(lvs ...string) (GaugeMetric, error) {
	if v.IsHidden() {
		return noop, nil
	}
	if !v.IsCreated() {
		return noop, errNotRegistered
	}
	if v.opts.LabelValueAllowLists != nil {
		v.opts.LabelValueAllowLists.ConstrainToAllowedList(v.originalLabels, lvs)
	}
	return v.TimingHistogramVec.WithLabelValues(lvs...).(GaugeMetric), nil
}

// With, if called after this vector has been
// registered in at least one registry and this vector is not
// hidden, will return a GaugeMetric that is NOT a noop along
// with nil error.  If called on a hidden vector then it will
// return a noop and a nil error.  Otherwise it returns a noop
// and an error that passes ErrIsNotRegistered.
func (v *timingHistogramVec) With(labels map[string]string) (GaugeMetric, error) {
	if v.IsHidden() {
		return noop, nil
	}
	if !v.IsCreated() {
		return noop, errNotRegistered
	}
	if v.opts.LabelValueAllowLists != nil {
		v.opts.LabelValueAllowLists.ConstrainLabelMap(labels)
	}
	return v.TimingHistogramVec.With(labels).(GaugeMetric), nil
}

// Delete deletes the metric where the variable labels are the same as those
// passed in as labels. It returns true if a metric was deleted.
//
// It is not an error if the number and names of the Labels are inconsistent
// with those of the VariableLabels in Desc. However, such inconsistent Labels
// can never match an actual metric, so the method will always return false in
// that case.
func (v *timingHistogramVec) Delete(labels map[string]string) bool {
	if !v.IsCreated() {
		return false // since we haven't created the metric, we haven't deleted a metric with the passed in values
	}
	return v.TimingHistogramVec.Delete(labels)
}

// Reset deletes all metrics in this vector.
func (v *timingHistogramVec) Reset() {
	if !v.IsCreated() {
		return
	}

	v.TimingHistogramVec.Reset()
}

// WithContext returns wrapped timingHistogramVec with context
func (v *timingHistogramVec) WithContext(ctx context.Context) GaugeMetricVec {
	return &TimingHistogramVecWithContext{
		ctx:                ctx,
		timingHistogramVec: v,
	}
}

// TimingHistogramVecWithContext is the wrapper of timingHistogramVec with context.
// Currently the context is ignored.
type TimingHistogramVecWithContext struct {
	*timingHistogramVec
	ctx context.Context
}
