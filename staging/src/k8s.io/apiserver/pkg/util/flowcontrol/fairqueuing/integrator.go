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

package fairqueuing

import (
	"math"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
)

// Integrator computes the integral of some variable X over time as
// read from a particular clock.  The integral starts when the
// Integrator is created, and ends at the latest operation on the
// Integrator.
type Integrator interface {
	Set(float64) // set the value of X
	Add(float64) // add the given quantity to X
	GetResults() IntegratorResults

	// Return the results of integrating to now, and reset integration to start now
	Reset() IntegratorResults
}

// IntegratorResults holds statistical abstracts of the integration
type IntegratorResults struct {
	Duration  float64 //seconds
	Average   float64
	Deviation float64 //standard deviation: sqrt(avg((value-avg)^2))
	Min, Max  float64
}

type integrator struct {
	sync.Mutex
	clk       clock.PassiveClock
	lastTime  time.Time
	x         float64
	integrals [3]float64 // integral of x^0, x^1, and x^2
	min, max  float64
}

// NewIntegrator makes one that uses the given clock
func NewIntegrator(clk clock.PassiveClock) Integrator {
	return &integrator{
		clk:      clk,
		lastTime: clk.Now(),
	}
}

func (igr *integrator) Set(x float64) {
	igr.Lock()
	igr.setLocked(x)
	igr.Unlock()
}

func (igr *integrator) setLocked(x float64) {
	igr.updateLocked()
	igr.x = x
	if x < igr.min {
		igr.min = x
	}
	if x > igr.max {
		igr.max = x
	}
}

func (igr *integrator) Add(deltaX float64) {
	igr.Lock()
	igr.setLocked(igr.x + deltaX)
	igr.Unlock()
}

func (igr *integrator) updateLocked() {
	now := igr.clk.Now()
	dt := now.Sub(igr.lastTime).Seconds()
	igr.lastTime = now
	igr.integrals[0] += dt
	igr.integrals[1] += dt * igr.x
	igr.integrals[2] += dt * igr.x * igr.x
}

func (igr *integrator) GetResults() IntegratorResults {
	igr.Lock()
	defer func() { igr.Unlock() }()
	return igr.getResultsLocked()
}

func (igr *integrator) Reset() IntegratorResults {
	igr.Lock()
	defer func() { igr.Unlock() }()
	results := igr.getResultsLocked()
	igr.integrals = [3]float64{0, 0, 0}
	igr.min = igr.x
	igr.max = igr.x
	return results
}

func (igr *integrator) getResultsLocked() (results IntegratorResults) {
	igr.updateLocked()
	results.Duration = igr.integrals[0]
	if results.Duration <= 0 {
		results.Average = math.NaN()
		results.Deviation = math.NaN()
		return
	}
	results.Average = igr.integrals[1] / igr.integrals[0]
	// Deviation is sqrt( Integral( (x - xbar)^2 dt) / Duration )
	// = sqrt( Integral( x^2 + xbar^2 -2*x*xbar dt ) / Duration )
	// = sqrt( ( Integral( x^2 dt ) + Duration * xbar^2 - 2*xbar*Integral(x dt) ) / Duration)
	// = sqrt( Integral(x^2 dt)/Duration - xbar^2 )
	variance := igr.integrals[2]/igr.integrals[0] - results.Average*results.Average
	if variance > 0 {
		results.Deviation = math.Sqrt(variance)
	}
	results.Min, results.Max = igr.min, igr.max
	return
}
