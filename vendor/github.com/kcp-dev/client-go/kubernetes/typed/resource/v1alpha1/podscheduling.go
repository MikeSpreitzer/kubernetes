//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright The KCP Authors.

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

// Code generated by kcp code-generator. DO NOT EDIT.

package v1alpha1

import (
	"context"

	kcpclient "github.com/kcp-dev/apimachinery/v2/pkg/client"
	"github.com/kcp-dev/logicalcluster/v3"

	resourcev1alpha1 "k8s.io/api/resource/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	resourcev1alpha1client "k8s.io/client-go/kubernetes/typed/resource/v1alpha1"
)

// PodSchedulingsClusterGetter has a method to return a PodSchedulingClusterInterface.
// A group's cluster client should implement this interface.
type PodSchedulingsClusterGetter interface {
	PodSchedulings() PodSchedulingClusterInterface
}

// PodSchedulingClusterInterface can operate on PodSchedulings across all clusters,
// or scope down to one cluster and return a PodSchedulingsNamespacer.
type PodSchedulingClusterInterface interface {
	Cluster(logicalcluster.Path) PodSchedulingsNamespacer
	List(ctx context.Context, opts metav1.ListOptions) (*resourcev1alpha1.PodSchedulingList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

type podSchedulingsClusterInterface struct {
	clientCache kcpclient.Cache[*resourcev1alpha1client.ResourceV1alpha1Client]
}

// Cluster scopes the client down to a particular cluster.
func (c *podSchedulingsClusterInterface) Cluster(clusterPath logicalcluster.Path) PodSchedulingsNamespacer {
	if clusterPath == logicalcluster.Wildcard {
		panic("A specific cluster must be provided when scoping, not the wildcard.")
	}

	return &podSchedulingsNamespacer{clientCache: c.clientCache, clusterPath: clusterPath}
}

// List returns the entire collection of all PodSchedulings across all clusters.
func (c *podSchedulingsClusterInterface) List(ctx context.Context, opts metav1.ListOptions) (*resourcev1alpha1.PodSchedulingList, error) {
	return c.clientCache.ClusterOrDie(logicalcluster.Wildcard).PodSchedulings(metav1.NamespaceAll).List(ctx, opts)
}

// Watch begins to watch all PodSchedulings across all clusters.
func (c *podSchedulingsClusterInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.clientCache.ClusterOrDie(logicalcluster.Wildcard).PodSchedulings(metav1.NamespaceAll).Watch(ctx, opts)
}

// PodSchedulingsNamespacer can scope to objects within a namespace, returning a resourcev1alpha1client.PodSchedulingInterface.
type PodSchedulingsNamespacer interface {
	Namespace(string) resourcev1alpha1client.PodSchedulingInterface
}

type podSchedulingsNamespacer struct {
	clientCache kcpclient.Cache[*resourcev1alpha1client.ResourceV1alpha1Client]
	clusterPath logicalcluster.Path
}

func (n *podSchedulingsNamespacer) Namespace(namespace string) resourcev1alpha1client.PodSchedulingInterface {
	return n.clientCache.ClusterOrDie(n.clusterPath).PodSchedulings(namespace)
}
