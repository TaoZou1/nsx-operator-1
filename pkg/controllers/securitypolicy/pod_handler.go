/* Copyright © 2024 Broadcom, Inc. All Rights Reserved.
   SPDX-License-Identifier: Apache-2.0 */

package securitypolicy

import (
	"context"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/vmware-tanzu/nsx-operator/pkg/nsx/services/securitypolicy"
	"github.com/vmware-tanzu/nsx-operator/pkg/util"
)

// We should consider the below scenarios:
// When a new added pod whose port name exists in security policy.
// When a deleted pod whose port name exists in security policy.
// When a pod's label is changed.
// In summary, we could roughly think if the port name of security policy exists in the
// new pod or old pod, we should reconcile the security policy.

type EnqueueRequestForPod struct {
	Client                   client.Client
	SecurityPolicyReconciler *SecurityPolicyReconciler
}

func (e *EnqueueRequestForPod) Create(_ context.Context, createEvent event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	e.Raw(createEvent, q)
}

func (e *EnqueueRequestForPod) Update(_ context.Context, updateEvent event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	e.Raw(updateEvent, q)
}

func (e *EnqueueRequestForPod) Delete(_ context.Context, deleteEvent event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	e.Raw(deleteEvent, q)
}

func (e *EnqueueRequestForPod) Generic(_ context.Context, genericEvent event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	e.Raw(genericEvent, q)
}

func (e *EnqueueRequestForPod) Raw(evt interface{}, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	var pods []v1.Pod
	var obj client.Object

	switch et := evt.(type) {
	case event.CreateEvent:
		obj = et.Object.(*v1.Pod)
	case event.UpdateEvent:
		obj = et.ObjectNew.(*v1.Pod)
	case event.DeleteEvent:
		obj = et.Object.(*v1.Pod)
	case event.GenericEvent:
		obj = et.Object.(*v1.Pod)
	default:
		log.Error(nil, "Unknown event type", "event", evt)
	}

	pod := obj.(*v1.Pod)
	vpcMode := securitypolicy.IsVPCEnabled(e.SecurityPolicyReconciler.Service)
	if isInSysNs, err := util.IsSystemNamespace(e.Client, pod.Namespace, nil, vpcMode); err != nil {
		log.Error(err, "Failed to fetch namespace", "namespace", pod.Namespace)
		return
	} else if isInSysNs {
		log.V(2).Info("POD is in system namespace, do nothing")
		return
	}
	pods = append(pods, *pod)
	err := reconcileSecurityPolicy(e.SecurityPolicyReconciler, e.Client, pods, q)
	if err != nil {
		log.Error(err, "Failed to reconcile security policy")
	}
}

func getAllPodPortNames(pods []v1.Pod) sets.Set[string] {
	podPortNames := sets.New[string]()
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.Name != "" {
					podPortNames.Insert(port.Name)
				}
			}
		}
	}
	return podPortNames
}

var PredicateFuncsPod = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		if p, ok := e.Object.(*v1.Pod); ok {
			log.V(1).Info("Receive pod create event", "namespace", p.Namespace, "name", p.Name)
			return util.CheckPodHasNamedPort(*p, "create")
		}
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldObj := e.ObjectOld.(*v1.Pod)
		newObj := e.ObjectNew.(*v1.Pod)
		log.V(1).Info("Receive pod update event", "namespace", oldObj.Namespace, "name", oldObj.Name)
		// The NSX operator should handle the case when the pod phase is changed from Pending to Running.
		if reflect.DeepEqual(oldObj.ObjectMeta.Labels, newObj.ObjectMeta.Labels) && oldObj.Status.Phase == newObj.Status.Phase {
			log.V(1).Info("POD label and phase are not changed, ignore it", "name", oldObj.Name)
			return false
		}
		if util.CheckPodHasNamedPort(*newObj, "update") {
			return true
		}
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		if p, ok := e.Object.(*v1.Pod); ok {
			log.V(1).Info("Receive pod delete event", "namespace", p.Namespace, "name", p.Name)
			return util.CheckPodHasNamedPort(*p, "delete")
		}
		return false
	},
}
