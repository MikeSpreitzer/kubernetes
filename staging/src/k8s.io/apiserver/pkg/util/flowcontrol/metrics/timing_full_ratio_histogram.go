package metrics

import (
	"sync"
	"time"

	compbasemetrics "k8s.io/component-base/metrics"
)

// TimingRatioHistogram is essentially a gauge for a ratio where the client
// independently controls the numerator and denominator.
// When scraped it produces a histogram of samples of the ratio
// taken at the end of every nanosecond.
// `*TimingRatioHistogram` implements both Registerable and RatioedObserver.
type TimingFullRatioHistogram struct {
	// The implementation is layered on TimingHistogram,
	// adding the division by an occasionally adjusted denominator.

	// Registerable is the registerable aspect.
	// That is the registerable aspect of the underlying TimingHistogram.
	compbasemetrics.Registerable

	// timingRatioHistogramInner implements the RatioedObsserver aspect.
	timingFullRatioHistogramInner
}

// timingRatioHistogramInner implements the instrumentation aspect
type timingFullRatioHistogramInner struct {
	under compbasemetrics.GaugeMetric
	sync.Mutex
	numerator, denominator float64
}

var _ RatioedObserver = &timingFullRatioHistogramInner{}
var _ RatioedObserver = &TimingFullRatioHistogram{}
var _ compbasemetrics.Registerable = &TimingFullRatioHistogram{}

// NewTestableTimingHistogram adds injection of the clock
func NewTestableTimingFullRatioHistogram(nowFunc func() time.Time, opts *TimingRatioHistogramOpts) *TimingFullRatioHistogram {
	ratioedOpts := opts.TimingHistogramOpts
	ratioedOpts.InitialValue /= opts.InitialDenominator
	th := compbasemetrics.NewTestableTimingHistogram(nowFunc, &ratioedOpts)
	return &TimingFullRatioHistogram{
		Registerable: th,
		timingFullRatioHistogramInner: timingFullRatioHistogramInner{
			under:       th,
			numerator:   opts.InitialValue,
			denominator: opts.InitialDenominator,
		}}
}

func (trh *timingFullRatioHistogramInner) Observe(numerator float64) {
	trh.Lock()
	defer trh.Unlock()
	trh.numerator = numerator
	ratio := numerator / trh.denominator
	trh.under.Set(ratio)
}

func (trh *timingFullRatioHistogramInner) SetDenominator(denominator float64) {
	trh.Lock()
	defer trh.Unlock()
	trh.denominator = denominator
	ratio := trh.numerator / denominator
	trh.under.Set(ratio)
}
