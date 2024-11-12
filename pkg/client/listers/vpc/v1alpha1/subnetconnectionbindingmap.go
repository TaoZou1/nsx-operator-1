/* Copyright © 2024 VMware, Inc. All Rights Reserved.
   SPDX-License-Identifier: Apache-2.0 */

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/vmware-tanzu/nsx-operator/pkg/apis/vpc/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// SubnetConnectionBindingMapLister helps list SubnetConnectionBindingMaps.
// All objects returned here must be treated as read-only.
type SubnetConnectionBindingMapLister interface {
	// List lists all SubnetConnectionBindingMaps in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.SubnetConnectionBindingMap, err error)
	// SubnetConnectionBindingMaps returns an object that can list and get SubnetConnectionBindingMaps.
	SubnetConnectionBindingMaps(namespace string) SubnetConnectionBindingMapNamespaceLister
	SubnetConnectionBindingMapListerExpansion
}

// subnetConnectionBindingMapLister implements the SubnetConnectionBindingMapLister interface.
type subnetConnectionBindingMapLister struct {
	indexer cache.Indexer
}

// NewSubnetConnectionBindingMapLister returns a new SubnetConnectionBindingMapLister.
func NewSubnetConnectionBindingMapLister(indexer cache.Indexer) SubnetConnectionBindingMapLister {
	return &subnetConnectionBindingMapLister{indexer: indexer}
}

// List lists all SubnetConnectionBindingMaps in the indexer.
func (s *subnetConnectionBindingMapLister) List(selector labels.Selector) (ret []*v1alpha1.SubnetConnectionBindingMap, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.SubnetConnectionBindingMap))
	})
	return ret, err
}

// SubnetConnectionBindingMaps returns an object that can list and get SubnetConnectionBindingMaps.
func (s *subnetConnectionBindingMapLister) SubnetConnectionBindingMaps(namespace string) SubnetConnectionBindingMapNamespaceLister {
	return subnetConnectionBindingMapNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// SubnetConnectionBindingMapNamespaceLister helps list and get SubnetConnectionBindingMaps.
// All objects returned here must be treated as read-only.
type SubnetConnectionBindingMapNamespaceLister interface {
	// List lists all SubnetConnectionBindingMaps in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.SubnetConnectionBindingMap, err error)
	// Get retrieves the SubnetConnectionBindingMap from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.SubnetConnectionBindingMap, error)
	SubnetConnectionBindingMapNamespaceListerExpansion
}

// subnetConnectionBindingMapNamespaceLister implements the SubnetConnectionBindingMapNamespaceLister
// interface.
type subnetConnectionBindingMapNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all SubnetConnectionBindingMaps in the indexer for a given namespace.
func (s subnetConnectionBindingMapNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.SubnetConnectionBindingMap, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.SubnetConnectionBindingMap))
	})
	return ret, err
}

// Get retrieves the SubnetConnectionBindingMap from the indexer for a given namespace and name.
func (s subnetConnectionBindingMapNamespaceLister) Get(name string) (*v1alpha1.SubnetConnectionBindingMap, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("subnetconnectionbindingmap"), name)
	}
	return obj.(*v1alpha1.SubnetConnectionBindingMap), nil
}
