/*
Copyright 2021 The Kubernetes Authors.

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

package flowcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	flowcontrol "k8s.io/api/flowcontrol/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	utilfc "k8s.io/apiserver/pkg/util/flowcontrol"
	fqtesting "k8s.io/apiserver/pkg/util/flowcontrol/fairqueuing/testing"
	"k8s.io/apiserver/pkg/util/flowcontrol/metrics"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

const timeFmt = "2006-01-02T15:04:05.999"

/* fightTest configures a test of how API Priority and Fairness config
   controllers fight when they disagree on how to set FlowSchemaStatus.
   In particular, they set the condition that indicates integrity of
   the reference to the PriorityLevelConfiguration.  The scenario tested is
   two teams of controllers, where the controllers in one team set the
   condition normally and the controllers in the other team set the condition
   to the opposite value.

   This is a behavioral test: it instantiates these controllers and runs them
   almost normally.  But instead of running all the controllers concurrently,
   with their informers connected to their workers through their workqueues,
   the informers and workers are decoupled.  The test synchronously invokes
   the workers one at a time.  Before advancing to the next iteration,
   the test waits in real time for each informer to deliver notifications
   of what the workers wrote.  The workers read a fake clock, so that the
   test can exactly control how that time advances.  After every worker run,
   the test evaluates whether that worker has been sufficiently reticent
   about updating FlowSchemaStatus.

   This test disables the usual controller in the kube-apiserver so that
   it does not modify FlowSchemaStatus; such modifications could confuse
   the test logic that waits for notifications of the ResourceVersions
   created by the test workers.
*/
type fightTest struct {
	t              *testing.T
	ctx            context.Context
	loopbackConfig *rest.Config
	teamSize       int
	stopCh         chan struct{}
	now            time.Time
	clk            *clock.FakeClock
	ctlrs          map[bool][]utilfc.TestableInterface

	countsMutex sync.Mutex

	// writeCounts maps FlowSchema.Name to fieldManager to number of writes
	writeCounts map[string]map[string]int
}

func newFightTest(t *testing.T, loopbackConfig *rest.Config, teamSize int) *fightTest {
	now := time.Now()
	ft := &fightTest{
		t:              t,
		ctx:            context.Background(),
		loopbackConfig: loopbackConfig,
		teamSize:       teamSize,
		stopCh:         make(chan struct{}),
		now:            now,
		clk:            clock.NewFakeClock(now),
		ctlrs: map[bool][]utilfc.TestableInterface{
			false: make([]utilfc.TestableInterface, teamSize),
			true:  make([]utilfc.TestableInterface, teamSize)},
		writeCounts: map[string]map[string]int{},
	}
	return ft
}

func (ft *fightTest) createMainInformer() {
	myConfig := rest.CopyConfig(ft.loopbackConfig)
	myConfig = rest.AddUserAgent(myConfig, "audience")
	myClientset := clientset.NewForConfigOrDie(myConfig)
	informerFactory := informers.NewSharedInformerFactory(myClientset, 0)
	inf := informerFactory.Flowcontrol().V1beta1().FlowSchemas().Informer()
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			fs := obj.(*flowcontrol.FlowSchema)
			ft.countWrite(fs)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			fs := newObj.(*flowcontrol.FlowSchema)
			ft.countWrite(fs)
		},
	})
	go inf.Run(ft.stopCh)
	if !cache.WaitForCacheSync(ft.stopCh, inf.HasSynced) {
		ft.t.Errorf("Failed to sync main informer cache")
	}
}

func (ft *fightTest) countWrite(fs *flowcontrol.FlowSchema) {
	ft.countsMutex.Lock()
	defer ft.countsMutex.Unlock()
	fsWrites := ft.writeCounts[fs.Name]
	if fsWrites == nil {
		fsWrites = map[string]int{}
		ft.writeCounts[fs.Name] = fsWrites
	}
	writer := getWriter(ft.t, fs.ManagedFields)
	if writer != "" {
		ft.t.Logf("Counting write from %q to FlowSchema %s", writer, fs.Name)
		fsWrites[writer]++
	} else {
		ft.t.Logf("Found no write to Dangling condition in %#+v", *fs)
	}
}

func getWriter(t *testing.T, mf []metav1.ManagedFieldsEntry) string {
	for _, mfe := range mf {
		if mfe.FieldsType != "FieldsV1" {
			t.Errorf("Entry %#+v has unexpected FieldsType", mfe)
			continue
		}
		if mfe.FieldsV1 == nil {
			t.Errorf("Entry %#+v has nil FieldsV1", mfe)
			continue
		}
		var fieldsIfc interface{}
		err := json.Unmarshal(mfe.FieldsV1.Raw, &fieldsIfc)
		if err != nil {
			t.Errorf("JSON parsing error for entry %#+v", mfe)
		}
		if fieldsMap, fieldsIsMap := fieldsIfc.(map[string]interface{}); fieldsIsMap {
			if statusIfc, gotStatus := fieldsMap["f:status"]; gotStatus {
				statusMap := statusIfc.(map[string]interface{})
				if condsIfc, gotConds := statusMap["f:conditions"]; gotConds {
					condsMap := condsIfc.(map[string]interface{})
					if _, gotDangle := condsMap["k:{\"type\":\"Dangling\"}"]; gotDangle {
						return mfe.Manager
					}
				}
			}
			t.Logf("Entry.FieldsV1 %s is not about the Dangling condition", string(mfe.FieldsV1.Raw))
		} else {
			t.Logf("Fields of entry %#+v is not a map", mfe)
		}
	}
	return ""
}

func (ft *fightTest) createController(invert bool, i int) {
	fieldMgr := fmt.Sprintf("testController%d%v", i, invert)
	myConfig := rest.CopyConfig(ft.loopbackConfig)
	myConfig = rest.AddUserAgent(myConfig, fieldMgr)
	myClientset := clientset.NewForConfigOrDie(myConfig)
	fcIfc := myClientset.FlowcontrolV1beta1()
	informerFactory := informers.NewSharedInformerFactory(myClientset, 0)
	foundToDangling := func(found bool) bool { return !found }
	if invert {
		foundToDangling = func(found bool) bool { return found }
	}
	ctlr := utilfc.NewTestable(utilfc.TestableConfig{
		Name:                   fieldMgr,
		FoundToDangling:        foundToDangling,
		Clock:                  clock.RealClock{},
		AsFieldManager:         fieldMgr,
		InformerFactory:        informerFactory,
		FlowcontrolClient:      fcIfc,
		ServerConcurrencyLimit: 200,             // server concurrency limit
		RequestWaitLimit:       time.Minute / 4, // request wait limit
		ObsPairGenerator:       metrics.PriorityLevelConcurrencyObserverPairGenerator,
		QueueSetFactory:        fqtesting.NewNoRestraintFactory(),
	})
	ft.ctlrs[invert][i] = ctlr
	informerFactory.Start(ft.stopCh)
	go ctlr.Run(ft.stopCh)
}

func (ft *fightTest) evaluate(tBeforeCreate, tAfterCreate time.Time) {
	tBeforeLock := time.Now()
	ft.countsMutex.Lock()
	defer ft.countsMutex.Unlock()
	tAfterLock := time.Now()
	minFightSecs := tBeforeLock.Sub(tAfterCreate).Seconds()
	maxFightSecs := tAfterLock.Sub(tBeforeCreate).Seconds()
	minTotalWrites := int(minFightSecs / 10)
	maxWritesPerWriter := 6 * int(math.Ceil(maxFightSecs/60))
	maxTotalWrites := (1 + ft.teamSize*2) * maxWritesPerWriter
	for flowSchemaName, flowSchemaCounts := range ft.writeCounts {
		var ourTotalWrites, grandTotalWrites int
		for writer, writes := range flowSchemaCounts {
			if writer == utilfc.ConfigConsumerAsFieldManager || strings.HasPrefix(writer, "testController") {
				ourTotalWrites += writes
				if writes > maxWritesPerWriter {
					ft.t.Errorf("Writer %q made %d writes to FlowSchema %s but should have made no more than %d from %s to %s", writer, writes, flowSchemaName, maxWritesPerWriter, tBeforeCreate, tAfterLock)
				}
			}
			grandTotalWrites += writes
		}
		if grandTotalWrites < minTotalWrites {
			ft.t.Errorf("There were a total of %d writes to FlowSchema %s but there should have been at least %d from %s to %s", grandTotalWrites, flowSchemaName, minTotalWrites, tAfterCreate, tBeforeLock)
		}
		if ourTotalWrites > maxTotalWrites {
			ft.t.Errorf("Controllers made a total of %d writes to FlowSchema %s but there should have been no more than %d from %s to %s", ourTotalWrites, flowSchemaName, maxTotalWrites, tBeforeCreate, tAfterLock)
		}
		ft.t.Logf("ourTotalWrites=%d, grandTotalWrites=%d for FlowSchema %s over %v, %v seconds", ourTotalWrites, grandTotalWrites, flowSchemaName, minFightSecs, maxFightSecs)
	}
}
func TestConfigConsumerFight(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, genericfeatures.APIPriorityAndFairness, true)()
	_, loopbackConfig, closeFn := setup(t, 100, 100)
	defer closeFn()
	const teamSize = 3
	ft := newFightTest(t, loopbackConfig, teamSize)
	tBeforeCreate := time.Now()
	ft.createMainInformer()
	ft.foreach(ft.createController)
	tAfterCreate := time.Now()
	time.Sleep(110 * time.Second)
	ft.evaluate(tBeforeCreate, tAfterCreate)
	close(ft.stopCh)
}

func (ft *fightTest) foreach(visit func(invert bool, i int)) {
	for i := 0; i < ft.teamSize; i++ {
		// The order of the following enumeration is not deterministic,
		// and that is good.
		invert := rand.Intn(2) == 0
		visit(invert, i)
		visit(!invert, i)
	}
}

func peel(obj interface{}) interface{} {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		return d.Obj
	}
	return obj
}

// durationMin computes the minimum of two time.Duration values
func durationMin(x, y time.Duration) time.Duration {
	if x < y {
		return x
	}
	return y
}
