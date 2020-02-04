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
	"crypto/sha256"
	"encoding/binary"
	"hash/crc64"
	"strconv"
	"time"

	// TODO: decide whether to use the existing metrics, which
	// categorize according to mutating vs readonly, or make new
	// metrics because this filter does not pay attention to that
	// distinction

	// "k8s.io/apiserver/pkg/endpoints/metrics"

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

// This request filter implements https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/20190228-priority-and-fairness.md

// New creates a new instance to implement API priority and fairness
func New(
	informerFactory kubeinformers.SharedInformerFactory,
	flowcontrolClient fcclientv1a1.FlowcontrolV1alpha1Interface,
	serverConcurrencyLimit int,
	requestWaitLimit time.Duration,
) Interface {
	grc := counter.NoOp{}
	return NewTestable(
		informerFactory,
		flowcontrolClient,
		serverConcurrencyLimit,
		requestWaitLimit,
		fqs.NewQueueSetFactory(&clock.RealClock{}, grc),
	)
}

// NewTestable is extra flexible to facilitate testing
func NewTestable(
	informerFactory kubeinformers.SharedInformerFactory,
	flowcontrolClient fcclientv1a1.FlowcontrolV1alpha1Interface,
	serverConcurrencyLimit int,
	requestWaitLimit time.Duration,
	queueSetFactory fq.QueueSetFactory,
) Interface {
	return newTestableController(informerFactory, flowcontrolClient, serverConcurrencyLimit, requestWaitLimit, queueSetFactory)
}

func (cfgCtl *configController) Handle(ctx context.Context, requestDigest RequestDigest,
	noteFn func(fs *fctypesv1a1.FlowSchema, pl *fctypesv1a1.PriorityLevelConfiguration),
	execFn func()) {
	var queued bool
	var startWaitingTime time.Time
	hashFn := func(fsName string, distinguisherMethod *fctypesv1a1.FlowDistinguisherMethod) uint64 {
		flowDistinguisher := computeFlowDistinguisher(requestDigest, distinguisherMethod)
		hash := hashFlowID(fsName, flowDistinguisher)
		return hash
	}
	preQueueFn := func() {
		queued = true
		startWaitingTime = time.Now()
	}
	fs, pl, isExempt, req := cfgCtl.startRequest(ctx, requestDigest, hashFn, preQueueFn)
	noteFn(fs, pl)
	if req == nil {
		if queued {
			metrics.ObserveWaitingDuration(pl.Name, fs.Name, strconv.FormatBool(req != nil), time.Now().Sub(startWaitingTime))
		}
		klog.V(7).Infof("Handle(%#+v) => fsName=%q, distMethod=%#+v, plName=%q, isExempt=%v, reject", requestDigest, fs.Name, fs.Spec.DistinguisherMethod, pl.Name, isExempt)
		return
	}
	klog.V(7).Infof("Handle(%#+v) => fsName=%q, distMethod=%#+v, plName=%q, isExempt=%v, queued=%v", requestDigest, fs.Name, fs.Spec.DistinguisherMethod, pl.Name, isExempt, queued)
	var executed bool
	idle3 := req.Finish(func() {
		if queued {
			metrics.ObserveWaitingDuration(pl.Name, fs.Name, strconv.FormatBool(req != nil), time.Now().Sub(startWaitingTime))
		}
		executed = true
		startExecutionTime := time.Now()
		execFn()
		metrics.ObserveExecutionDuration(pl.Name, fs.Name, time.Now().Sub(startExecutionTime))
	})
	if queued && !executed {
		metrics.ObserveWaitingDuration(pl.Name, fs.Name, strconv.FormatBool(req != nil), time.Now().Sub(startWaitingTime))
	}
	klog.V(7).Infof("Handle(%#+v) => fsName=%q, distMethod=%#+v, plName=%q, isExempt=%v, queued=%v, Finish() => idle=%v", requestDigest, fs.Name, fs.Spec.DistinguisherMethod, pl.Name, isExempt, queued, idle3)
	if idle3 {
		cfgCtl.maybeReap(pl.Name)
	}
}

// computeFlowDistinguisher extracts the flow distinguisher according to the given method
func computeFlowDistinguisher(rd RequestDigest, method *fctypesv1a1.FlowDistinguisherMethod) string {
	if method == nil {
		return ""
	}
	switch method.Type {
	case fctypesv1a1.FlowDistinguisherMethodByUserType:
		return rd.User.GetName()
	case fctypesv1a1.FlowDistinguisherMethodByNamespaceType:
		return rd.RequestInfo.Namespace
	default:
		// this line shall never reach
		panic("invalid flow-distinguisher method")
	}
}

const hashByCRC = false

// hashFlowID hashes the inputs into 64-bits
func hashFlowID(fsName, fDistinguisher string) uint64 {
	if hashByCRC {
		return crcFlowID(fsName, fDistinguisher)
	}
	return shaFlowID(fsName, fDistinguisher)
}

func shaFlowID(fsName, fDistinguisher string) uint64 {
	hash := sha256.New()
	var sep = [1]byte{0}
	hash.Write([]byte(fsName))
	hash.Write(sep[:])
	hash.Write([]byte(fDistinguisher))
	var sum [32]byte
	hash.Sum(sum[:0])
	return binary.LittleEndian.Uint64(sum[:8])
}

var crcHashTable = crc64.MakeTable(crc64.ECMA)

func crcFlowID(fsName, fDistinguisher string) uint64 {
	var hash uint64
	hash = crc64.Update(hash, crcHashTable, []byte(fsName))
	hash = crc64.Update(hash, crcHashTable, []byte{1})
	hash = crc64.Update(hash, crcHashTable, []byte(fDistinguisher))
	return hash
}
