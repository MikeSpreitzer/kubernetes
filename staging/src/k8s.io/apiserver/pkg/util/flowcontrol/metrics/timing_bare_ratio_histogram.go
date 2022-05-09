package metrics

import (
	"time"

	compbasemetrics "k8s.io/component-base/metrics"
)

// TimingRatioHistogram is essentially a gauge for a ratio where the client
// independently controls the numerator and denominator.
// When scraped it produces a histogram of samples of the ratio
// taken at the end of every nanosecond.
// `*TimingRatioHistogram` implements both Registerable and RatioedObserver.
type TimingBareRatioHistogram struct {
	// The implementation is layered on TimingHistogram,
	// adding the division by an occasionally adjusted denominator.

	// Registerable is the registerable aspect.
	// That is the registerable aspect of the underlying TimingHistogram.
	compbasemetrics.Registerable

	// timingRatioHistogramInner implements the RatioedObsserver aspect.
	timingBareRatioHistogramInner
}

// timingRatioHistogramInner implements the instrumentation aspect
type timingBareRatioHistogramInner struct {
	under                  compbasemetrics.GaugeMetric
	numerator, denominator float64
}

var _ RatioedObserver = &timingBareRatioHistogramInner{}
var _ RatioedObserver = &TimingBareRatioHistogram{}
var _ compbasemetrics.Registerable = &TimingBareRatioHistogram{}

// NewTestableTimingHistogram adds injection of the clock
func NewTestableTimingBareRatioHistogram(nowFunc func() time.Time, opts *TimingRatioHistogramOpts) *TimingBareRatioHistogram {
	ratioedOpts := opts.TimingHistogramOpts
	ratioedOpts.InitialValue /= opts.InitialDenominator
	th := compbasemetrics.NewTestableTimingHistogram(nowFunc, &ratioedOpts)
	return &TimingBareRatioHistogram{
		Registerable: th,
		timingBareRatioHistogramInner: timingBareRatioHistogramInner{
			under:       th,
			numerator:   opts.InitialValue,
			denominator: opts.InitialDenominator,
		}}
}

func (trh *timingBareRatioHistogramInner) Observe(numerator float64) {
	trh.numerator = numerator
	ratio := numerator / trh.denominator
	trh.under.Set(ratio)
}

func (trh *timingBareRatioHistogramInner) SetDenominator(denominator float64) {
	trh.denominator = denominator
	ratio := trh.numerator / denominator
	trh.under.Set(ratio)
}
