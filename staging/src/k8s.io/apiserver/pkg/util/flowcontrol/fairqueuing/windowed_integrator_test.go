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
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
)

func TestWindowedIntegrator(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	wi := NewWindowedIntegrator(clk, time.Millisecond*3500, 4)
	wi.Set(2)
	clk.Step(2 * time.Second)
	wi.Add(2)
	clk.Step(2 * time.Second)
	wi.Add(2)
	clk.Step(2 * time.Second)
	r1 := wi.GetResults(nil, nil)
	if e, a := []float64{4, 0, 0, 0}, r1.Min; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected Min %#+v, got %#+v", e, a)
	}
	if e, a := []float64{6, 4, 0, 0}, r1.Max; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected Max %#+v, got %#+v", e, a)
	}
	if e, a := (Integrals{6, 24, 112}), r1.Integrals; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected integrals %#+v, got %#+v", e, a)
	}
	for i := 0; i < 6; i++ {
		wi.Add(2)
		clk.Step(2 * time.Second)
	}
	// at elapsed time T, X = floor(T/2sec)*2 + 2
	// time has advanced to 18.
	// curnt win is [17.5, 21.0); min=18, max=18
	// cur-1 win is [14.0, 17.5); min=14, max=18
	// cur-2 win is [10.5, 14.0); min=12, max=14
	// cur-3 win is [07.0, 10.5); min=08, max=12
	// X was:
	// - 08 (avg - 5 5/11) from 07 to 08
	// - 10 (avg - 3 5/11) from 08 to 10
	// - 12 (avg - 1 5/11) from 10 to 12
	// - 14 (avg +   6/11) from 12 to 14
	// - 16 (avg + 2 6/11) from 14 to 16
	// - 18 (avg + 4 6/11) from 16 to 18
	r2 := wi.GetResults(r1.Min, r1.Max)
	if e, a := []float64{18, 14, 12, 8}, r2.Min; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected Min %#+v, got %#+v", e, a)
	}
	if e, a := []float64{18, 18, 14, 12}, r2.Max; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected Max %#+v, got %#+v", e, a)
	}
	if e, a := (Integrals{18, 180, 2280}), r2.Integrals; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected integrals %#+v, got %#+v", e, a)
	}
}
