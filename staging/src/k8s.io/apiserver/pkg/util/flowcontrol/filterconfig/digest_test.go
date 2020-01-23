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
	"testing"
	"time"

	fcv1a1 "k8s.io/api/flowcontrol/v1alpha1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	fcboot "k8s.io/apiserver/pkg/apis/flowcontrol/bootstrap"
	"k8s.io/client-go/informers"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
)

func TestDigestConfig(t *testing.T) {
	clientset := clientsetfake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(clientset, 0)
	flowcontrolClient := clientset.FlowcontrolV1alpha1()
	ctl := NewTestableController(
		informerFactory,
		flowcontrolClient,
		100,         // server concurrency limit
		time.Minute, // request wait limit
		noRestraintQSF,
	)
	rngOuter := rand.New(rand.NewSource(42))
	for i := 1; i <= 1; i++ {
		rng := rand.New(rand.NewSource(rngOuter.Int63()))
		t.Run(fmt.Sprintf("trial%d:", i), func(t *testing.T) {
			oldGoodPLMap := map[string]*fcv1a1.PriorityLevelConfiguration{}
			newPLs, newGoodPLMap, newGoodPLNames, newBadPLNames := genPLs(rng, 1+rng.Intn(3))
			newFSs, newGoodFSMap, newFSDigestses := genFSs(t, rng, fmt.Sprintf("trial%d", i), newGoodPLNames, newBadPLNames, 1+rng.Intn(5))
			for _, newPL := range newPLs {
				t.Logf("Digesting newPL=%#+v", newPL)
			}
			for _, newFS := range newFSs {
				t.Logf("Digesting newFSs=%#+v", newFS)
			}
			ctl.digestConfigObjects(newPLs, newFSs)
			newMandBitsPL := seekMandPLs(newGoodPLMap)
			newMandBitsFS := seekMandFSs(newGoodFSMap)
			if e, a := len(newGoodFSMap)+2-newMandBitsFS.count(), len(ctl.flowSchemas); e != a {
				t.Errorf("e=%v, a=%v", e, a)
			}
			if e, a := len(newGoodPLMap)+2-newMandBitsPL.count(), len(ctl.priorityLevelStates); e != a {
				t.Errorf("e=%v, a=%v", e, a)
			}
			for plName, plState := range ctl.priorityLevelStates {
				checkNewPLState(t, plName, plState, oldGoodPLMap, newGoodPLMap)
			}
			for _, fs := range ctl.flowSchemas {
				checkNewFS(t, ctl, fs, newGoodFSMap, newFSDigestses[fs.Name])
			}
		})
	}
}

func checkNewPLState(t *testing.T, plName string, plState *priorityLevelState, oldGoodPLMap, newGoodPLMap map[string]*fcv1a1.PriorityLevelConfiguration) {
	pl, inNew := newGoodPLMap[plName]
	opl, _ := oldGoodPLMap[plName]
	if pl == nil {
		pl = opl
	}
	var isMand bool
	for _, mpl := range fcboot.MandatoryPriorityLevelConfigurations {
		if plName == mpl.Name {
			isMand = true
			if pl == nil {
				pl = mpl
			}
		}
	}
	if pl == nil {
		t.Errorf("Inexplicable entry %q %#+v", plName, plState)
		return
	}
	if e, a := pl.Spec, plState.config; e != a {
		t.Errorf("e=%#+v, a=%#+v", e, a)
	}
	isExempt := pl.Spec.Type == fcv1a1.PriorityLevelEnablementExempt
	if e, a := isExempt, (plState.qsCompleter == nil); e != a {
		t.Errorf("e=%v, a=%v", e, a)
	}
	if e, a := isExempt, (plState.queues == nil); e != a {
		t.Errorf("e=%v, a=%v", e, a)
	}
	if e, a := !(inNew || isMand), plState.quiescing; e != a {
		t.Errorf("e=%v, a=%v", e, a)
	}
	if e, a := 0, plState.numPending; e != a {
		t.Errorf("e=%v, a=%v", e, a)
	}
}

func checkNewFS(t *testing.T, ctl *configController, fs *fcv1a1.FlowSchema, newGoodFSMap map[string]*fcv1a1.FlowSchema, digests *flowSchemaDigests) {
	orig := newGoodFSMap[fs.Name]
	for _, mfs := range fcboot.MandatoryFlowSchemas {
		if fs.Name == mfs.Name {
			if orig == nil {
				orig = mfs
			}
		}
	}
	if orig == nil {
		t.Errorf("Inexplicable entry %#+v", *fs)
		return
	}
	if e, a := orig.Spec, fs.Spec; !apiequality.Semantic.DeepEqual(e, a) {
		t.Errorf("e=%s, a=%s", FmtFlowSchemaSpec(&e), FmtFlowSchemaSpec(&a))
	}
	if digests != nil {
		for _, rd := range digests.matches {
			matchFSName, matchDistMethod, matchPLName, startFn := ctl.Match(rd)
			if matchFSName == orig.Name {
				if e, a := orig.Spec.DistinguisherMethod, matchDistMethod; !apiequality.Semantic.DeepEqual(e, a) {
					t.Errorf("Got DistinguisherMethod %#+v for %s", a, FmtFlowSchema(orig))
				}
				if e, a := orig.Spec.PriorityLevelConfiguration.Name, matchPLName; e != a {
					t.Errorf("e=%v, a=%v", e, a)
				}
			} else if matchFSName == fcv1a1.FlowSchemaNameCatchAll {
				t.Errorf("Failed expected match for %#+v against %s", rd, FmtFlowSchema(orig))
			}
			if startFn != nil {
				exec, after := startFn(context.Background(), 47, rd.RequestInfo, rd.User)
				if exec {
					after()
				}
			}
		}
		for _, rd := range digests.mismatches {
			matchFSName, _, _, _ := ctl.Match(rd)
			if matchFSName == orig.Name {
				t.Errorf("Digest %#+v unexpectedly matched schema %s", rd, FmtFlowSchema(fs))
			}
		}
	}
}

func genPLs(rng *rand.Rand, n int) (pls []*fcv1a1.PriorityLevelConfiguration, goodPLs map[string]*fcv1a1.PriorityLevelConfiguration, goodNames, badNames sets.String) {
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
		pl, valid := genPL(rng, fmt.Sprintf("pl%d", i))
		if valid {
			addGood(pl)
		} else {
			badNames.Insert(pl.Name)
			pls = append(pls, pl)
		}
	}
	if rng.Float32() < 0.5 {
		addGood(fcboot.MandatoryPriorityLevelConfigurationExempt)
	}
	if rng.Float32() < 0.5 {
		addGood(fcboot.MandatoryPriorityLevelConfigurationCatchAll)
	}
	return
}

func genFSs(t *testing.T, rng *rand.Rand, trial string, goodPLNames, badPLNames sets.String, n int) (newFSs []*fcv1a1.FlowSchema, newGoodFSMap map[string]*fcv1a1.FlowSchema, newFSDigestses map[string]*flowSchemaDigests) {
	newGoodFSMap = map[string]*fcv1a1.FlowSchema{}
	newFSDigestses = map[string]*flowSchemaDigests{}
	for i := 1; i <= n; i++ {
		fs, valid, _, _, matches, mismatches := genFS(t, rng, fmt.Sprintf("%s-%d:", trial, i), goodPLNames, badPLNames)
		newFSs = append(newFSs, fs)
		if valid {
			newGoodFSMap[fs.Name] = fs
			newFSDigestses[fs.Name] = &flowSchemaDigests{matches: matches, mismatches: mismatches}
			t.Logf("Generated good FlowSchema %#+v", fs)
		}
	}
	return
}

type mandBits struct{ exempt, catchAll bool }

func (mb mandBits) count() int {
	return btoi(mb.exempt) + btoi(mb.catchAll)
}

func seekMandPLs(pls map[string]*fcv1a1.PriorityLevelConfiguration) mandBits {
	return mandBits{
		exempt:   pls[fcv1a1.PriorityLevelConfigurationNameExempt] != nil,
		catchAll: pls[fcv1a1.PriorityLevelConfigurationNameCatchAll] != nil,
	}
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
