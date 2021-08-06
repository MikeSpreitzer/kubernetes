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

package promise

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	testclock "k8s.io/apiserver/pkg/util/flowcontrol/fairqueuing/clock/testing"
	"k8s.io/klog/v2"
)

func TestMain(m *testing.M) {
	klog.InitFlags(nil)
	os.Exit(m.Run())
}

func TestCountingWriteOnceSet(t *testing.T) {
	oldTime := time.Now()
	cval := &oldTime
	doneCh := make(chan struct{})
	now := time.Now()
	clock, counter := testclock.NewFakeEventClock(now, 0, nil)
	var lock sync.Mutex
	wr := NewCountingWriteOnce(counter, &lock, nil, doneCh, cval)
	gots := make(chan interface{}, 1)
	counter.Add(1)
	go func() {
		gots <- wr.Get()
		counter.Add(-1)
	}()
	clock.Run(nil)
	select {
	case <-gots:
		t.Error("Get returned before Set")
	case <-time.After(5 * time.Second):
		t.Log("Good: Get did not return yet")
	}
	aval := &now
	func() {
		lock.Lock()
		defer lock.Unlock()
		if !wr.Set(aval) {
			t.Error("Set() returned false")
		}
	}()
	clock.Run(nil)
	select {
	case gotVal := <-gots:
		t.Logf("Got %#+v", gotVal)
		if gotVal != aval {
			t.Error("Get did not return what was Set")
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Get did not return after Set")
	}
	counter.Add(1)
	go func() {
		gots <- wr.Get()
		counter.Add(-1)
	}()
	clock.Run(nil)
	select {
	case gotVal := <-gots:
		t.Logf("Got %#+v", gotVal)
		if gotVal != aval {
			t.Error("Second Get did not return what was Set")
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Second Get did not return")
	}
	later := time.Now()
	bval := &later
	func() {
		lock.Lock()
		defer lock.Unlock()
		if wr.Set(bval) {
			t.Error("second Set() returned true")
		}
	}()
	if wr.Get() != aval {
		t.Error("Get() after second Set returned wrong value")
	}
	counter.Add(1)
	close(doneCh)
	time.Sleep(5 * time.Second) // give it a chance to misbehave
	if wr.Get() != aval {
		t.Error("Get() after cancel returned wrong value")
	}
	close(gots)
}
func TestCountingWriteOnceCancel(t *testing.T) {
	oldTime := time.Now()
	cval := &oldTime
	clock, counter := testclock.NewFakeEventClock(oldTime, 0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	var lock sync.Mutex
	wr := NewCountingWriteOnce(counter, &lock, nil, ctx.Done(), cval)
	gots := make(chan interface{}, 1)
	counter.Add(1)
	go func() {
		gots <- wr.Get()
		counter.Add(-1)
	}()
	clock.Run(nil)
	select {
	case <-gots:
		t.Error("Get returned before Set")
	case <-time.After(5 * time.Second):
		t.Log("Good: Get did not return yet")
	}
	counter.Add(1) // account for unblocking the receive
	cancel()
	clock.Run(nil)
	select {
	case gotVal := <-gots:
		t.Logf("Got %#+v", gotVal)
		if gotVal != cval {
			t.Error("Get did not return cval after cancel")
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Get did not return after cancel")
	}
	counter.Add(1)
	go func() {
		gots <- wr.Get()
		counter.Add(-1)
	}()
	clock.Run(nil)
	select {
	case gotVal := <-gots:
		t.Logf("Got %#+v", gotVal)
		if gotVal != cval {
			t.Error("Second Get did not return cval")
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Second Get did not return")
	}
	later := time.Now()
	bval := &later
	if wr.Set(bval) {
		t.Error("Set() after cancel returned true")
	}
	if wr.Get() != cval {
		t.Error("Get() after Cancel then Set returned wrong value")
	}
	close(gots)
}

func TestCountingWriteOnceInitial(t *testing.T) {
	oldTime := time.Now()
	cval := &oldTime
	clock, counter := testclock.NewFakeEventClock(oldTime, 0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	var lock sync.Mutex
	now := time.Now()
	aval := &now
	wr := NewCountingWriteOnce(counter, &lock, aval, ctx.Done(), cval)
	gots := make(chan interface{}, 1)
	counter.Add(1)
	go func() {
		gots <- wr.Get()
		counter.Add(-1)
	}()
	clock.Run(nil)
	select {
	case gotVal := <-gots:
		t.Logf("Got %#+v", gotVal)
		if gotVal != aval {
			t.Error("Get returned wrong value")
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Get did not return")
	}
	later := time.Now()
	bval := &later
	if wr.Set(bval) {
		t.Error("Set of initialized promise returned true")
	}
	if wr.Get() != aval {
		t.Error("Second Get of initialized promise did not return initial value")
	}
	counter.Add(1) // account for unblocking receive
	cancel()
	time.Sleep(5 * time.Second) // give it a chance to misbehave
	if wr.Get() != aval {
		t.Error("Get of initialized promise after cancel did not return initial value")
	}
	close(gots)
}
