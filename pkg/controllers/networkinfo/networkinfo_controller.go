/* Copyright © 2023 VMware, Inc. All Rights Reserved.
   SPDX-License-Identifier: Apache-2.0 */

package networkinfo

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/vmware-tanzu/nsx-operator/pkg/apis/crd.nsx.vmware.com/v1alpha1"
	"github.com/vmware-tanzu/nsx-operator/pkg/controllers/common"
	"github.com/vmware-tanzu/nsx-operator/pkg/logger"
	"github.com/vmware-tanzu/nsx-operator/pkg/metrics"
	_ "github.com/vmware-tanzu/nsx-operator/pkg/nsx/ratelimiter"
	commonservice "github.com/vmware-tanzu/nsx-operator/pkg/nsx/services/common"
	"github.com/vmware-tanzu/nsx-operator/pkg/nsx/services/vpc"
)

var (
	log           = &logger.Log
	MetricResType = common.MetricResTypeNetworkInfo
)

// NetworkInfoReconciler NetworkInfoReconcile reconciles a NetworkInfo object
// Actually it is more like a shell, which is used to manage nsx VPC
type NetworkInfoReconciler struct {
	Client   client.Client
	Scheme   *apimachineryruntime.Scheme
	Service  *vpc.VPCService
	Recorder record.EventRecorder
}

func (r *NetworkInfoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &v1alpha1.NetworkInfo{}
	log.Info("reconciling NetworkInfo CR", "NetworkInfo", req.NamespacedName)
	metrics.CounterInc(r.Service.NSXConfig, metrics.ControllerSyncTotal, common.MetricResTypeNetworkInfo)

	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		log.Error(err, "unable to fetch NetworkInfo CR", "req", req.NamespacedName)
		return common.ResultNormal, client.IgnoreNotFound(err)
	}

	if obj.ObjectMeta.DeletionTimestamp.IsZero() {
		metrics.CounterInc(r.Service.NSXConfig, metrics.ControllerUpdateTotal, common.MetricResTypeNetworkInfo)
		if !controllerutil.ContainsFinalizer(obj, commonservice.NetworkInfoFinalizerName) {
			controllerutil.AddFinalizer(obj, commonservice.NetworkInfoFinalizerName)
			if err := r.Client.Update(ctx, obj); err != nil {
				log.Error(err, "add finalizer", "NetworkInfo", req.NamespacedName)
				updateFail(r, &ctx, obj, &err, r.Client, nil)
				return common.ResultRequeue, err
			}
			log.V(1).Info("added finalizer on NetworkInfo CR", "NetworkInfo", req.NamespacedName)
		}

		createdVpc, nc, err := r.Service.CreateOrUpdateVPC(obj)
		if err != nil {
			log.Error(err, "create vpc failed, would retry exponentially", "VPC", req.NamespacedName)
			updateFail(r, &ctx, obj, &err, r.Client, nil)
			return common.ResultRequeueAfter10sec, err
		}

		isShared, err := r.Service.IsSharedVPCNamespaceByNS(obj.GetNamespace())
		if err != nil {
			log.Error(err, "failed to check if namespace is shared", "Namespace", obj.GetNamespace())
			return common.ResultRequeue, err
		}
		if r.Service.NSXConfig.NsxConfig.UseAVILoadBalancer && !isShared {
			err = r.Service.CreateOrUpdateAVIRule(createdVpc, obj.Namespace)
			if err != nil {
				state := &v1alpha1.VPCState{
					Name:                    *createdVpc.DisplayName,
					VPCPath:                 *createdVpc.Path,
					DefaultSNATIP:           "",
					LoadBalancerIPAddresses: "",
					PrivateIPv4CIDRs:        nc.PrivateIPv4CIDRs,
				}
				log.Error(err, "update avi rule failed, would retry exponentially", "NetworkInfo", req.NamespacedName)
				updateFail(r, &ctx, obj, &err, r.Client, state)
				return common.ResultRequeueAfter10sec, err
			}
		}

		snatIP, path, cidr := "", "", ""
		// currently, auto snat is not exposed, and use default value True
		// checking autosnat to support future extension in vpc configuration
		if createdVpc.ServiceGateway != nil && createdVpc.ServiceGateway.AutoSnat != nil && *createdVpc.ServiceGateway.AutoSnat {
			snatIP, err = r.Service.GetDefaultSNATIP(*createdVpc)
			if err != nil {
				log.Error(err, "failed to read default SNAT ip from VPC", "VPC", createdVpc.Id)
				state := &v1alpha1.VPCState{
					Name:                    *createdVpc.DisplayName,
					VPCPath:                 *createdVpc.Path,
					DefaultSNATIP:           "",
					LoadBalancerIPAddresses: "",
					PrivateIPv4CIDRs:        nc.PrivateIPv4CIDRs,
				}
				updateFail(r, &ctx, obj, &err, r.Client, state)
				return common.ResultRequeueAfter10sec, err
			}
		}

		// if lb vpc enabled, read avi subnet path and cidr
		// nsx bug, if set LoadBalancerVpcEndpoint.Enabled to false, when read this vpc back,
		// LoadBalancerVpcEndpoint.Enabled will become a nil pointer.
		if r.Service.NSXConfig.NsxConfig.UseAVILoadBalancer && createdVpc.LoadBalancerVpcEndpoint.Enabled != nil && *createdVpc.LoadBalancerVpcEndpoint.Enabled {
			path, cidr, err = r.Service.GetAVISubnetInfo(*createdVpc)
			if err != nil {
				log.Error(err, "failed to read lb subnet path and cidr", "VPC", createdVpc.Id)
				state := &v1alpha1.VPCState{
					Name:                    *createdVpc.DisplayName,
					VPCPath:                 *createdVpc.Path,
					DefaultSNATIP:           snatIP,
					LoadBalancerIPAddresses: "",
					PrivateIPv4CIDRs:        nc.PrivateIPv4CIDRs,
				}
				updateFail(r, &ctx, obj, &err, r.Client, state)
				return common.ResultRequeueAfter10sec, err
			}
		}

		state := &v1alpha1.VPCState{
			Name:                    *createdVpc.DisplayName,
			VPCPath:                 *createdVpc.Path,
			DefaultSNATIP:           snatIP,
			LoadBalancerIPAddresses: cidr,
			PrivateIPv4CIDRs:        nc.PrivateIPv4CIDRs,
		}
		updateSuccess(r, &ctx, obj, r.Client, state, nc.Name, path, r.Service.GetNSXLBSPath(*createdVpc.Id))
	} else {
		if controllerutil.ContainsFinalizer(obj, commonservice.NetworkInfoFinalizerName) {
			metrics.CounterInc(r.Service.NSXConfig, metrics.ControllerDeleteTotal, common.MetricResTypeNetworkInfo)
			isShared, err := r.Service.IsSharedVPCNamespaceByNS(obj.GetNamespace())
			if err != nil {
				log.Error(err, "failed to check if namespace is shared", "Namespace", obj.GetNamespace())
				return common.ResultRequeue, err
			}
			vpcs := r.Service.GetVPCsByNamespace(obj.GetNamespace())
			// if nsx resource do not exist, continue to remove finalizer, or the crd can not be removed
			if len(vpcs) == 0 {
				// when nsx vpc not found in vpc store, skip deleting NSX VPC
				log.Info("can not find VPC in store, skip deleting NSX VPC, remove finalizer from NetworkInfo CR")
			} else if !isShared {
				vpc := vpcs[0]
				// first delete vpc and then ipblock or else it will fail arguing it is being referenced by other objects
				if err := r.Service.DeleteVPC(*vpc.Path); err != nil {
					log.Error(err, "failed to delete nsx VPC, would retry exponentially", "NetworkInfo", req.NamespacedName)
					deleteFail(r, &ctx, obj, &err, r.Client)
					return common.ResultRequeueAfter10sec, err
				}
				if err := r.Service.DeleteIPBlockInVPC(*vpc); err != nil {
					log.Error(err, "failed to delete private ip blocks for VPC", "VPC", req.NamespacedName)
					return common.ResultRequeueAfter10sec, err
				}
			}

			controllerutil.RemoveFinalizer(obj, commonservice.NetworkInfoFinalizerName)
			if err := r.Client.Update(ctx, obj); err != nil {
				deleteFail(r, &ctx, obj, &err, r.Client)
				return common.ResultRequeue, err
			}
			log.V(1).Info("removed finalizer", "NetworkInfo", req.NamespacedName)
			deleteSuccess(r, &ctx, obj)
		} else {
			// only print a message because it's not a normal case
			log.Info("finalizers cannot be recognized", "NetworkInfo", req.NamespacedName)
		}
	}
	return common.ResultNormal, nil
}

func (r *NetworkInfoReconciler) setupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NetworkInfo{}).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: common.NumReconcile(),
			}).
		Watches(
			// For created/removed network config, add/remove from vpc network config cache.
			// For modified network config, currently only support appending ips to public ip blocks,
			// update network config in cache and update nsx vpc object.
			&v1alpha1.VPCNetworkConfiguration{},
			&VPCNetworkConfigurationHandler{
				Client:     mgr.GetClient(),
				vpcService: r.Service,
			},
			builder.WithPredicates(VPCNetworkConfigurationPredicate)).
		Complete(r)
}

// Start setup manager and launch GC
func (r *NetworkInfoReconciler) Start(mgr ctrl.Manager) error {
	err := r.setupWithManager(mgr)
	if err != nil {
		return err
	}
	return nil
}

// CollectGarbage logic for nsx-vpc is that:
// 1. list all current existing namespace in kubernetes
// 2. list all the nsx-vpc in vpcStore
// 3. loop all the nsx-vpc to get its namespace, check if the namespace still exist
// 4. if ns do not exist anymore, delete the nsx-vpc resource
// it implements the interface GarbageCollector method.
func (r *NetworkInfoReconciler) CollectGarbage(ctx context.Context) {
	log.Info("VPC garbage collector started")
	// read all nsx-vpc from vpc store
	nsxVPCList := r.Service.ListVPC()
	if len(nsxVPCList) == 0 {
		return
	}

	// read all namespaces from k8s
	namespaces := &corev1.NamespaceList{}
	err := r.Client.List(ctx, namespaces)
	if err != nil {
		log.Error(err, "failed to list k8s namespaces")
		return
	}
	nsSet := sets.NewString()
	for _, ns := range namespaces.Items {
		nsSet.Insert(ns.Name)
	}

	for i := len(nsxVPCList) - 1; i >= 0; i-- {
		nsxVPCNamespace := getNamespaceFromNSXVPC(&nsxVPCList[i])
		if nsSet.Has(nsxVPCNamespace) {
			continue
		}
		elem := nsxVPCList[i]
		log.Info("GC collected nsx VPC object", "ID", elem.Id, "Namespace", nsxVPCNamespace)
		metrics.CounterInc(r.Service.NSXConfig, metrics.ControllerDeleteTotal, common.MetricResTypeNetworkInfo)
		err = r.Service.DeleteVPC(*elem.Path)
		if err != nil {
			metrics.CounterInc(r.Service.NSXConfig, metrics.ControllerDeleteFailTotal, common.MetricResTypeNetworkInfo)
		} else {
			metrics.CounterInc(r.Service.NSXConfig, metrics.ControllerDeleteSuccessTotal, common.MetricResTypeNetworkInfo)
			if err := r.Service.DeleteIPBlockInVPC(elem); err != nil {
				log.Error(err, "failed to delete private ip blocks for VPC", "VPC", *elem.DisplayName)
			}
			log.Info("deleted private ip blocks for VPC", "VPC", *elem.DisplayName)
		}
	}
}
