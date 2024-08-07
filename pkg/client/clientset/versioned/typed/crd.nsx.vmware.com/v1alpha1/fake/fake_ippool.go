/* Copyright © 2024 VMware, Inc. All Rights Reserved.
   SPDX-License-Identifier: Apache-2.0 */

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/vmware-tanzu/nsx-operator/pkg/apis/crd.nsx.vmware.com/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeIPPools implements IPPoolInterface
type FakeIPPools struct {
	Fake *FakeCrdV1alpha1
	ns   string
}

var ippoolsResource = v1alpha1.SchemeGroupVersion.WithResource("ippools")

var ippoolsKind = v1alpha1.SchemeGroupVersion.WithKind("IPPool")

// Get takes name of the iPPool, and returns the corresponding iPPool object, and an error if there is any.
func (c *FakeIPPools) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.IPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(ippoolsResource, c.ns, name), &v1alpha1.IPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.IPPool), err
}

// List takes label and field selectors, and returns the list of IPPools that match those selectors.
func (c *FakeIPPools) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.IPPoolList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(ippoolsResource, ippoolsKind, c.ns, opts), &v1alpha1.IPPoolList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.IPPoolList{ListMeta: obj.(*v1alpha1.IPPoolList).ListMeta}
	for _, item := range obj.(*v1alpha1.IPPoolList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested iPPools.
func (c *FakeIPPools) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(ippoolsResource, c.ns, opts))

}

// Create takes the representation of a iPPool and creates it.  Returns the server's representation of the iPPool, and an error, if there is any.
func (c *FakeIPPools) Create(ctx context.Context, iPPool *v1alpha1.IPPool, opts v1.CreateOptions) (result *v1alpha1.IPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(ippoolsResource, c.ns, iPPool), &v1alpha1.IPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.IPPool), err
}

// Update takes the representation of a iPPool and updates it. Returns the server's representation of the iPPool, and an error, if there is any.
func (c *FakeIPPools) Update(ctx context.Context, iPPool *v1alpha1.IPPool, opts v1.UpdateOptions) (result *v1alpha1.IPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(ippoolsResource, c.ns, iPPool), &v1alpha1.IPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.IPPool), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeIPPools) UpdateStatus(ctx context.Context, iPPool *v1alpha1.IPPool, opts v1.UpdateOptions) (*v1alpha1.IPPool, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(ippoolsResource, "status", c.ns, iPPool), &v1alpha1.IPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.IPPool), err
}

// Delete takes name of the iPPool and deletes it. Returns an error if one occurs.
func (c *FakeIPPools) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(ippoolsResource, c.ns, name, opts), &v1alpha1.IPPool{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeIPPools) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(ippoolsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.IPPoolList{})
	return err
}

// Patch applies the patch and returns the patched iPPool.
func (c *FakeIPPools) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.IPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(ippoolsResource, c.ns, name, pt, data, subresources...), &v1alpha1.IPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.IPPool), err
}
