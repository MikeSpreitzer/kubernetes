package metrics

import (
	"testing"
	"time"

	compbasemetrics "k8s.io/component-base/metrics"
	testclock "k8s.io/utils/clock/testing"
)

func BenchmarkSampleAndWatermarkHistogramVec(b *testing.B) {
	b.StopTimer()
	now := time.Now()
	clk := testclock.NewFakePassiveClock(now)
	whv := NewSampleAndWaterMarkHistogramsVec(clk,
		time.Millisecond,
		&compbasemetrics.HistogramOpts{
			Namespace: "testns",
			Subsystem: "testsubsys",
			Name:      "testhist_samples",
			Help:      "Me",
			Buckets:   []float64{1, 2, 4, 8, 16},
		},
		&compbasemetrics.HistogramOpts{
			Namespace: "testns",
			Subsystem: "testsubsys",
			Name:      "testhist_watermarks",
			Help:      "Me",
			Buckets:   []float64{1, 2, 4, 8, 16},
		},
		"labelname")
	registry := compbasemetrics.NewKubeRegistry()
	registry.MustRegister(whv.metrics()...)
	wh, err := whv.WithLabelValues(3, "labelvalue")
	if err != nil {
		b.Error(err)
	}
	var x int
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		delta := (i % 6) + 1
		now = now.Add(time.Duration(delta) * time.Millisecond)
		clk.SetTime(now)
		wh.Observe(float64(x))
		x = (x + i) % 60
	}
}
