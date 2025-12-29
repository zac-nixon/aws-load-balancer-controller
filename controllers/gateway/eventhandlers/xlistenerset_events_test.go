package eventhandlers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwxalpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
)

// setupTestClient creates a test client with GatewayClass and Gateway
func setupTestClient() client.Client {
	scheme := runtime.NewScheme()
	_ = gwv1.AddToScheme(scheme)
	_ = gwxalpha1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create a GatewayClass
	gatewayClass := &gwv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-class",
		},
		Spec: gwv1.GatewayClassSpec{
			ControllerName: "test-controller",
		},
	}
	_ = client.Create(context.Background(), gatewayClass)

	// Create a Gateway
	gateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gw",
			Namespace: "default",
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}
	_ = client.Create(context.Background(), gateway)

	return client
}

func TestXListenerSetEventHandler_Create(t *testing.T) {
	client := setupTestClient()
	eventRecorder := record.NewFakeRecorder(10)
	logger := logr.Discard()

	handler := NewEnqueueRequestsForXListenerSetEventHandler(client, eventRecorder, "test-controller", logger)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())

	// Create XListenerSet
	listenerSet := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ls",
			Namespace: "default",
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{Name: "test-gw"},
			Listeners: []gwxalpha1.ListenerEntry{
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
			},
		},
	}

	createEvent := event.TypedCreateEvent[*gwxalpha1.XListenerSet]{
		Object: listenerSet,
	}

	handler.Create(context.Background(), createEvent, queue)

	// Verify that the Gateway was enqueued
	assert.Equal(t, 1, queue.Len())
	item, _ := queue.Get()
	expectedReq := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-gw",
			Namespace: "default",
		},
	}
	assert.Equal(t, expectedReq, item)
}

func TestXListenerSetEventHandler_Update(t *testing.T) {
	client := setupTestClient()
	eventRecorder := record.NewFakeRecorder(10)
	logger := logr.Discard()

	handler := NewEnqueueRequestsForXListenerSetEventHandler(client, eventRecorder, "test-controller", logger)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())

	// Create XListenerSet
	listenerSet := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ls",
			Namespace: "default",
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{Name: "test-gw"},
			Listeners: []gwxalpha1.ListenerEntry{
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
			},
		},
	}

	updateEvent := event.TypedUpdateEvent[*gwxalpha1.XListenerSet]{
		ObjectOld: listenerSet,
		ObjectNew: listenerSet,
	}

	handler.Update(context.Background(), updateEvent, queue)

	// Verify that the Gateway was enqueued
	assert.Equal(t, 1, queue.Len())
}

func TestXListenerSetEventHandler_Delete(t *testing.T) {
	client := setupTestClient()
	eventRecorder := record.NewFakeRecorder(10)
	logger := logr.Discard()

	handler := NewEnqueueRequestsForXListenerSetEventHandler(client, eventRecorder, "test-controller", logger)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())

	// Create XListenerSet
	listenerSet := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ls",
			Namespace: "default",
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{Name: "test-gw"},
			Listeners: []gwxalpha1.ListenerEntry{
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
			},
		},
	}

	deleteEvent := event.TypedDeleteEvent[*gwxalpha1.XListenerSet]{
		Object: listenerSet,
	}

	handler.Delete(context.Background(), deleteEvent, queue)

	// Verify that the Gateway was enqueued
	assert.Equal(t, 1, queue.Len())
}

func TestXListenerSetEventHandler_NonExistentGateway(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = gwv1.AddToScheme(scheme)
	_ = gwxalpha1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	eventRecorder := record.NewFakeRecorder(10)
	logger := logr.Discard()

	handler := NewEnqueueRequestsForXListenerSetEventHandler(client, eventRecorder, "test-controller", logger)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())

	// Create XListenerSet referencing non-existent Gateway
	listenerSet := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ls",
			Namespace: "default",
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{Name: "non-existent-gw"},
			Listeners: []gwxalpha1.ListenerEntry{
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
			},
		},
	}

	createEvent := event.TypedCreateEvent[*gwxalpha1.XListenerSet]{
		Object: listenerSet,
	}

	handler.Create(context.Background(), createEvent, queue)

	// Verify that no Gateway was enqueued since it doesn't exist
	assert.Equal(t, 0, queue.Len())
}

func TestXListenerSetEventHandler_Update_ParentRefChange(t *testing.T) {
	client := setupTestClient()
	eventRecorder := record.NewFakeRecorder(10)
	logger := logr.Discard()

	handler := NewEnqueueRequestsForXListenerSetEventHandler(client, eventRecorder, "test-controller", logger)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())

	// Create second Gateway
	gateway2 := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-gw",
			Namespace: "default",
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}
	err := client.Create(context.Background(), gateway2)
	assert.NoError(t, err)

	// Create XListenerSet with old parentRef
	oldListenerSet := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ls",
			Namespace: "default",
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{Name: "test-gw"},
			Listeners: []gwxalpha1.ListenerEntry{
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
			},
		},
	}

	// Create XListenerSet with new parentRef
	newListenerSet := oldListenerSet.DeepCopy()
	newListenerSet.Spec.ParentRef.Name = "new-gw"

	updateEvent := event.TypedUpdateEvent[*gwxalpha1.XListenerSet]{
		ObjectOld: oldListenerSet,
		ObjectNew: newListenerSet,
	}

	handler.Update(context.Background(), updateEvent, queue)

	// Verify that both Gateways were enqueued (old and new)
	assert.Equal(t, 2, queue.Len())
}
