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

package flowcontrol

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	fcboot "k8s.io/apiserver/pkg/apis/flowcontrol/bootstrap"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	utilfc "k8s.io/apiserver/pkg/util/flowcontrol"
	fqtesting "k8s.io/apiserver/pkg/util/flowcontrol/fairqueuing/testing"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/klog"
)

const timeFmt = "2006-01-02T15:04:05.999"

func TestConfigConsumerFight(t *testing.T) {
	// Disable the APF FeatureGate so that the normal config consumer
	// controller does not interfere
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, genericfeatures.APIPriorityAndFairness, false)()
	_, loopbackConfig, closeFn := setup(t, 100, 100)
	defer closeFn()
	ctx := context.Background()
	const size = 3
	fsName := fcboot.MandatoryPriorityLevelConfigurationCatchAll.Name
	stopCh := make(chan struct{})
	now := time.Now()
	clk := clock.NewFakeClock(now)
	ctls := map[bool][]utilfc.TestableInterface{
		false: make([]utilfc.TestableInterface, size),
		true:  make([]utilfc.TestableInterface, size)}
	foreach := func(visit func(invert bool, i int, ctl utilfc.TestableInterface)) {
		for i := 0; i < size; i++ {
			for invert, slice := range ctls {
				visit(invert, i, slice[i])
			}
		}
	}
	foreach(func(invert bool, i int, ctl utilfc.TestableInterface) {
		myConfig := rest.CopyConfig(loopbackConfig)
		myConfig = rest.AddUserAgent(myConfig, fmt.Sprintf("invert=%v, i=%d", invert, i))
		myClientset := clientset.NewForConfigOrDie(myConfig)
		fcIfc := myClientset.FlowcontrolV1alpha1()
		// Wait until the chosen FlowSchema gets defined by the config producer
		wait.Poll(time.Second, wait.ForeverTestTimeout, func() (bool, error) {
			_, err := fcIfc.FlowSchemas().Get(ctx, fsName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return true, nil
		})
		informerFactory := informers.NewSharedInformerFactory(myClientset, 0)
		ctls[invert][i] = utilfc.NewTestable(
			fmt.Sprintf("Controller%d[invert=%v]", i, invert),
			true,
			clk,
			invert,
			informerFactory,
			fcIfc,
			200,         // server concurrency limit
			time.Minute, // request wait limit
			fqtesting.NewNoRestraintFactory(),
		)
		informerFactory.Start(stopCh)
	})
	foreach(func(invert bool, i int, ctl utilfc.TestableInterface) {
		if !ctl.WaitForCacheSync(stopCh) {
			t.Fatalf("Never achieved initial sync for i=%d, invert=%v", i, invert)
		}
	})
	var writeCount int
	for j := 0; j < 5; j++ {
		t.Logf("Syncing[size=%d, j=%d] at %s", size, j, clk.Now().Format(timeFmt))
		klog.V(3).Infof("Syncing size=%d, j=%d at %s", size, j, clk.Now().Format(timeFmt))
		wait := time.Hour
		foreach(func(invert bool, i int, ctl utilfc.TestableInterface) {
			triedWrites, didWrites, needRetry, thisWait := ctl.SyncOne()
			if needRetry {
				t.Errorf("Error for invert=%v, i=%d", invert, i)
			}
			t.Logf("For invert=%v, i=%d: triedWrites=%v, didWrites=%v, wait=%s", invert, i, triedWrites, didWrites, thisWait)
			if thisWait > 0 {
				wait = utilfc.DurationMin(wait, thisWait)
			}
			if triedWrites {
				writeCount += 1
			}
			if didWrites {
				wait = utilfc.DurationMin(wait, time.Millisecond*150)
			}
		})
		t.Logf("WriteCount[size=%d, j=%d] at %s is %d", size, j, clk.Now().Format(timeFmt), writeCount)
		time.Sleep(time.Millisecond * 150)
		if wait == time.Hour {
			wait = time.Second / 2
		}
		now = now.Add(wait)
		clk.SetTime(now)
	}
	close(stopCh)
}
