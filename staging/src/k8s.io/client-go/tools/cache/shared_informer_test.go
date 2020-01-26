/*
Copyright 2017 The Kubernetes Authors.

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

package cache

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	fcache "k8s.io/client-go/tools/cache/testing"
)

const timeFmt = "15:04:05.999999999"

type testListener struct {
	clock             clock.PassiveClock
	lock              sync.RWMutex
	resyncPeriod      time.Duration
	expectedItemNames sets.String
	receivedItemNames []string
	name              string
}

func newTestListener(name string, clock clock.PassiveClock, resyncPeriod time.Duration, expected ...string) *testListener {
	l := &testListener{
		clock:             clock,
		resyncPeriod:      resyncPeriod,
		expectedItemNames: sets.NewString(expected...),
		name:              name,
	}
	return l
}

func (l *testListener) OnAdd(obj interface{}) {
	l.handle(obj)
}

func (l *testListener) OnUpdate(old, new interface{}) {
	l.handle(new)
}

func (l *testListener) OnDelete(obj interface{}) {
}

func (l *testListener) handle(obj interface{}) {
	key, _ := MetaNamespaceKeyFunc(obj)
	fmt.Printf("%s %s: handle: %v\n", l.clock.Now().Format(timeFmt), l.name, key)
	l.lock.Lock()
	defer l.lock.Unlock()

	objectMeta, _ := meta.Accessor(obj)
	l.receivedItemNames = append(l.receivedItemNames, objectMeta.GetName())
}

func (l *testListener) ok() bool {
	fmt.Printf("%s %s: polling\n", l.clock.Now().Format(timeFmt), l.name)
	err := wait.PollImmediate(100*time.Millisecond, 2*time.Second, func() (bool, error) {
		if l.satisfiedExpectations() {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return false
	}

	// wait just a bit to allow any unexpected stragglers to come in
	fmt.Printf("%s %s: sleeping\n", l.clock.Now().Format(timeFmt), l.name)
	time.Sleep(1 * time.Second)
	fmt.Printf("%s %s: final check\n", l.clock.Now().Format(timeFmt), l.name)
	return l.satisfiedExpectations()
}

func (l *testListener) satisfiedExpectations() bool {
	l.lock.RLock()
	defer l.lock.RUnlock()

	return sets.NewString(l.receivedItemNames...).Equal(l.expectedItemNames)
}

func TestListenerResyncPeriods(t *testing.T) {
	for _, defaultCheckPeriodMsec := range []int{1000, 1300, 1500, 5000} {
		defaultCheckPeriod := time.Millisecond * time.Duration(defaultCheckPeriodMsec)
		t.Run(fmt.Sprintf("defaultCheckPeriod=%s", defaultCheckPeriod), func(t *testing.T) { checkListenerResyncPeriods(t, defaultCheckPeriod) })
	}
}

func checkListenerResyncPeriods(t *testing.T, defaultCheckPeriod time.Duration) {
	sfx := fmt.Sprintf("-%s", defaultCheckPeriod)
	name1 := "pod1" + sfx
	name2 := "pod2" + sfx
	// source simulates an apiserver object endpoint.
	source := fcache.NewFakeControllerSource()
	source.Add(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name1}})
	source.Add(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name2}})

	// create the shared informer and resync every 1s
	informer := NewSharedInformer(source, &v1.Pod{}, defaultCheckPeriod).(*sharedIndexInformer)

	clock := clock.NewFakeClock(time.Now())
	informer.clock = clock
	informer.processor.clock = clock

	// listener 1, never resync
	listener1 := newTestListener("listener1"+sfx, clock, 0, name1, name2)
	informer.AddEventHandlerWithResyncPeriod(listener1, listener1.resyncPeriod)

	// listener 2, resync every 2s
	listener2 := newTestListener("listener2"+sfx, clock, 2*time.Second, name1, name2)
	informer.AddEventHandlerWithResyncPeriod(listener2, listener2.resyncPeriod)

	// listener 3, resync every 3s
	listener3 := newTestListener("listener3"+sfx, clock, 3*time.Second, name1, name2)
	informer.AddEventHandlerWithResyncPeriod(listener3, listener3.resyncPeriod)
	listeners := []*testListener{listener1, listener2, listener3}

	expectedCheckPeriod := defaultCheckPeriod
	if expectedCheckPeriod > 2*time.Second {
		expectedCheckPeriod = 2 * time.Second
	}
	resync2Lat := time.Duration(math.Ceil(float64(2*time.Second)/float64(expectedCheckPeriod))) * expectedCheckPeriod
	resync3Lat := time.Duration(math.Ceil(float64(3*time.Second)/float64(expectedCheckPeriod))) * expectedCheckPeriod
	resync1Lat := resync2Lat / 2

	stop := make(chan struct{})
	defer close(stop)

	go informer.Run(stop)

	// ensure all listeners got the initial List
	for _, listener := range listeners {
		if !listener.ok() {
			t.Errorf("%s: expected %v, got %v", listener.name, listener.expectedItemNames, listener.receivedItemNames)
		}
	}

	// reset
	for _, listener := range listeners {
		listener.receivedItemNames = []string{}
	}

	// advance fake time halfway to the first resync time
	clock.Step(resync1Lat)

	// wait a bit to give errant items a chance to arrive
	time.Sleep(1 * time.Second)

	// make sure no listener got anything
	for _, listener := range listeners {
		if len(listener.receivedItemNames) != 0 {
			t.Errorf("%s: should not have resynced (got %v)", listener.name, listener.receivedItemNames)
		}
	}

	// advance fake time the rest of the way to the first resync time
	clock.Step(resync1Lat)

	// make sure listener2 got the resync
	if !listener2.ok() {
		t.Errorf("%s: expected %v, got %v", listener2.name, listener2.expectedItemNames, listener2.receivedItemNames)
	}

	// wait a bit to give errant items a chance to go to 1 and 3
	time.Sleep(1 * time.Second)

	// make sure listener1 got nothing
	if len(listener1.receivedItemNames) != 0 {
		t.Errorf("listener1: should not have resynced (got %d)", len(listener1.receivedItemNames))
	}

	if resync3Lat == resync2Lat { // listener3 should have gotten notified too
		if !listener3.ok() {
			t.Errorf("%s: expected %v, got %v", listener3.name, listener3.expectedItemNames, listener3.receivedItemNames)
		}
		return // we are done in this case
	}

	if len(listener3.receivedItemNames) != 0 {
		t.Errorf("listener3: should not have resynced (got %d)", len(listener3.receivedItemNames))
	}

	// reset
	for _, listener := range listeners {
		listener.receivedItemNames = []string{}
	}

	// advance fake clock to the time when listener3 should get a resync
	clock.Step(resync3Lat - resync2Lat)

	// make sure listener3 got the resync
	if !listener3.ok() {
		t.Errorf("%s: expected %v, got %v", listener3.name, listener3.expectedItemNames, listener3.receivedItemNames)
	}

	// wait a bit to give errant items a chance to go to 1 and 2
	time.Sleep(1 * time.Second)

	// make sure listener1 got nothing
	if len(listener1.receivedItemNames) != 0 {
		t.Errorf("listener1: should not have resynced (got %d)", len(listener1.receivedItemNames))
	}

	if resync3Lat >= 2*resync2Lat { // listener2 should have gotten synced again
		if !listener2.ok() {
			t.Errorf("%s: expected %v, got %v", listener2.name, listener2.expectedItemNames, listener2.receivedItemNames)
		}
	} else { // make sure listener2 got nothing
		if len(listener2.receivedItemNames) != 0 {
			t.Errorf("listener2: should not have resynced (got %d)", len(listener2.receivedItemNames))
		}
	}
}

// verify that https://github.com/kubernetes/kubernetes/issues/59822 is fixed
func TestSharedInformerInitializationRace(t *testing.T) {
	source := fcache.NewFakeControllerSource()
	informer := NewSharedInformer(source, &v1.Pod{}, 1*time.Second).(*sharedIndexInformer)
	listener := newTestListener("raceListener", clock.RealClock{}, 0)

	stop := make(chan struct{})
	go informer.AddEventHandlerWithResyncPeriod(listener, listener.resyncPeriod)
	go informer.Run(stop)
	close(stop)
}

// TestSharedInformerWatchDisruption simulates a watch that was closed
// with updates to the store during that time. We ensure that handlers with
// resync and no resync see the expected state.
func TestSharedInformerWatchDisruption(t *testing.T) {
	// source simulates an apiserver object endpoint.
	source := fcache.NewFakeControllerSource()

	source.Add(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", UID: "pod1"}})
	source.Add(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod2", UID: "pod2"}})

	// create the shared informer and resync every 1s
	informer := NewSharedInformer(source, &v1.Pod{}, 1*time.Second).(*sharedIndexInformer)

	clock := clock.NewFakeClock(time.Now())
	informer.clock = clock
	informer.processor.clock = clock

	// listener, never resync
	listenerNoResync := newTestListener("listenerNoResync", clock, 0, "pod1", "pod2")
	informer.AddEventHandlerWithResyncPeriod(listenerNoResync, listenerNoResync.resyncPeriod)

	listenerResync := newTestListener("listenerResync", clock, 1*time.Second, "pod1", "pod2")
	informer.AddEventHandlerWithResyncPeriod(listenerResync, listenerResync.resyncPeriod)
	listeners := []*testListener{listenerNoResync, listenerResync}

	stop := make(chan struct{})
	defer close(stop)

	go informer.Run(stop)

	for _, listener := range listeners {
		if !listener.ok() {
			t.Errorf("%s: expected %v, got %v", listener.name, listener.expectedItemNames, listener.receivedItemNames)
		}
	}

	// Add pod3, bump pod2 but don't broadcast it, so that the change will be seen only on relist
	source.AddDropWatch(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod3", UID: "pod3"}})
	source.ModifyDropWatch(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod2", UID: "pod2"}})

	// Ensure that nobody saw any changes
	for _, listener := range listeners {
		if !listener.ok() {
			t.Errorf("%s: expected %v, got %v", listener.name, listener.expectedItemNames, listener.receivedItemNames)
		}
	}

	for _, listener := range listeners {
		listener.receivedItemNames = []string{}
	}

	listenerNoResync.expectedItemNames = sets.NewString("pod1", "pod2", "pod3")
	listenerResync.expectedItemNames = sets.NewString("pod1", "pod2", "pod3")

	// This calls shouldSync, which deletes noResync from the list of syncingListeners
	clock.Step(1 * time.Second)

	// Simulate a connection loss (or even just a too-old-watch)
	source.ResetWatch()

	for _, listener := range listeners {
		if !listener.ok() {
			t.Errorf("%s: expected %v, got %v", listener.name, listener.expectedItemNames, listener.receivedItemNames)
		}
	}
}
