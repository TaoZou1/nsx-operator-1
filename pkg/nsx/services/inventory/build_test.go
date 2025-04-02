package inventory

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"

	"github.com/vmware-tanzu/nsx-operator/pkg/config"
	mockClient "github.com/vmware-tanzu/nsx-operator/pkg/mock/controller-runtime/client"
	"github.com/vmware-tanzu/nsx-operator/pkg/nsx"
	"github.com/vmware-tanzu/nsx-operator/pkg/nsx/ratelimiter"
	commonservice "github.com/vmware-tanzu/nsx-operator/pkg/nsx/services/common"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vmware/go-vmware-nsxt/common"
	"github.com/vmware/go-vmware-nsxt/containerinventory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createService(t *testing.T) (*InventoryService, *mockClient.MockClient) {
	config2 := nsx.NewConfig("localhost", "1", "1", []string{}, 10, 3, 20, 20, true, true, true, ratelimiter.AIMD, nil, nil, []string{"127.0.0.1"})

	cluster, _ := nsx.NewCluster(config2)
	rc := cluster.NewRestConnector()

	mockCtrl := gomock.NewController(t)
	k8sClient := mockClient.NewMockClient(mockCtrl)
	httpClient := http.DefaultClient
	cf := &config.NSXOperatorConfig{
		CoeConfig: &config.CoeConfig{
			Cluster: "k8scl-one:test",
		},
		NsxConfig: &config.NsxConfig{NsxApiManagers: []string{"127.0.0.1"}},
	}
	nsxApiClient, _ := nsx.CreateNsxtApiClient(cf, httpClient)
	commonservice := commonservice.Service{
		Client: k8sClient,
		NSXClient: &nsx.Client{
			RestConnector: rc,
			NsxConfig:     cf,
			NsxApiClient:  nsxApiClient,
		},
		NSXConfig: &config.NSXOperatorConfig{
			CoeConfig: &config.CoeConfig{
				Cluster: "k8scl-one:test",
			},
		},
	}

	service, _ := InitializeService(commonservice)
	return service, k8sClient
}

func TestBuildPod(t *testing.T) {
	labels := make(map[string]string)
	labels["app"] = "inventory"
	labels["role"] = "test-only"
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node",
			UID:    "111111111",
			Labels: labels,
		},
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "namespace",
			UID:    "222222222",
			Labels: labels,
		},
	}
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid-123",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	t.Run("NormalFlow", func(t *testing.T) {
		inventoryService, k8sClient := createService(t)

		k8sClient.EXPECT().
			Get(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.ListOption) error {
				ns, ok := obj.(*corev1.Namespace)
				if !ok {
					return nil
				}
				namespace.DeepCopyInto(ns)
				return nil
			})
		k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.ListOption) error {
				result, ok := obj.(*corev1.Node)
				if !ok {
					return nil
				}
				node.DeepCopyInto(result)
				return nil
			})

		testPod.Status = corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIPs: []corev1.PodIP{
				{IP: "192.168.1.1"},
			},
		}
		applicationInstance := &containerinventory.ContainerApplicationInstance{}
		applicationInstance.ContainerApplicationIds = []string{"app-id-123"}
		inventoryService.pendingAdd[string(testPod.UID)] = applicationInstance
		retry := inventoryService.BuildPod(testPod)

		assert.False(t, retry)
		assert.Contains(t, inventoryService.pendingAdd, "pod-uid-123")
		applicationInstance = inventoryService.pendingAdd["pod-uid-123"].(*containerinventory.ContainerApplicationInstance)
		assert.Equal(t, applicationInstance.ContainerApplicationIds, []string{"app-id-123"})
		assert.Equal(t, applicationInstance.ContainerProjectId, string(namespace.UID))
		assert.Equal(t, applicationInstance.ClusterNodeId, string(node.UID))
		assert.Equal(t, applicationInstance.ExternalId, string(testPod.UID))
		assert.Equal(t, applicationInstance.ContainerClusterId, inventoryService.NSXConfig.Cluster)

		keypaire := common.KeyValuePair{Key: "ip", Value: "192.168.1.1"}
		assert.Contains(t, applicationInstance.OriginProperties, keypaire)
	})

	t.Run("NamespaceNotFound", func(t *testing.T) {
		inventoryService, k8sClient := createService(t)
		k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("not found"))
		retry := inventoryService.BuildPod(testPod)
		assert.True(t, retry)
	})

	t.Run("NodeNotFoundWithRetry", func(t *testing.T) {
		inventoryService, k8sClient := createService(t)
		k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("node not found"))
		retry := inventoryService.BuildPod(testPod)
		assert.True(t, retry)
	})
}

func TestGetTagsFromLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected []common.Tag
	}{
		{
			name: "normal case with sorted keys",
			input: map[string]string{
				"app":     "nginx",
				"version": "1.19",
			},
			expected: []common.Tag{
				{Scope: "k8s.label.app", Tag: "nginx"},
				{Scope: "k8s.label.version", Tag: "1.19"},
			},
		},
		{
			name: "long label value truncation",
			input: map[string]string{
				"key": "this-is-a-very-long-value-that-exceeds-max-tag-length-1234567890",
			},
			expected: []common.Tag{
				{Scope: "k8s.label.key", Tag: "this-is-a-very-long-value-that-exceed"},
			},
		},
		{
			name: "long key normalization with hash suffix",
			input: map[string]string{
				"this-is-an-extra-long-label-key-that-needs-normalization": "value",
			},
			expected: []common.Tag{
				{
					Scope: "k8s.label.this-is-an-extra-long-label-key-that--4d35f159",
					Tag:   "value",
				},
			},
		},
		{
			name:     "empty labels input",
			input:    map[string]string{},
			expected: []common.Tag{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetTagsFromLabels(tt.input)

			// Validate tag count matches expectation
			assert.Equal(t, len(tt.expected), len(result), "Tag count mismatch")

			// Verify alphabetical order of scopes
			for i := 1; i < len(result); i++ {
				assert.True(t, result[i-1].Scope < result[i].Scope, "Tags not sorted alphabetically")
			}

			// Validate individual tag properties
			for i := range tt.expected {
				if i >= len(result) {
					break
				}

				// Check k8s label prefix
				assert.Contains(t, result[i].Scope, InventoryK8sPrefix, "Missing k8s label prefix")

				// Validate scope length limit
				assert.True(t, len(result[i].Scope) <= MaxResourceTypeLen,
					"Scope length exceeds limit, actual length: %d", len(result[i].Scope))

				// Verify value truncation when needed
				originalValue := tt.input[result[i].Scope[len(InventoryK8sPrefix):]]
				if len(originalValue) > MaxTagLen {
					assert.Equal(t, MaxTagLen, len(result[i].Tag), "Value not properly truncated")
				}
			}
		})
	}
}

func TestIsIPChanged(t *testing.T) {
	type testCase struct {
		name     string
		pre      containerinventory.ContainerApplicationInstance
		cur      containerinventory.ContainerApplicationInstance
		expected bool
	}

	buildInstance := func(ipValue string) containerinventory.ContainerApplicationInstance {
		var props []common.KeyValuePair
		if ipValue != "" {
			props = append(props, common.KeyValuePair{
				Key:   "ip",
				Value: ipValue,
			})
		}
		return containerinventory.ContainerApplicationInstance{
			DisplayName:      "test-pod",
			OriginProperties: props,
		}
	}

	tests := []testCase{
		{
			name:     "no ips",
			pre:      buildInstance(""),
			cur:      buildInstance(""),
			expected: false,
		},
		{
			name:     "ip added",
			pre:      buildInstance(""),
			cur:      buildInstance("192.168.1.1"),
			expected: true,
		},
		{
			name:     "ip count changed",
			pre:      buildInstance("10.0.0.1"),
			cur:      buildInstance("10.0.0.1,192.168.1.1"),
			expected: true,
		},
		{
			name:     "single ip same",
			pre:      buildInstance("192.168.1.1"),
			cur:      buildInstance("192.168.1.1"),
			expected: false,
		},
		{
			name:     "single ip different",
			pre:      buildInstance("192.168.1.1"),
			cur:      buildInstance("10.0.0.1"),
			expected: true,
		},
		{
			name:     "dual ips order changed",
			pre:      buildInstance("192.168.1.1,10.0.0.1"),
			cur:      buildInstance("10.0.0.1,192.168.1.1"),
			expected: false,
		},
		{
			name:     "dual ips content changed",
			pre:      buildInstance("192.168.1.1,10.0.0.1"),
			cur:      buildInstance("192.168.1.1,10.0.0.2"),
			expected: true,
		},
		{
			name:     "multiple ips same",
			pre:      buildInstance("a,b,c"),
			cur:      buildInstance("a,b,c"),
			expected: false,
		},
		{
			name:     "multiple ips different",
			pre:      buildInstance("a,b,c"),
			cur:      buildInstance("d,e,f"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPChanged(tt.pre, tt.cur)
			if got != tt.expected {
				t.Errorf("isIPChanged() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildService(t *testing.T) {
	labels := make(map[string]string)
	labels["app"] = "inventory"
	labels["role"] = "test-only"
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "namespace",
			UID:    "222222222",
			Labels: labels,
		},
	}
	testService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			UID:       "service-uid-123",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	t.Run("NormalFlow", func(t *testing.T) {
		inventoryService, k8sClient := createService(t)

		k8sClient.EXPECT().
			Get(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.ListOption) error {
				ns, ok := obj.(*corev1.Namespace)
				if !ok {
					return nil
				}
				namespace.DeepCopyInto(ns)
				return nil
			})

		patches := gomonkey.ApplyFunc(GetPodIDsFromEndpoint,
			func(_ context.Context, _ client.Client, _ string, _ string) ([]string, bool) {
				return []string{"1", "2"}, false
			})
		defer patches.Reset()

		// ApplyMethod private synchronizeServiceIDsWithApplicationInstances
		patches.ApplyPrivateMethod(reflect.TypeOf(inventoryService), "synchronizeServiceIDsWithApplicationInstances",
			func(_ *InventoryService, podUIDs []string, service *corev1.Service) (retry bool) {
				return false
			})

		retry := inventoryService.BuildService(testService)

		assert.False(t, retry)
		assert.Contains(t, inventoryService.pendingAdd, "service-uid-123")
		containerApplication := inventoryService.pendingAdd["service-uid-123"].(*containerinventory.ContainerApplication)
		assert.Equal(t, containerApplication.ContainerProjectId, string(namespace.UID))
		assert.Equal(t, containerApplication.ExternalId, string(testService.UID))
		assert.Equal(t, containerApplication.ContainerClusterId, inventoryService.NSXConfig.Cluster)
		assert.Equal(t, containerApplication.Status, InventoryStatusUp)
		assert.Equal(t, containerApplication.NetworkStatus, NetworkStatusHealthy)
	})

	t.Run("NamespaceNotFound", func(t *testing.T) {
		inventoryService, k8sClient := createService(t)
		k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("not found"))
		retry := inventoryService.BuildService(testService)
		assert.True(t, retry)
	})

}

func TestBuildNamespace(t *testing.T) {
	labels := make(map[string]string)
	labels["environment"] = "test"
	labels["team"] = "platform"

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-namespace",
			UID:    "namespace-uid-123",
			Labels: labels,
		},
	}

	t.Run("NormalFlow", func(t *testing.T) {
		inventoryService, _ := createService(t)

		// Test with a new namespace
		retry := inventoryService.BuildNamespace(testNamespace)

		assert.False(t, retry)
		assert.Contains(t, inventoryService.pendingAdd, "namespace-uid-123")

		containerProject := inventoryService.pendingAdd["namespace-uid-123"].(*containerinventory.ContainerProject)
		assert.Equal(t, "test-namespace", containerProject.DisplayName)
		assert.Equal(t, string(ContainerProject), containerProject.ResourceType)
		assert.Equal(t, string(testNamespace.UID), containerProject.ExternalId)
		assert.Equal(t, inventoryService.NSXConfig.Cluster, containerProject.ContainerClusterId)
		assert.Equal(t, NetworkStatusHealthy, containerProject.NetworkStatus)

		// Verify tags are created from labels
		expectedTags := GetTagsFromLabels(labels)
		assert.Equal(t, len(expectedTags), len(containerProject.Tags))
		for i, tag := range containerProject.Tags {
			assert.Equal(t, expectedTags[i].Scope, tag.Scope)
			assert.Equal(t, expectedTags[i].Tag, tag.Tag)
		}
	})

	t.Run("UpdateExistingNamespace", func(t *testing.T) {
		inventoryService, _ := createService(t)

		// Create a pre-existing namespace in the ProjectStore
		existingProject := &containerinventory.ContainerProject{
			DisplayName:        "old-name",
			ResourceType:       string(ContainerProject),
			Tags:               []common.Tag{},
			ContainerClusterId: inventoryService.NSXConfig.Cluster,
			ExternalId:         string(testNamespace.UID),
			NetworkStatus:      NetworkStatusHealthy,
		}

		// Add to ProjectStore
		inventoryService.ProjectStore.Add(existingProject)

		// Now build the namespace with updated information
		updatedNamespace := testNamespace.DeepCopy()
		updatedNamespace.Labels["new-label"] = "new-value"

		retry := inventoryService.BuildNamespace(updatedNamespace)

		assert.False(t, retry)
		assert.Contains(t, inventoryService.pendingAdd, "namespace-uid-123")

		// Verify the updated namespace is in pendingAdd
		containerProject := inventoryService.pendingAdd["namespace-uid-123"].(*containerinventory.ContainerProject)
		assert.Equal(t, "test-namespace", containerProject.DisplayName)

		// Verify the updated tags include the new label
		found := false
		for _, tag := range containerProject.Tags {
			if tag.Scope == "dis:k8s:new-label" && tag.Tag == "new-value" {
				found = true
				break
			}
		}
		assert.True(t, found, "New label should be included in tags")
	})

	t.Run("NoChangeToNamespace", func(t *testing.T) {
		inventoryService, _ := createService(t)

		// First build creates the initial namespace
		inventoryService.BuildNamespace(testNamespace)

		// Clear pendingAdd to simulate a completed sync
		initialProject := inventoryService.pendingAdd[string(testNamespace.UID)]
		delete(inventoryService.pendingAdd, string(testNamespace.UID))

		// Add to ProjectStore to simulate it's already been processed
		inventoryService.ProjectStore.Add(initialProject)

		// Build the same namespace again without changes
		retry := inventoryService.BuildNamespace(testNamespace)

		assert.False(t, retry)
		// Since there are no changes, it shouldn't be added to pendingAdd
		assert.NotContains(t, inventoryService.pendingAdd, string(testNamespace.UID))
	})
}

func TestSynchronizeServiceIDsWithApplicationInstances(t *testing.T) {
	t.Run("UpdateServiceIDs", func(t *testing.T) {
		inventoryService, k8sClient := createService(t)

		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "default",
				UID:       "service-uid-789",
			},
		}

		// Add an application instance to the store
		inventoryService.ApplicationInstanceStore.Add(&containerinventory.ContainerApplicationInstance{
			ExternalId:              "pod-uid-123",
			ContainerApplicationIds: []string{},
		})

		// Expect the List function to be called and mock the behavior
		k8sClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		patches := gomonkey.ApplyFunc(GetPodByUID,
			func(ctx context.Context, client client.Client, uid types.UID, namespace string) (*corev1.Pod, error) {
				return &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID: uid,
					},
				}, nil
			},
		).ApplyFunc(GetServicesUIDByPodUID,
			func(_ context.Context, _ client.Client, _ types.UID, _ string) ([]string, error) {
				return []string{"service-uid-234", "service-uid-456"}, nil
			},
		)
		defer patches.Reset()

		podUIDs := []string{"pod-uid-123"}
		retry := inventoryService.synchronizeServiceIDsWithApplicationInstances(podUIDs, service)
		assert.False(t, retry)

		instance := inventoryService.ApplicationInstanceStore.GetByKey("pod-uid-123").(*containerinventory.ContainerApplicationInstance)
		assert.Contains(t, instance.ContainerApplicationIds, "service-uid-234")
		assert.Contains(t, instance.ContainerApplicationIds, "service-uid-456")
	})

	t.Run("RemoveStaleServiceIDs", func(t *testing.T) {
		inventoryService, _ := createService(t)

		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "default",
				UID:       "service-uid-789",
			},
		}

		staleInstance := &containerinventory.ContainerApplicationInstance{
			ExternalId:              "stale-pod-uid",
			ContainerApplicationIds: []string{"service-uid-789"},
		}

		inventoryService.ApplicationInstanceStore.Add(staleInstance)

		podUIDs := []string{"pod-uid-123"}
		inventoryService.removeStaleServiceIDsFromApplicationInstances(podUIDs, service)

		updatedInstance := inventoryService.ApplicationInstanceStore.GetByKey("stale-pod-uid").(*containerinventory.ContainerApplicationInstance)
		assert.NotContains(t, updatedInstance.ContainerApplicationIds, string(service.UID))
	})
}

func TestApplyServiceIDUpdates(t *testing.T) {
	inventoryService, _ := createService(t)

	instance := &containerinventory.ContainerApplicationInstance{
		ExternalId:              "pod-uid-123",
		ContainerApplicationIds: []string{},
	}
	updatedServiceIDs := []string{"service-uid-456", "service-uid-789"}

	inventoryService.applyServiceIDUpdates(instance, updatedServiceIDs)

	assert.Equal(t, updatedServiceIDs, instance.ContainerApplicationIds)
	assert.Equal(t, 1, len(inventoryService.requestBuffer))
	assert.Equal(t, instance.ExternalId, inventoryService.requestBuffer[0].ContainerObject["external_id"])
}

func TestUpdateServiceIDsForApplicationInstance(t *testing.T) {
	inventoryService, k8sClient := createService(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			UID:       "service-uid-123",
		},
	}

	podUID := "pod-uid-123"
	instance := &containerinventory.ContainerApplicationInstance{
		ExternalId: podUID,
	}
	inventoryService.ApplicationInstanceStore.Add(instance)

	k8sClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	patches := gomonkey.ApplyFunc(GetPodByUID,
		func(ctx context.Context, client client.Client, uid types.UID, namespace string) (*corev1.Pod, error) {
			return &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID: uid,
				},
			}, nil
		},
	).ApplyFunc(GetServicesUIDByPodUID,
		func(_ context.Context, _ client.Client, _ types.UID, _ string) ([]string, error) {
			return []string{"service-uid-234", "service-uid-345"}, nil
		},
	)
	defer patches.Reset()

	retry := inventoryService.updateServiceIDsForApplicationInstance(podUID, service)

	assert.False(t, retry)
	assert.Contains(t, instance.ContainerApplicationIds, "service-uid-234")
	assert.Contains(t, instance.ContainerApplicationIds, "service-uid-345")
}

func TestRemoveStaleServiceIDsFromApplicationInstances(t *testing.T) {
	inventoryService, _ := createService(t)

	// Create a service which has a UID that should be removed
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			UID:       "service-uid-789",
		},
	}

	// Add application instances to the store, some of which will have stale service UIDs
	instanceWithStaleID := &containerinventory.ContainerApplicationInstance{
		ExternalId:              "pod-uid-123",
		ContainerApplicationIds: []string{"service-uid-789", "service-uid-456"},
	}

	instanceWithValidID := &containerinventory.ContainerApplicationInstance{
		ExternalId:              "pod-uid-456",
		ContainerApplicationIds: []string{"service-uid-456"},
	}

	inventoryService.ApplicationInstanceStore.Add(instanceWithStaleID)
	inventoryService.ApplicationInstanceStore.Add(instanceWithValidID)

	// Simulate the list of pod UIDs that are currently valid
	podUIDs := []string{"pod-uid-456"}

	inventoryService.removeStaleServiceIDsFromApplicationInstances(podUIDs, service)

	// Verify that the stale UID is removed from the first instance
	updatedInstanceWithStaleID := inventoryService.ApplicationInstanceStore.GetByKey("pod-uid-123").(*containerinventory.ContainerApplicationInstance)
	assert.NotContains(t, updatedInstanceWithStaleID.ContainerApplicationIds, "service-uid-789")
	assert.Contains(t, updatedInstanceWithStaleID.ContainerApplicationIds, "service-uid-456")

	// Verify that the instance with valid IDs remains unchanged
	updatedInstanceWithValidID := inventoryService.ApplicationInstanceStore.GetByKey("pod-uid-456").(*containerinventory.ContainerApplicationInstance)
	assert.Contains(t, updatedInstanceWithValidID.ContainerApplicationIds, "service-uid-456")
}
