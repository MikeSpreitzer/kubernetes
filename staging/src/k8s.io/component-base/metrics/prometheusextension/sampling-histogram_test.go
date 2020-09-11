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
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	testclock "k8s.io/utils/clock/testing"
)

func TestSamplingHistogram(t *testing.T) {
	clk := testclock.NewFakeClock(time.Unix(time.Now().Unix(), 999999990))
	sh, err := NewTestableSamplingHistogram(clk,
		SamplingHistogramOpts{
			HistogramOpts: prometheus.HistogramOpts{
				Namespace:   "test",
				Subsystem:   "func",
				Name:        "one",
				Help:        "a helpful string",
				ConstLabels: map[string]string{"l1": "v1", "l2": "v2"},
				Buckets:     []float64{0, 1, 2},
			},
			InitialValue:   1,
			SamplingPeriod: time.Second,
		})
	if sh == nil || err != nil {
		t.Errorf("Creation failed; err=%s", err.Error())
	}
	exerciseSamplingHistogram(t, "singleton", clk, sh, map[string]string{"l1": "v1", "l2": "v2"})
}

func TestSamplingHistogramVec(t *testing.T) {
	clk := testclock.NewFakeClock(time.Unix(time.Now().Unix(), 999999990))
	shv, err := NewTestableSamplingHistogramVec(clk,
		SamplingHistogramOpts{
			HistogramOpts: prometheus.HistogramOpts{
				Namespace:   "test",
				Subsystem:   "func",
				Name:        "one",
				Help:        "a helpful string",
				ConstLabels: map[string]string{"l1": "v1", "l2": "v2"},
				Buckets:     []float64{0, 1, 2},
			},
			InitialValue:   1,
			SamplingPeriod: time.Second,
		},
		[]string{"l3", "l4"})
	if shv == nil || err != nil {
		t.Errorf("Creation failed; err=%s", err.Error())
	}
	sh1 := shv.WithLabelValues("v3", "v4").(SamplingHistogram)
	exerciseSamplingHistogram(t, "vector member 1", clk, sh1, map[string]string{"l1": "v1", "l2": "v2", "l3": "v3", "l4": "v4"})
	clk.SetTime(time.Unix(time.Now().Unix()+1, 999999990))
	sh2 := shv.With(map[string]string{"l4": "V4", "l3": "V3"}).(SamplingHistogram)
	exerciseSamplingHistogram(t, "vector member 2", clk, sh2, map[string]string{"l1": "v1", "l2": "v2", "l3": "V3", "l4": "V4"})
}

func exerciseSamplingHistogram(t *testing.T, what string, clk *testclock.FakeClock, sh SamplingHistogram, expectedLabels map[string]string) {
	t.Logf("%s is %#+v", what, sh)
	expectHistogram(t, "After creation of "+what, sh, expectedLabels, 0, 0, 0, 0)
	sh.Set(2)
	expectHistogram(t, "After initial Set of "+what, sh, expectedLabels, 0, 0, 0, 0)
	clk.Step(9 * time.Nanosecond)
	expectHistogram(t, "Just before the end of the first sampling period of "+what, sh, expectedLabels, 0, 0, 0, 0)
	clk.Step(1 * time.Nanosecond)
	expectHistogram(t, "At the end of the first sampling period of "+what, sh, expectedLabels, 0, 0, 1, 1)
	clk.Step(1 * time.Nanosecond)
	expectHistogram(t, "Barely into second sampling period of "+what, sh, expectedLabels, 0, 0, 1, 1)
	sh.Set(-0.5)
	sh.Add(1)
	clk.Step(999999998 * time.Nanosecond)
	expectHistogram(t, "Just before the end of second sampling period of "+what, sh, expectedLabels, 0, 0, 1, 1)
	clk.Step(1 * time.Nanosecond)
	expectHistogram(t, "At the end of second sampling period of "+what, sh, expectedLabels, 0, 1, 2, 2)
}

func expectHistogram(t *testing.T, when string, sh SamplingHistogram, expectedLabels map[string]string, buckets ...uint64) {
	metrics := make(chan prometheus.Metric, 2)
	sh.Collect(metrics)
	var dtom dto.Metric
	select {
	case metric := <-metrics:
		metric.Write(&dtom)
	default:
		t.Errorf("%s, zero Metrics collected", when)
	}
	select {
	case metric := <-metrics:
		t.Errorf("%s, collected more than one Metric; second Metric = %#+v", when, metric)
	default:
	}
	missingLabels := make(map[string]string, len(expectedLabels))
	for k, v := range expectedLabels {
		missingLabels[k] = v
	}
	for _, lp := range dtom.Label {
		if lp == nil || lp.Name == nil || lp.Value == nil {
			continue
		}
		if val, ok := missingLabels[*(lp.Name)]; ok {
			if val != *(lp.Value) {
				t.Errorf("%s, found label named %q with value %q instead of %q", when, *(lp.Name), *(lp.Value), val)
			}
		} else {
			t.Errorf("%s, got unexpected label name %q", when, *(lp.Name))
		}
		delete(missingLabels, *(lp.Name))
	}
	if len(missingLabels) > 0 {
		t.Errorf("%s, missed labels %#+v", when, missingLabels)
	}
	if dtom.Histogram == nil {
		t.Errorf("%s, Collect returned a Metric without a Histogram: %#+v", when, dtom)
	}
	mh := dtom.Histogram
	if len(mh.Bucket) != len(buckets)-1 {
		t.Errorf("%s, expected %d buckets but got %d: %#+v", when, len(buckets)-1, len(mh.Bucket), mh.Bucket)
	}
	if mh.SampleCount == nil {
		t.Errorf("%s, got Histogram with nil SampleCount", when)
	}
	if *(mh.SampleCount) != buckets[len(mh.Bucket)] {
		t.Errorf("%s, SampleCount=%d but expected %d", when, *(mh.SampleCount), buckets[len(mh.Bucket)])
	}
	for i, mhBucket := range mh.Bucket {
		if mhBucket == nil {
			t.Errorf("%s, bucket %d was nil", when, i)
		}
		if mhBucket.UpperBound == nil || mhBucket.CumulativeCount == nil {
			t.Errorf("%s, bucket %d had nil bound or count", when, i)
		}
		ub := float64(i)
		if ub != *(mhBucket.UpperBound) {
			t.Errorf("%s, bucket %d's upper bound was %v", when, i, *(mhBucket.UpperBound))
		}
		expectedCount := buckets[i]
		if expectedCount != *(mhBucket.CumulativeCount) {
			t.Errorf("%s, bucket %d's count was %d rather than %d", when, i, mhBucket.CumulativeCount, expectedCount)
		}
	}
}
