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

package filterconfig

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	fcv1a1 "k8s.io/api/flowcontrol/v1alpha1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	fcboot "k8s.io/apiserver/pkg/apis/flowcontrol/bootstrap"
	fmtv1a1 "k8s.io/apiserver/pkg/apis/flowcontrol/v1alpha1"
	"k8s.io/client-go/informers"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog"
)

func TestMain(m *testing.M) {
	klog.InitFlags(nil)
	os.Exit(m.Run())
}

func TestDigestConfig(t *testing.T) {
	rngOuter := rand.New(rand.NewSource(1234567890123456789))
	for i := 1; i <= 3; i++ {
		rng := rand.New(rand.NewSource(int64(rngOuter.Uint64())))
		clientset := clientsetfake.NewSimpleClientset()
		informerFactory := informers.NewSharedInformerFactory(clientset, 0)
		flowcontrolClient := clientset.FlowcontrolV1alpha1()
		ctl := NewTestableController(
			informerFactory,
			flowcontrolClient,
			100,         // server concurrency limit
			time.Minute, // request wait limit
			noRestraintQSF,
		).(*configController)
		t.Run(fmt.Sprintf("trial%d:", i), func(t *testing.T) {
			oldPLStates := map[string]*priorityLevelState{}
			trialName := fmt.Sprintf("trial%d-0", i)
			_, newGoodPLMap, newGoodPLNames, newBadPLNames := genPLs(rng, trialName, 0)
			_, newGoodFSMap, newFSDigestses := genFSs(t, rng, trialName, newGoodPLNames, newBadPLNames, 0)
			for j := 0; ; {
				// Check that the latest digestion did the right thing
				newMandBitsFS := seekMandFSs(newGoodFSMap)
				expectedPLNames := newGoodPLNames.Union(sets.StringKeySet(oldPLStates))
				expectedPLNames = expectedPLNames.Union(sets.NewString(
					fcv1a1.PriorityLevelConfigurationNameExempt,
					fcv1a1.PriorityLevelConfigurationNameCatchAll))
				if e, a := expectedPLNames, sets.StringKeySet(ctl.priorityLevelStates); !e.Equal(a) {
					t.Errorf("Fail at %s: e=%v, a=%v", trialName, e, a)
				}
				if e, a := len(newGoodFSMap)+2-newMandBitsFS.count(), len(ctl.flowSchemas); e != a {
					t.Errorf("Fail at %s: e=%v, a=%v", trialName, e, a)
				}
				for plName := range expectedPLNames {
					plState := ctl.priorityLevelStates[plName]
					checkNewPLState(t, trialName, plName, plState, oldPLStates, newGoodPLMap)
				}
				for _, fs := range ctl.flowSchemas {
					checkNewFS(t, ctl, trialName, fs, newGoodFSMap, newFSDigestses[fs.Name])
				}

				j++
				if j > 3 {
					break
				}

				// Calculate expected survivors
				oldPLStates = map[string]*priorityLevelState{}
				for oldPLName, oldPLState := range ctl.priorityLevelStates {
					if oldPLName == fcv1a1.PriorityLevelConfigurationNameExempt || oldPLName == fcv1a1.PriorityLevelConfigurationNameCatchAll || oldPLState.queues != nil && !(oldPLState.quiescing && oldPLState.queues.IsIdle()) {
						oldState := *oldPLState
						oldPLStates[oldPLName] = &oldState
					}
				}

				// Now create a new config and digest it
				trialName := fmt.Sprintf("trial%d-%d", i, j)
				var newPLs []*fcv1a1.PriorityLevelConfiguration
				var newFSs []*fcv1a1.FlowSchema
				newPLs, newGoodPLMap, newGoodPLNames, newBadPLNames = genPLs(rng, trialName, 1+rng.Intn(3))
				newFSs, newGoodFSMap, newFSDigestses = genFSs(t, rng, trialName, newGoodPLNames, newBadPLNames, 1+rng.Intn(5))

				for _, newPL := range newPLs {
					t.Logf("For %s, digesting newPL=%#+v", trialName, newPL)
				}
				for _, newFS := range newFSs {
					t.Logf("For %s, digesting newFS=%#+v", trialName, newFS)
				}

				ctl.digestConfigObjects(newPLs, newFSs)
			}
		})
	}
}

func checkNewPLState(t *testing.T, trialName, plName string, plState *priorityLevelState, oldPLStates map[string]*priorityLevelState, newGoodPLMap map[string]*fcv1a1.PriorityLevelConfiguration) {
	var expectedSpec *fcv1a1.PriorityLevelConfigurationSpec
	pl, inNew := newGoodPLMap[plName]
	if inNew {
		expectedSpec = &pl.Spec
	}
	ost, inOld := oldPLStates[plName]
	if expectedSpec == nil && inOld {
		expectedSpec = &ost.config
	}
	var isMand bool
	for _, mpl := range fcboot.MandatoryPriorityLevelConfigurations {
		if plName == mpl.Name {
			isMand = true
			if expectedSpec == nil {
				expectedSpec = &mpl.Spec
			}
		}
	}
	if expectedSpec == nil {
		t.Errorf("Fail at %s: Inexplicable entry %q %#+v", trialName, plName, plState)
		return
	}
	if plState == nil {
		t.Errorf("Fail at %s: missing new priorityLevelState for %s", trialName, plName)
		return
	}
	if e, a := *expectedSpec, plState.config; e != a {
		t.Errorf("Fail at %s: e=%#+v, a=%#+v", trialName, e, a)
	}
	isExempt := expectedSpec.Type == fcv1a1.PriorityLevelEnablementExempt
	if e, a := isExempt, (plState.qsCompleter == nil); e != a {
		t.Errorf("Fail at %s: e=%v, a=%v", trialName, e, a)
	}
	if e, a := isExempt, (plState.queues == nil); e != a {
		t.Errorf("Fail at %s: e=%v, a=%v", trialName, e, a)
	}
	if inOld {
		if e, a := ost.queues, plState.queues; e != a {
			t.Errorf("Fail at %s: e=%p, a=%p", trialName, e, a)
		}
	}
	if e, a := !(inNew || isMand), plState.quiescing; e != a {
		t.Errorf("Fail at %s: e=%v, a=%v", trialName, e, a)
	}
	if e, a := 0, plState.numPending; e != a {
		t.Errorf("Fail at %s: e=%v, a=%v", trialName, e, a)
	}
}

func checkNewFS(t *testing.T, ctl *configController, trialName string, fs *fcv1a1.FlowSchema, newGoodFSMap map[string]*fcv1a1.FlowSchema, digests *flowSchemaDigests) {
	orig := newGoodFSMap[fs.Name]
	for _, mfs := range fcboot.MandatoryFlowSchemas {
		if fs.Name == mfs.Name {
			if orig == nil {
				orig = mfs
			}
		}
	}
	if orig == nil {
		t.Errorf("Fail at %s: Inexplicable entry %#+v", trialName, *fs)
		return
	}
	if e, a := orig.Spec, fs.Spec; !apiequality.Semantic.DeepEqual(e, a) {
		t.Errorf("Fail at %s: e=%s, a=%s", trialName, fmtv1a1.FmtFlowSchemaSpec(&e), fmtv1a1.FmtFlowSchemaSpec(&a))
	}
	if digests != nil {
		for _, rd := range digests.matches {
			matchFSName, matchDistMethod, matchPLName, startFn := ctl.Match(rd)
			t.Logf("Considering FlowSchema %s, Match(%#+v) => %q, %#+v, %q, %v", fs.Name, rd, matchFSName, matchDistMethod, matchPLName, startFn)
			if matchFSName == orig.Name {
				if e, a := orig.Spec.DistinguisherMethod, matchDistMethod; !apiequality.Semantic.DeepEqual(e, a) {
					t.Errorf("Fail at %s: Got DistinguisherMethod %#+v for %s", trialName, a, fmtv1a1.FmtFlowSchema(orig))
				}
				if e, a := orig.Spec.PriorityLevelConfiguration.Name, matchPLName; e != a {
					t.Errorf("Fail at %s: e=%v, a=%v", trialName, e, a)
				}
			} else if matchFSName == fcv1a1.FlowSchemaNameCatchAll {
				t.Errorf("Fail at %s: Failed expected match for %#+v against %s", trialName, rd, fmtv1a1.FmtFlowSchema(orig))
			}
			if startFn != nil {
				exec, after := startFn(context.Background(), 47)
				t.Logf("startFn(..) => %v, %p", exec, after)
				if exec {
					after()
					t.Log("called after()")
				}
			}
		}
		for _, rd := range digests.mismatches {
			matchFSName, _, _, startFn := ctl.Match(rd)
			if matchFSName == orig.Name {
				t.Errorf("Fail at %s: Digest %#+v unexpectedly matched schema %s", trialName, rd, fmtv1a1.FmtFlowSchema(fs))
			}
			if startFn != nil {
				exec, after := startFn(context.Background(), 74)
				t.Logf("startFn(..) => %v, %p", exec, after)
				if exec {
					after()
					t.Log("called after()")
				}
			}
		}
	}
}

func genPLs(rng *rand.Rand, trial string, n int) (pls []*fcv1a1.PriorityLevelConfiguration, goodPLs map[string]*fcv1a1.PriorityLevelConfiguration, goodNames, badNames sets.String) {
	pls = make([]*fcv1a1.PriorityLevelConfiguration, 0, n)
	goodPLs = make(map[string]*fcv1a1.PriorityLevelConfiguration, n)
	goodNames = sets.NewString()
	badNames = sets.NewString()
	addGood := func(pl *fcv1a1.PriorityLevelConfiguration) {
		goodPLs[pl.Name] = pl
		goodNames.Insert(pl.Name)
		pls = append(pls, pl)
	}
	for i := 1; i <= n; i++ {
		pl, valid := genPL(rng, fmt.Sprintf("%s-%d", trial, i))
		if valid {
			addGood(pl)
		} else {
			badNames.Insert(pl.Name)
			pls = append(pls, pl)
		}
	}
	badNames.Insert(fmt.Sprintf("%s-%d", trial, n+1))
	if n == 0 || rng.Float32() < 0.5 {
		addGood(fcboot.MandatoryPriorityLevelConfigurationExempt)
	}
	if n == 0 || rng.Float32() < 0.5 {
		addGood(fcboot.MandatoryPriorityLevelConfigurationCatchAll)
	}
	return
}

func genFSs(t *testing.T, rng *rand.Rand, trial string, goodPLNames, badPLNames sets.String, n int) (newFSs []*fcv1a1.FlowSchema, newGoodFSMap map[string]*fcv1a1.FlowSchema, newFSDigestses map[string]*flowSchemaDigests) {
	newGoodFSMap = map[string]*fcv1a1.FlowSchema{}
	newFSDigestses = map[string]*flowSchemaDigests{}
	addGood := func(fs *fcv1a1.FlowSchema, matches, mismatches []RequestDigest) {
		newFSs = append(newFSs, fs)
		newGoodFSMap[fs.Name] = fs
		newFSDigestses[fs.Name] = &flowSchemaDigests{matches: matches, mismatches: mismatches}
		t.Logf("Generated good FlowSchema %#+v", fs)
	}
	if n == 0 || rng.Float32() < 0.5 {
		addGood(fcboot.MandatoryFlowSchemaCatchAll, nil, nil)
	}
	for i := 1; i <= n; i++ {
		fs, valid, _, _, matches, mismatches := genFS(t, rng, fmt.Sprintf("%s-%d", trial, i), goodPLNames, badPLNames)
		if valid {
			addGood(fs, matches, mismatches)
		} else {
			newFSs = append(newFSs, fs)
		}
	}
	if n == 0 || rng.Float32() < 0.5 {
		addGood(fcboot.MandatoryFlowSchemaExempt, nil, nil)
	}
	return
}

type mandBits struct{ exempt, catchAll bool }

func (mb mandBits) count() int {
	return btoi(mb.exempt) + btoi(mb.catchAll)
}

func seekMandFSs(fses map[string]*fcv1a1.FlowSchema) mandBits {
	return mandBits{
		exempt:   fses[fcv1a1.FlowSchemaNameExempt] != nil,
		catchAll: fses[fcv1a1.FlowSchemaNameCatchAll] != nil,
	}
}

type flowSchemaDigests struct {
	matches    []RequestDigest
	mismatches []RequestDigest
}

var _ *fcv1a1.FlowSchema = nil
var _ *metav1.ObjectMeta = nil
var _ = sets.NewString("x", "y")

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
