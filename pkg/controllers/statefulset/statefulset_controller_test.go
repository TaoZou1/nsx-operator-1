/* Copyright © 2026 VMware, Inc. All Rights Reserved.
   SPDX-License-Identifier: Apache-2.0 */

package statefulset

import (
	"context"
	"errors"
	"testing"
	"time"

	gomonkey "github.com/agiledragon/gomonkey/v2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmware/vsphere-automation-sdk-go/services/nsxt/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/vmware-tanzu/nsx-operator/pkg/config"
	"github.com/vmware-tanzu/nsx-operator/pkg/controllers/common"
	mock_client "github.com/vmware-tanzu/nsx-operator/pkg/mock/controller-runtime/client"
	servicecommon "github.com/vmware-tanzu/nsx-operator/pkg/nsx/services/common"
	subnetportservice "github.com/vmware-tanzu/nsx-operator/pkg/nsx/services/subnetport"
)

type MockManager struct {
	ctrl.Manager
	client client.Client
	scheme *runtime.Scheme
}

func (m *MockManager) GetClient() client.Client {
	return m.client
}

func (m *MockManager) GetScheme() *runtime.Scheme {
	return m.scheme
}

func (m *MockManager) GetEventRecorderFor(name string) record.EventRecorder {
	return nil
}

func (m *MockManager) Add(runnable manager.Runnable) error {
	return nil
}

func (m *MockManager) Start(context.Context) error {
	return nil
}

type fakeRecorder struct{}

func (recorder fakeRecorder) Event(object runtime.Object, eventtype, reason, message string) {}

func (recorder fakeRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
}

func (recorder fakeRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
}

func TestParseIndexFromPodName(t *testing.T) {
	tests := []struct {
		name    string
		podName string
		want    int
	}{
		{
			name:    "valid index",
			podName: "tea-set-0",
			want:    0,
		},
		{
			name:    "valid index 5",
			podName: "tea-set-5",
			want:    5,
		},
		{
			name:    "no dash",
			podName: "teaset",
			want:    -1,
		},
		{
			name:    "invalid index",
			podName: "tea-set-abc",
			want:    -1,
		},
		{
			name:    "empty string",
			podName: "",
			want:    -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIndexFromPodName(tt.podName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStsPodOrdinalFromPort(t *testing.T) {
	podIndexScope := servicecommon.TagScopePodIndex
	podNameScope := servicecommon.TagScopePodName
	tests := []struct {
		name string
		port *model.VpcSubnetPort
		want int
	}{
		{
			name: "pod-index tag",
			port: &model.VpcSubnetPort{
				Tags: []model.Tag{
					{Scope: &podIndexScope, Tag: servicecommon.String("9")},
					{Scope: &podNameScope, Tag: servicecommon.String("web-9")},
				},
			},
			want: 9,
		},
		{
			name: "fallback to pod name",
			port: &model.VpcSubnetPort{
				Tags: []model.Tag{
					{Scope: &podNameScope, Tag: servicecommon.String("tea-set-3")},
				},
			},
			want: 3,
		},
		{
			name: "prefer pod-index over misleading name",
			port: &model.VpcSubnetPort{
				Tags: []model.Tag{
					{Scope: &podIndexScope, Tag: servicecommon.String("2")},
					{Scope: &podNameScope, Tag: servicecommon.String("custom-template-9")},
				},
			},
			want: 2,
		},
		{
			name: "invalid pod-index falls back to name",
			port: &model.VpcSubnetPort{
				Tags: []model.Tag{
					{Scope: &podIndexScope, Tag: servicecommon.String("not-a-number")},
					{Scope: &podNameScope, Tag: servicecommon.String("tea-set-1")},
				},
			},
			want: 1,
		},
		{
			name: "nil port",
			port: nil,
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stsPodOrdinalFromPort(tt.port)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPredicateUpdateFunc(t *testing.T) {
	tests := []struct {
		name        string
		oldReplicas *int32
		newReplicas *int32
		want        bool
	}{
		{
			name:        "shrink replicas",
			oldReplicas: func() *int32 { r := int32(5); return &r }(),
			newReplicas: func() *int32 { r := int32(3); return &r }(),
			want:        true,
		},
		{
			name:        "expand replicas",
			oldReplicas: func() *int32 { r := int32(3); return &r }(),
			newReplicas: func() *int32 { r := int32(5); return &r }(),
			want:        false,
		},
		{
			name:        "same replicas",
			oldReplicas: func() *int32 { r := int32(3); return &r }(),
			newReplicas: func() *int32 { r := int32(3); return &r }(),
			want:        false,
		},
		{
			name:        "old nil replicas",
			oldReplicas: nil,
			newReplicas: func() *int32 { r := int32(3); return &r }(),
			want:        false,
		},
		{
			name:        "new nil replicas",
			oldReplicas: func() *int32 { r := int32(3); return &r }(),
			newReplicas: nil,
			want:        false,
		},
		{
			name:        "both nil replicas",
			oldReplicas: nil,
			newReplicas: nil,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldSts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: tt.oldReplicas,
				},
			}
			newSts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: tt.newReplicas,
				},
			}

			updateEvent := event.UpdateEvent{
				ObjectOld: oldSts,
				ObjectNew: newSts,
			}
			result := PredicateFuncsForStatefulSet.UpdateFunc(updateEvent)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestPredicateFuncsForStatefulSet(t *testing.T) {
	createEvent := event.CreateEvent{}
	assert.False(t, PredicateFuncsForStatefulSet.CreateFunc(createEvent))

	deleteEvent := event.DeleteEvent{}
	assert.True(t, PredicateFuncsForStatefulSet.DeleteFunc(deleteEvent))

	genericEvent := event.GenericEvent{}
	assert.False(t, PredicateFuncsForStatefulSet.GenericFunc(genericEvent))
}

func TestRestoreReconcile(t *testing.T) {
	reconciler := &StatefulSetReconciler{}
	err := reconciler.RestoreReconcile()
	assert.NoError(t, err)
}

func TestNewStatefulSetReconciler(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		Service: servicecommon.Service{
			Client: fakeClient,
			NSXConfig: &config.NSXOperatorConfig{
				NsxConfig: &config.NsxConfig{
					EnforcementPoint: "vmc-enforcementpoint",
				},
			},
		},
	}
	mockMgr := &MockManager{scheme: runtime.NewScheme(), client: fakeClient}
	patches := gomonkey.ApplyFunc((*StatefulSetReconciler).setupWithManager, func(r *StatefulSetReconciler, mgr manager.Manager) error {
		return nil
	})
	defer patches.Reset()
	r := NewStatefulSetReconciler(mockMgr, subnetPortService)
	assert.NotNil(t, r)
}

func TestStatefulSetReconciler_StartController(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		Service: servicecommon.Service{
			Client: fakeClient,
		},
	}
	mockMgr := &MockManager{scheme: runtime.NewScheme(), client: fakeClient}
	patches := gomonkey.ApplyFunc((*StatefulSetReconciler).setupWithManager, func(r *StatefulSetReconciler, mgr manager.Manager) error {
		return nil
	})
	patches.ApplyFunc(common.GenericGarbageCollector, func(cancel chan bool, timeout time.Duration, f func(ctx context.Context) error) {
	})
	defer patches.Reset()
	r := NewStatefulSetReconciler(mockMgr, subnetPortService)
	err := r.StartController(mockMgr, nil)
	assert.Nil(t, err)
}

func TestStatefulSetReconciler_Reconcile(t *testing.T) {
	mockCtl := gomock.NewController(t)
	k8sClient := mock_client.NewMockClient(mockCtl)
	defer mockCtl.Finish()

	r := &StatefulSetReconciler{
		Client: k8sClient,
		SubnetPortService: &subnetportservice.SubnetPortService{
			Service: servicecommon.Service{
				NSXConfig: &config.NSXOperatorConfig{
					NsxConfig: &config.NsxConfig{
						EnforcementPoint: "vmc-enforcementpoint",
					},
				},
			},
			SubnetPortStore: &subnetportservice.SubnetPortStore{},
		},
		Recorder: fakeRecorder{},
	}
	r.StatusUpdater = common.NewStatusUpdater(k8sClient, r.SubnetPortService.NSXConfig, r.Recorder, MetricResTypeStatefulSet, "SubnetPort", "StatefulSet")

	tests := []struct {
		name           string
		prepareFunc    func(*testing.T, *StatefulSetReconciler) *gomonkey.Patches
		expectedErr    string
		expectedResult ctrl.Result
	}{
		{
			name: "StatefulSet not found",
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("not found"))
				return nil
			},
			expectedErr:    "not found",
			expectedResult: common.ResultRequeue,
		},
		{
			name: "StatefulSet found",
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				sts := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: func() *int32 { r := int32(3); return &r }(),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas: 3,
					},
				}
				k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Do(
					func(ctx interface{}, key interface{}, obj interface{}, opts ...interface{}) error {
						sts.DeepCopyInto(obj.(*appsv1.StatefulSet))
						return nil
					})
				patches := gomonkey.ApplyFunc((*StatefulSetReconciler).handleReplicaChange,
					func(r *StatefulSetReconciler, ctx context.Context, sts *appsv1.StatefulSet) error {
						return nil
					})
				return patches
			},
			expectedResult: common.ResultNormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patches := tt.prepareFunc(t, r)
			if patches != nil {
				defer patches.Reset()
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-sts",
					Namespace: "default",
				},
			}

			result, err := r.Reconcile(context.Background(), req)
			if tt.expectedErr != "" {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}

func TestStatefulSetReconciler_CollectGarbage(t *testing.T) {
	mockCtl := gomock.NewController(t)
	k8sClient := mock_client.NewMockClient(mockCtl)
	defer mockCtl.Finish()

	subnetPortService := &subnetportservice.SubnetPortService{
		Service: servicecommon.Service{
			NSXConfig: &config.NSXOperatorConfig{
				NsxConfig: &config.NsxConfig{
					EnforcementPoint: "vmc-enforcementpoint",
				},
			},
		},
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            k8sClient,
		SubnetPortService: subnetPortService,
	}

	tests := []struct {
		name        string
		prepareFunc func(*testing.T, *StatefulSetReconciler) *gomonkey.Patches
		wantErr     bool
	}{
		{
			name: "list error",
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				k8sClient.EXPECT().List(gomock.Any(), gomock.Any()).Return(errors.New("list failed"))
				return nil
			},
			wantErr: true,
		},
		{
			name: "list success with empty items",
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				k8sClient.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil).Do(
					func(ctx interface{}, list interface{}, opts ...interface{}) error {
						list.(*appsv1.StatefulSetList).Items = []appsv1.StatefulSet{}
						return nil
					})
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortStore).GetByIndex,
					func(s *subnetportservice.SubnetPortStore, indexKey string, indexValue string) []*model.VpcSubnetPort {
						return []*model.VpcSubnetPort{}
					})
				return patches
			},
			wantErr: false,
		},
		{
			name: "list success with StatefulSet - replicas shrink",
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				sts := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts",
						Namespace: "default",
						UID:       "sts-uid-1",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: func() *int32 { r := int32(2); return &r }(),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas: 5,
					},
				}
				k8sClient.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil).Do(
					func(ctx interface{}, list interface{}, opts ...interface{}) error {
						list.(*appsv1.StatefulSetList).Items = []appsv1.StatefulSet{*sts}
						return nil
					})
				callCount := 0
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortStore).GetByIndex,
					func(s *subnetportservice.SubnetPortStore, indexKey string, indexValue string) []*model.VpcSubnetPort {
						callCount++
						if callCount == 1 {
							return []*model.VpcSubnetPort{
								{DisplayName: servicecommon.String("test-sts-0")},
								{DisplayName: servicecommon.String("test-sts-1")},
								{DisplayName: servicecommon.String("test-sts-2")},
								{DisplayName: servicecommon.String("test-sts-3")},
								{DisplayName: servicecommon.String("test-sts-4")},
							}
						}
						return []*model.VpcSubnetPort{}
					})
				patches.ApplyFunc(
					(*subnetportservice.SubnetPortService).DeleteSubnetPort,
					func(s *subnetportservice.SubnetPortService, port *model.VpcSubnetPort) error {
						return nil
					})
				return patches
			},
			wantErr: false,
		},
		{
			name: "list success - stateful deleted (orphaned ports)",
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				k8sClient.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil).Do(
					func(ctx interface{}, list interface{}, opts ...interface{}) error {
						list.(*appsv1.StatefulSetList).Items = []appsv1.StatefulSet{}
						return nil
					})
				callCount := 0
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortStore).GetByIndex,
					func(s *subnetportservice.SubnetPortStore, indexKey string, indexValue string) []*model.VpcSubnetPort {
						callCount++
						if callCount == 1 {
							return []*model.VpcSubnetPort{}
						}
						portID := servicecommon.String("orphaned-port-1")
						stsUID := servicecommon.String("deleted-sts-uid")
						return []*model.VpcSubnetPort{
							{
								Id:          portID,
								DisplayName: servicecommon.String("test-sts-0"),
								Tags: []model.Tag{
									{Scope: servicecommon.String(servicecommon.TagScopeStatefulSetUID), Tag: stsUID},
								},
							},
						}
					})
				patches.ApplyFunc(
					(*subnetportservice.SubnetPortService).DeleteSubnetPort,
					func(s *subnetportservice.SubnetPortService, port *model.VpcSubnetPort) error {
						return nil
					})
				return patches
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patches := tt.prepareFunc(t, r)
			if patches != nil {
				defer patches.Reset()
			}

			err := r.CollectGarbage(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStatefulSetReconciler_HandleReplicaChange(t *testing.T) {
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		SubnetPortService: subnetPortService,
	}

	tests := []struct {
		name        string
		sts         *appsv1.StatefulSet
		prepareFunc func(*testing.T, *StatefulSetReconciler) *gomonkey.Patches
		wantErr     bool
	}{
		{
			name: "replicas decreased",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts",
					Namespace: "default",
					UID:       "test-sts-uid",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: func() *int32 { r := int32(2); return &r }(),
				},
				Status: appsv1.StatefulSetStatus{
					Replicas: 5,
				},
			},
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortService).ListSubnetPortByStsUid,
					func(s *subnetportservice.SubnetPortService, ns string, stsUid string) []*model.VpcSubnetPort {
						return []*model.VpcSubnetPort{
							{Id: servicecommon.String("port1"), DisplayName: servicecommon.String("test-sts-0")},
							{Id: servicecommon.String("port2"), DisplayName: servicecommon.String("test-sts-1")},
							{Id: servicecommon.String("port3"), DisplayName: servicecommon.String("test-sts-2")},
							{Id: servicecommon.String("port4"), DisplayName: servicecommon.String("test-sts-3")},
							{Id: servicecommon.String("port5"), DisplayName: servicecommon.String("test-sts-4")},
						}
					})
				patches.ApplyFunc((*StatefulSetReconciler).releaseSubnetPortForPod,
					func(r *StatefulSetReconciler, ctx context.Context, namespace, podName string) error {
						return nil
					})
				return patches
			},
			wantErr: false,
		},
		{
			name: "replicas increased",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts",
					Namespace: "default",
					UID:       "test-sts-uid",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: func() *int32 { r := int32(5); return &r }(),
				},
				Status: appsv1.StatefulSetStatus{
					Replicas: 3,
				},
			},
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortService).ListSubnetPortByStsUid,
					func(s *subnetportservice.SubnetPortService, ns string, stsUid string) []*model.VpcSubnetPort {
						return []*model.VpcSubnetPort{}
					})
				return patches
			},
			wantErr: false,
		},
		{
			name: "nil replicas spec",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts",
					Namespace: "default",
					UID:       "test-sts-uid",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: nil,
				},
				Status: appsv1.StatefulSetStatus{
					Replicas: 3,
				},
			},
			prepareFunc: func(t *testing.T, r *StatefulSetReconciler) *gomonkey.Patches {
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortService).ListSubnetPortByStsUid,
					func(s *subnetportservice.SubnetPortService, ns string, stsUid string) []*model.VpcSubnetPort {
						return []*model.VpcSubnetPort{}
					})
				patches.ApplyFunc((*StatefulSetReconciler).releaseSubnetPortForPod,
					func(r *StatefulSetReconciler, ctx context.Context, namespace, podName string) error {
						return nil
					})
				return patches
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patches *gomonkey.Patches
			if tt.prepareFunc != nil {
				patches = tt.prepareFunc(t, r)
				if patches != nil {
					defer patches.Reset()
				}
			}

			err := r.handleReplicaChange(context.Background(), tt.sts)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStatefulSetReconciler_ReleaseSubnetPortsForStatefulSet(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	tests := []struct {
		name        string
		prepareFunc func(*testing.T, *subnetportservice.SubnetPortService) *gomonkey.Patches
		wantErr     bool
	}{
		{
			name: "no ports",
			prepareFunc: func(t *testing.T, sps *subnetportservice.SubnetPortService) *gomonkey.Patches {
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortService).ListSubnetPortByStsName,
					func(s *subnetportservice.SubnetPortService, ns string, stsName string) []*model.VpcSubnetPort {
						return []*model.VpcSubnetPort{}
					})
				return patches
			},
			wantErr: false,
		},
		{
			name: "with ports delete success",
			prepareFunc: func(t *testing.T, sps *subnetportservice.SubnetPortService) *gomonkey.Patches {
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortService).ListSubnetPortByStsName,
					func(s *subnetportservice.SubnetPortService, ns string, stsName string) []*model.VpcSubnetPort {
						podNameScope := "nsx-op/pod_name"
						return []*model.VpcSubnetPort{
							{Id: servicecommon.String("port1"), DisplayName: servicecommon.String("test-sts-0"),
								Tags: []model.Tag{{Scope: &podNameScope, Tag: servicecommon.String("test-sts-0")}}},
							{Id: servicecommon.String("port2"), DisplayName: servicecommon.String("test-sts-1"),
								Tags: []model.Tag{{Scope: &podNameScope, Tag: servicecommon.String("test-sts-1")}}},
						}
					})
				patches.ApplyFunc(
					(*subnetportservice.SubnetPortService).DeleteSubnetPort,
					func(s *subnetportservice.SubnetPortService, port *model.VpcSubnetPort) error {
						return nil
					})
				return patches
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patches := tt.prepareFunc(t, subnetPortService)
			if patches != nil {
				defer patches.Reset()
			}

			err := r.releaseSubnetPortsForStatefulSet(context.Background(), "default", "test-sts")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStatefulSetReconciler_ReleaseSubnetPortForPod(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	tests := []struct {
		name        string
		prepareFunc func(*testing.T, *subnetportservice.SubnetPortService) *gomonkey.Patches
		wantErr     bool
	}{
		{
			name: "no ports",
			prepareFunc: func(t *testing.T, sps *subnetportservice.SubnetPortService) *gomonkey.Patches {
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortService).ListSubnetPortByPodName,
					func(s *subnetportservice.SubnetPortService, ns string, name string) []*model.VpcSubnetPort {
						return []*model.VpcSubnetPort{}
					})
				return patches
			},
			wantErr: false,
		},
		{
			name: "with ports delete success",
			prepareFunc: func(t *testing.T, sps *subnetportservice.SubnetPortService) *gomonkey.Patches {
				patches := gomonkey.ApplyFunc(
					(*subnetportservice.SubnetPortService).ListSubnetPortByPodName,
					func(s *subnetportservice.SubnetPortService, ns string, name string) []*model.VpcSubnetPort {
						return []*model.VpcSubnetPort{
							{Id: servicecommon.String("port1")},
						}
					})
				patches.ApplyFunc(
					(*subnetportservice.SubnetPortService).DeleteSubnetPort,
					func(s *subnetportservice.SubnetPortService, port *model.VpcSubnetPort) error {
						return nil
					})
				return patches
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patches := tt.prepareFunc(t, subnetPortService)
			if patches != nil {
				defer patches.Reset()
			}
			err := r.releaseSubnetPortForPod(context.Background(), "default", "test-sts-0")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSetupWithManager(t *testing.T) {
	reconciler := &StatefulSetReconciler{}
	err := reconciler.setupWithManager(nil)
	assert.Error(t, err)
}

func TestStart(t *testing.T) {
	reconciler := &StatefulSetReconciler{}
	err := reconciler.Start(nil)
	assert.Error(t, err)
}

func TestGetOrdinalRange(t *testing.T) {
	tests := []struct {
		name          string
		sts           *appsv1.StatefulSet
		expectedStart int
		expectedEnd   int
	}{
		{
			name: "default ordinals",
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Replicas: func() *int32 { r := int32(3); return &r }(),
				},
			},
			expectedStart: 0,
			expectedEnd:   2,
		},
		{
			name: "custom ordinals start",
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Ordinals: &appsv1.StatefulSetOrdinals{Start: 10},
					Replicas: func() *int32 { r := int32(3); return &r }(),
				},
			},
			expectedStart: 10,
			expectedEnd:   12,
		},
		{
			name: "zero replicas",
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Replicas: func() *int32 { r := int32(0); return &r }(),
				},
			},
			expectedStart: -1,
			expectedEnd:   -1,
		},
		{
			name:          "nil replicas",
			sts:           &appsv1.StatefulSet{},
			expectedStart: -1,
			expectedEnd:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &StatefulSetReconciler{}
			start, end := r.GetOrdinalRange(tt.sts)
			assert.Equal(t, tt.expectedStart, start)
			assert.Equal(t, tt.expectedEnd, end)
		})
	}
}

func TestHandleReplicaChange_WithOrdinals(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
			UID:       "test-sts-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Ordinals: &appsv1.StatefulSetOrdinals{Start: 5},
			Replicas: func() *int32 { r := int32(3); return &r }(),
		},
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByStsUid,
		func(s *subnetportservice.SubnetPortService, ns string, stsUid string) []*model.VpcSubnetPort {
			return []*model.VpcSubnetPort{
				{Id: servicecommon.String("port1"), DisplayName: servicecommon.String("test-sts-0")},
				{Id: servicecommon.String("port2"), DisplayName: servicecommon.String("test-sts-1")},
				{Id: servicecommon.String("port3"), DisplayName: servicecommon.String("test-sts-5")},
				{Id: servicecommon.String("port4"), DisplayName: servicecommon.String("test-sts-6")},
				{Id: servicecommon.String("port5"), DisplayName: servicecommon.String("test-sts-7")},
			}
		})
	patches.ApplyFunc((*StatefulSetReconciler).releaseSubnetPortForPod,
		func(r *StatefulSetReconciler, ctx context.Context, namespace, podName string) error {
			return nil
		})
	defer patches.Reset()

	err := r.handleReplicaChange(context.Background(), sts)
	assert.NoError(t, err)
}

func TestHandleReplicaChange_WithNilDisplayName(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
			UID:       "test-sts-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(2); return &r }(),
		},
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByStsUid,
		func(s *subnetportservice.SubnetPortService, ns string, stsUid string) []*model.VpcSubnetPort {
			return []*model.VpcSubnetPort{
				{Id: servicecommon.String("port1")},
			}
		})
	patches.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByPodName,
		func(s *subnetportservice.SubnetPortService, ns string, name string) []*model.VpcSubnetPort {
			return []*model.VpcSubnetPort{}
		})
	defer patches.Reset()

	err := r.handleReplicaChange(context.Background(), sts)
	assert.NoError(t, err)
}

func TestReleaseSubnetPortForPod_PodExists(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByPodName,
		func(s *subnetportservice.SubnetPortService, ns string, name string) []*model.VpcSubnetPort {
			return []*model.VpcSubnetPort{}
		})
	defer patches.Reset()

	err := r.releaseSubnetPortForPod(context.Background(), "default", "existing-pod")
	assert.NoError(t, err)
}

func TestReleaseSubnetPortsForStatefulSet_PodExists(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts-0",
			Namespace: "default",
		},
	}).Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByStsName,
		func(s *subnetportservice.SubnetPortService, ns string, stsName string) []*model.VpcSubnetPort {
			podNameScope := "nsx-op/pod_name"
			return []*model.VpcSubnetPort{
				{Id: servicecommon.String("port1"), DisplayName: servicecommon.String("test-sts-0"),
					Tags: []model.Tag{{Scope: &podNameScope, Tag: servicecommon.String("test-sts-0")}}},
			}
		})
	defer patches.Reset()

	err := r.releaseSubnetPortsForStatefulSet(context.Background(), "default", "test-sts")
	assert.NoError(t, err)
}

func TestReleaseSubnetPortForPod_DeleteError(t *testing.T) {
	mockCtl := gomock.NewController(t)
	defer mockCtl.Finish()
	k8sClient := mock_client.NewMockClient(mockCtl)

	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            k8sClient,
		SubnetPortService: subnetPortService,
	}

	// Mock Get to return NotFound, so it proceeds to release port
	k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "test-pod-0"))

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByPodName,
		func(s *subnetportservice.SubnetPortService, ns string, podName string) []*model.VpcSubnetPort {
			return []*model.VpcSubnetPort{
				{Id: servicecommon.String("port-delete-error"), DisplayName: servicecommon.String("test-sts-0"), Path: servicecommon.String("/path/to/port")},
			}
		})
	patches.ApplyFunc(
		(*subnetportservice.SubnetPortService).DeleteSubnetPort,
		func(s *subnetportservice.SubnetPortService, port *model.VpcSubnetPort) error {
			return errors.New("delete subnet port failed")
		})
	defer patches.Reset()

	err := r.releaseSubnetPortForPod(context.Background(), "default", "test-pod-0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete subnet port failed")
}

func TestCollectGarbage_OrphanedPortDeleteError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortStore).GetByIndex,
		func(s *subnetportservice.SubnetPortStore, indexKey string, indexValue string) []*model.VpcSubnetPort {
			if indexKey == servicecommon.IndexKeyAllStsPorts {
				stsUIDScope := "nsx-op/sts_uid"
				nsScope := "nsx-op/namespace"
				podNameScope := "nsx-op/pod_name"
				return []*model.VpcSubnetPort{
					{Id: servicecommon.String("port-orphan"),
						Tags: []model.Tag{
							{Scope: &stsUIDScope, Tag: servicecommon.String("deleted-sts-uid")},
							{Scope: &nsScope, Tag: servicecommon.String("default")},
							{Scope: &podNameScope, Tag: servicecommon.String("test-pod-0")},
						}},
				}
			}
			return []*model.VpcSubnetPort{}
		})
	patches.ApplyFunc(
		(*subnetportservice.SubnetPortService).DeleteSubnetPort,
		func(s *subnetportservice.SubnetPortService, port *model.VpcSubnetPort) error {
			return errors.New("delete orphaned port failed")
		})
	defer patches.Reset()

	err := r.CollectGarbage(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete error(s)")
}

func TestCollectGarbage_MixedErrors(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
			UID:       "test-sts-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(2); return &r }(),
		},
	}).Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortStore).GetByIndex,
		func(s *subnetportservice.SubnetPortStore, indexKey string, indexValue string) []*model.VpcSubnetPort {
			if indexKey == servicecommon.TagScopeStatefulSetUID && indexValue == "test-sts-uid" {
				podNameScope := "nsx-op/pod_name"
				return []*model.VpcSubnetPort{
					{Id: servicecommon.String("port-oor"), DisplayName: servicecommon.String("test-sts-5"), Tags: []model.Tag{{Scope: &podNameScope, Tag: servicecommon.String("test-sts-5")}}},
				}
			}
			if indexKey == servicecommon.IndexKeyAllStsPorts {
				stsUIDScope := "nsx-op/sts_uid"
				nsScope := "nsx-op/namespace"
				podNameScope := "nsx-op/pod_name"
				return []*model.VpcSubnetPort{
					{Id: servicecommon.String("port-orphan"),
						Tags: []model.Tag{
							{Scope: &stsUIDScope, Tag: servicecommon.String("deleted-sts-uid")},
							{Scope: &nsScope, Tag: servicecommon.String("default")},
							{Scope: &podNameScope, Tag: servicecommon.String("test-pod-0")},
						}},
				}
			}
			return []*model.VpcSubnetPort{}
		})

	patches.ApplyFunc(
		(*subnetportservice.SubnetPortService).DeleteSubnetPort,
		func(s *subnetportservice.SubnetPortService, port *model.VpcSubnetPort) error {
			if port.Path == nil {
				port.Path = servicecommon.String("/orgs/default/projects/default/vpcs/default/subnets/default/ports/default")
			}
			if port.Id != nil && *port.Id == "port-oor" {
				return errors.New("failed to delete out-of-range port")
			}
			if port.Id != nil && *port.Id == "port-orphan" {
				return errors.New("failed to delete orphaned port")
			}
			return nil
		})
	defer patches.Reset()

	err := r.CollectGarbage(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete out-of-range port")
	assert.Contains(t, err.Error(), "failed to delete orphaned port")
}

func TestCollectGarbage_WithNilDisplayName(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
			UID:       "test-sts-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(2); return &r }(),
		},
	}).Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortStore).GetByIndex,
		func(s *subnetportservice.SubnetPortStore, indexKey string, indexValue string) []*model.VpcSubnetPort {
			if indexKey == servicecommon.TagScopeStatefulSetUID && indexValue == "test-sts-uid" {
				return []*model.VpcSubnetPort{
					{Id: servicecommon.String("port1"), DisplayName: nil},
					{Id: servicecommon.String("port2"), DisplayName: servicecommon.String("test-sts-0")},
				}
			}
			if indexKey == servicecommon.IndexKeyAllStsPorts {
				return []*model.VpcSubnetPort{}
			}
			return []*model.VpcSubnetPort{}
		})
	defer patches.Reset()

	err := r.CollectGarbage(context.Background())
	assert.NoError(t, err)
}

func TestCollectGarbage_WithInvalidIndexDisplayName(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
			UID:       "test-sts-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(2); return &r }(),
		},
	}).Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortStore).GetByIndex,
		func(s *subnetportservice.SubnetPortStore, indexKey string, indexValue string) []*model.VpcSubnetPort {
			if indexKey == servicecommon.TagScopeStatefulSetUID && indexValue == "test-sts-uid" {
				return []*model.VpcSubnetPort{
					{Id: servicecommon.String("port1"), DisplayName: servicecommon.String("invalid-name")},
					{Id: servicecommon.String("port2"), DisplayName: servicecommon.String("test-sts-0")},
				}
			}
			if indexKey == servicecommon.IndexKeyAllStsPorts {
				return []*model.VpcSubnetPort{}
			}
			return []*model.VpcSubnetPort{}
		})
	defer patches.Reset()

	err := r.CollectGarbage(context.Background())
	assert.NoError(t, err)
}

func TestHandleReplicaChange_WithPodStillExists(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts-0",
			Namespace: "default",
		},
	}).Build()
	subnetPortService := &subnetportservice.SubnetPortService{
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            fakeClient,
		SubnetPortService: subnetPortService,
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
			UID:       "test-sts-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(2); return &r }(),
		},
	}

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByStsUid,
		func(s *subnetportservice.SubnetPortService, ns string, stsUid string) []*model.VpcSubnetPort {
			return []*model.VpcSubnetPort{
				{Id: servicecommon.String("port1"), DisplayName: servicecommon.String("test-sts-0")},
			}
		})
	defer patches.Reset()

	err := r.handleReplicaChange(context.Background(), sts)
	assert.NoError(t, err)
}

func TestReconcile_PodCacheNotReady(t *testing.T) {
	mockCtl := gomock.NewController(t)
	defer mockCtl.Finish()
	k8sClient := mock_client.NewMockClient(mockCtl)

	subnetPortService := &subnetportservice.SubnetPortService{
		Service: servicecommon.Service{
			NSXConfig: &config.NSXOperatorConfig{
				NsxConfig: &config.NsxConfig{
					EnforcementPoint: "vmc-enforcementpoint",
				},
			},
		},
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            k8sClient,
		SubnetPortService: subnetPortService,
		Recorder:          fakeRecorder{},
	}
	r.StatusUpdater = common.NewStatusUpdater(k8sClient, r.SubnetPortService.NSXConfig, r.Recorder, MetricResTypeStatefulSet, "SubnetPort", "StatefulSet")

	// Return an arbitrary error from Get to simulate cache not ready
	k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("cache not ready"))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-sts", Namespace: "default"}}
	res, err := r.Reconcile(context.Background(), req)

	assert.Error(t, err)
	assert.Equal(t, "cache not ready", err.Error())
	assert.True(t, res.Requeue)
}

func TestReconcile_WithDeleteAnnotation(t *testing.T) {
	mockCtl := gomock.NewController(t)
	defer mockCtl.Finish()
	k8sClient := mock_client.NewMockClient(mockCtl)

	subnetPortService := &subnetportservice.SubnetPortService{
		Service: servicecommon.Service{
			NSXConfig: &config.NSXOperatorConfig{
				NsxConfig: &config.NsxConfig{
					EnforcementPoint: "vmc-enforcementpoint",
				},
			},
		},
		SubnetPortStore: &subnetportservice.SubnetPortStore{},
	}

	r := &StatefulSetReconciler{
		Client:            k8sClient,
		SubnetPortService: subnetPortService,
		Recorder:          fakeRecorder{},
	}
	r.StatusUpdater = common.NewStatusUpdater(k8sClient, r.SubnetPortService.NSXConfig, r.Recorder, MetricResTypeStatefulSet, "SubnetPort", "StatefulSet")

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-sts",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(2); return &r }(),
		},
	}

	k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			sts.DeepCopyInto(obj.(*appsv1.StatefulSet))
			return nil
		})

	patches := gomonkey.ApplyFunc(
		(*subnetportservice.SubnetPortService).ListSubnetPortByStsName,
		func(s *subnetportservice.SubnetPortService, ns string, stsName string) []*model.VpcSubnetPort {
			return nil
		})
	defer patches.Reset()

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-sts", Namespace: "default"}}
	res, err := r.Reconcile(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, common.ResultNormal, res)
}
