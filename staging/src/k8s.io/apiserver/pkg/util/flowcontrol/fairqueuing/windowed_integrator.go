/*
Copyright 2020 The Kubernetes Authors.

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

package fairqueuing

import (
	"math"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
)

// WindowedIntegrator computes statistics about a variable X over the
// past few windows of time.  The window width and number of windows
// are configured characteristics.
type WindowedIntegrator interface {
	Set(float64) // set the value of X
	Add(float64) // add the given quantity to X

	// GetResults returns the statistics for the last few windows.
	// The windows are delineated on a fixed schedule, regardless of
	// when this method is called.  Thus, the latest window is likely
	// to get more data after this method is called.  The results are
	// a snapshot that the callee does not modify after returning.
	// The given slices are re-used if possible.
	GetResults(mins, maxs []float64) WindowedIntegratorResults
}

// WindowedIntegratorResults is about the behavior of X over the last
// few windows.  This low and high watermarks are reported for each
// window.  The other results concern all the windows together.
type WindowedIntegratorResults struct {
	// Min holds the low water marks for the windows.  Min[0] is from
	// the current window, Min[1] is for the window before that,
	// Min[2] is for the window before that, and so on.
	Min []float64

	// Max holds the high water marks for the windows.
	Max []float64

	// Duration is the number of seconds covered by the windows
	Duration float64

	// Average is the time-weighted average of X over the windows.
	Average float64

	// StandardDeviation is sqrt( average_over_windows( (X-Average)^2 ) )
	StandardDeviation float64

	// Integrals[i] is the integral of X^i since the creation of the integrator
	Integrals [3]float64
}

type windowedIntegrator struct {
	clk         clock.PassiveClock
	windowWidth time.Duration
	windows     []integratorWindow // circular buffer
	sync.Mutex
	currentWindow      int // the one currently accumulating new data
	oldestWindow       int // the oldest one holding data
	currentWindowStart time.Time
	lastTime           time.Time // time of last setting of X in the current window
	x                  float64

	// integrals since currentWindowStart of x^0, x^1, x^2.  That
	// covers at most 5 seconds, a little more then 2^32 nanoseconds.
	// Supposing concurrency is at most 2^10, headIntegrals[2] needs
	// at most a little over 2^52 bits of precision --- which it has.
	headIntegrals [3]float64

	// integrals from creation of this integrator to
	// currentWindowStart of x^0, x^1, x^2.  Regarding precision:
	// let's say we do not want to lose the fact that (x+1) rather
	// than (x) persisted for 2^-8 seconds.  With max x of 1000, that
	// difference takes about 10 bits at one instant.  A year is about
	// 2^25 seconds.  So we need about 2^43 bits of precision per
	// year.  With 53 bits in a float64, this should work for about a
	// thousand years.
	tailIntegrals [3]float64
}

type integratorWindow struct {
	integrals [3]float64 // integrals[i] is integral of X^i
	min, max  float64
}

// NewWindowedIntegrator makes one that uses the given clock
func NewWindowedIntegrator(clk clock.PassiveClock, windowWidth time.Duration, numWindows int) WindowedIntegrator {
	now := clk.Now()
	return &windowedIntegrator{
		clk:                clk,
		windowWidth:        windowWidth,
		windows:            make([]integratorWindow, numWindows),
		currentWindowStart: now,
		lastTime:           now,
	}
}

// NewWindowedIntegratorPair makes a pair using the given parameters
func NewWindowedIntegratorPair(clk clock.PassiveClock, windowWidth time.Duration, numWindows int) WindowedIntegratorPair {
	return WindowedIntegratorPair{
		RequestsWaiting:   NewWindowedIntegrator(clk, windowWidth, numWindows),
		RequestsExecuting: NewWindowedIntegrator(clk, windowWidth, numWindows),
	}
}

// Advance windows as necessary to get the given time in the current window
func (wi *windowedIntegrator) slideTo(now time.Time) {
	if now.Before(wi.currentWindowStart) {
		panic([]interface{}{now, wi})
	}
	for currentWindowEnd := wi.currentWindowStart.Add(wi.windowWidth); !now.Before(currentWindowEnd); currentWindowEnd = wi.currentWindowStart.Add(wi.windowWidth) {
		// need to close out the current window and start another
		wi.updateLocked(currentWindowEnd)
		wi.tailIntegrals[0] += wi.headIntegrals[0]
		wi.tailIntegrals[1] += wi.headIntegrals[1]
		wi.tailIntegrals[2] += wi.headIntegrals[2]
		wi.headIntegrals = [3]float64{0, 0, 0}
		wi.currentWindowStart = currentWindowEnd
		wi.currentWindow = (wi.currentWindow + 1) % len(wi.windows)
		if wi.currentWindow == wi.oldestWindow {
			wi.oldestWindow = (wi.oldestWindow + 1) % len(wi.windows)
		}
		wi.windows[wi.currentWindow] = integratorWindow{
			min: wi.x,
			max: wi.x,
		}
	}
}

// Update the current window to account for time advancing to the given value
func (wi *windowedIntegrator) updateLocked(now time.Time) {
	dt := now.Sub(wi.lastTime).Seconds()
	wi.lastTime = now
	iw := &wi.windows[wi.currentWindow]
	x := wi.x
	xdt := x * dt
	xxdt := x * x * dt
	iw.integrals[0] += dt
	iw.integrals[1] += xdt
	iw.integrals[2] += xxdt
	wi.headIntegrals[0] += dt
	wi.headIntegrals[1] += xdt
	wi.headIntegrals[2] += xxdt
}

func (wi *windowedIntegrator) GetResults(mins, maxs []float64) WindowedIntegratorResults {
	wi.Lock()
	defer wi.Unlock()
	now := wi.clk.Now()
	wi.slideTo(now)
	wi.updateLocked(now)
	windows := wi.windows
	sum := windows[wi.currentWindow]
	mins = append(mins[:0], sum.min)
	maxs = append(maxs[:0], sum.max)
	n := len(windows)
	for i := wi.currentWindow; i != wi.oldestWindow; {
		i = (i + n - 1) % n
		iw := &windows[i]
		mins = append(mins, iw.min)
		maxs = append(maxs, iw.max)
		sum.min = math.Min(sum.min, iw.min)
		sum.max = math.Max(sum.max, iw.max)
		sum.integrals[0] += iw.integrals[0]
		sum.integrals[1] += iw.integrals[1]
		sum.integrals[2] += iw.integrals[2]
	}
	for len(mins) < len(windows) {
		mins = append(mins, 0)
		maxs = append(maxs, 0)
	}
	var avg, stddev float64
	if sum.integrals[0] <= 0 {
		avg, stddev = math.NaN(), math.NaN()
	} else {
		avg = sum.integrals[1] / sum.integrals[0]
		variance := sum.integrals[2]/sum.integrals[0] - avg*avg
		if variance >= 0 {
			stddev = math.Sqrt(variance)
		} else {
			stddev = math.NaN()
		}
	}
	return WindowedIntegratorResults{
		Min:               mins,
		Max:               maxs,
		Duration:          sum.integrals[0],
		Average:           avg,
		StandardDeviation: stddev,
		Integrals: [3]float64{
			wi.headIntegrals[0] + wi.tailIntegrals[0],
			wi.headIntegrals[1] + wi.tailIntegrals[1],
			wi.headIntegrals[2] + wi.tailIntegrals[2],
		},
	}
}

func (wi *windowedIntegrator) setLocked(x float64) {
	now := wi.clk.Now()
	wi.slideTo(now)
	wi.updateLocked(now)
	wi.x = x
	iw := &wi.windows[wi.currentWindow]
	if x < iw.min {
		iw.min = x
	}
	if x > iw.max {
		iw.max = x
	}
}

func (wi *windowedIntegrator) Set(x float64) {
	wi.Lock()
	defer wi.Unlock()
	wi.setLocked(x)
}
func (wi *windowedIntegrator) Add(deltaX float64) {
	wi.Lock()
	defer wi.Unlock()
	wi.setLocked(wi.x + deltaX)
}
