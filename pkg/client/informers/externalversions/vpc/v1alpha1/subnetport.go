/* Copyright © 2024 VMware, Inc. All Rights Reserved.
   SPDX-License-Identifier: Apache-2.0 */

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	vpcv1alpha1 "github.com/vmware-tanzu/nsx-operator/pkg/apis/vpc/v1alpha1"
	versioned "github.com/vmware-tanzu/nsx-operator/pkg/client/clientset/versioned"
	internalinterfaces "github.com/vmware-tanzu/nsx-operator/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/vmware-tanzu/nsx-operator/pkg/client/listers/vpc/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// SubnetPortInformer provides access to a shared informer and lister for
// SubnetPorts.
type SubnetPortInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.SubnetPortLister
}

type subnetPortInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewSubnetPortInformer constructs a new informer for SubnetPort type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewSubnetPortInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredSubnetPortInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredSubnetPortInformer constructs a new informer for SubnetPort type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredSubnetPortInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CrdV1alpha1().SubnetPorts(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CrdV1alpha1().SubnetPorts(namespace).Watch(context.TODO(), options)
			},
		},
		&vpcv1alpha1.SubnetPort{},
		resyncPeriod,
		indexers,
	)
}

func (f *subnetPortInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredSubnetPortInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *subnetPortInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&vpcv1alpha1.SubnetPort{}, f.defaultInformer)
}

func (f *subnetPortInformer) Lister() v1alpha1.SubnetPortLister {
	return v1alpha1.NewSubnetPortLister(f.Informer().GetIndexer())
}
