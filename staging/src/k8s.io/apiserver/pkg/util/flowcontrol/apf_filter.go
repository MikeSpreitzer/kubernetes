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
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apiserver/pkg/util/flowcontrol/counter"
	fq "k8s.io/apiserver/pkg/util/flowcontrol/fairqueuing"
	fqs "k8s.io/apiserver/pkg/util/flowcontrol/fairqueuing/queueset"
	"k8s.io/apiserver/pkg/util/flowcontrol/metrics"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog"

	fctypesv1a1 "k8s.io/api/flowcontrol/v1alpha1"
	fcclientv1a1 "k8s.io/client-go/kubernetes/typed/flowcontrol/v1alpha1"
)

// Interface defines how the API Priority and Fairness filter interacts with the underlying system.
type Interface interface {
	// Handle takes care of queuing and dispatching a request
	// characterized by the given digest.  The given `noteFn` will be
	// invoked with the results of request classification.  If Handle
	// decides that the request should be executed then `execute()`
	// will be invoked once to execute the request; otherwise
	// `execute()` will not be invoked.
	Handle(ctx context.Context,
		requestDigest RequestDigest,
		noteFn func(fs *fctypesv1a1.FlowSchema, pl *fctypesv1a1.PriorityLevelConfiguration),
		execFn func(),
	)

	// Run monitors config objects from the main apiservers and causes
	// any needed changes to local behavior.  This method ceases
	// activity and returns after the given channel is closed.
	Run(stopCh <-chan struct{}) error
}

type TestableInterface interface {
	Interface

	// WaitForCacheSync waits for the caches of the informers of the
	// implementation to sync.
	WaitForCacheSync(stopCh <-chan struct{}) bool

	// SyncOne attempts to sync all the API Priority and Fairness
	// config objects.  Returns a bool indicating whether updates were
	// attempted, another bool indicating whether updates succeeded,
	// another bool indicating whether a retry is needed to recover
	// from errors, and a Duration to wait before trying again if
	// updates are desired but suppressed (zero otherwise).
	SyncOne() (bool, bool, bool, time.Duration)
}

// This request filter implements https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/20190228-priority-and-fairness.md

// New creates a new instance to implement API priority and fairness
func New(
	informerFactory kubeinformers.SharedInformerFactory,
	flowcontrolClient fcclientv1a1.FlowcontrolV1alpha1Interface,
	serverConcurrencyLimit int,
	requestWaitLimit time.Duration,
) Interface {
	grc := counter.NoOp{}
	clk := clock.RealClock{}
	return NewTestable(
		"Controller",
		false,
		clk,
		false,
		informerFactory,
		flowcontrolClient,
		serverConcurrencyLimit,
		requestWaitLimit,
		fqs.NewQueueSetFactory(clk, grc),
	)
}

// NewTestable is extra flexible to facilitate testing
func NewTestable(
	name string,
	ignoreNotifications bool,
	clock clock.PassiveClock,
	invertDangle bool,
	informerFactory kubeinformers.SharedInformerFactory,
	flowcontrolClient fcclientv1a1.FlowcontrolV1alpha1Interface,
	serverConcurrencyLimit int,
	requestWaitLimit time.Duration,
	queueSetFactory fq.QueueSetFactory,
) TestableInterface {
	return newTestableController(name, ignoreNotifications, clock, invertDangle, informerFactory, flowcontrolClient, serverConcurrencyLimit, requestWaitLimit, queueSetFactory)
}

func (cfgCtl *configController) Handle(ctx context.Context, requestDigest RequestDigest,
	noteFn func(fs *fctypesv1a1.FlowSchema, pl *fctypesv1a1.PriorityLevelConfiguration),
	execFn func()) {
	fs, pl, isExempt, req, startWaitingTime := cfgCtl.startRequest(ctx, requestDigest)
	queued := startWaitingTime != time.Time{}
	noteFn(fs, pl)
	if req == nil {
		if queued {
			metrics.ObserveWaitingDuration(pl.Name, fs.Name, strconv.FormatBool(req != nil), time.Since(startWaitingTime))
		}
		klog.V(7).Infof("Handle(%#+v) => fsName=%q, distMethod=%#+v, plName=%q, isExempt=%v, reject", requestDigest, fs.Name, fs.Spec.DistinguisherMethod, pl.Name, isExempt)
		return
	}
	klog.V(7).Infof("Handle(%#+v) => fsName=%q, distMethod=%#+v, plName=%q, isExempt=%v, queued=%v", requestDigest, fs.Name, fs.Spec.DistinguisherMethod, pl.Name, isExempt, queued)
	var executed bool
	idle := req.Finish(func() {
		if queued {
			metrics.ObserveWaitingDuration(pl.Name, fs.Name, strconv.FormatBool(req != nil), time.Since(startWaitingTime))
		}
		metrics.AddDispatch(pl.Name, fs.Name)
		executed = true
		startExecutionTime := time.Now()
		execFn()
		metrics.ObserveExecutionDuration(pl.Name, fs.Name, time.Since(startExecutionTime))
	})
	if queued && !executed {
		metrics.ObserveWaitingDuration(pl.Name, fs.Name, strconv.FormatBool(req != nil), time.Since(startWaitingTime))
	}
	klog.V(7).Infof("Handle(%#+v) => fsName=%q, distMethod=%#+v, plName=%q, isExempt=%v, queued=%v, Finish() => idle=%v", requestDigest, fs.Name, fs.Spec.DistinguisherMethod, pl.Name, isExempt, queued, idle)
	if idle {
		cfgCtl.maybeReap(pl.Name)
	}
}
