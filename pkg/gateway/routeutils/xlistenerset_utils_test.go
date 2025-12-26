package routeutils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwxalpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
)

func TestGetAttachedXListenerSets(t *testing.T) {
	scheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(scheme)
	gwv1.AddToScheme(scheme)
	gwxalpha1.AddToScheme(scheme)

	gw := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}

	// Create XListenerSets with different creation times
	now := metav1.Now()
	older := metav1.NewTime(now.Add(-time.Hour))
	newer := metav1.NewTime(now.Add(time.Hour))

	attachedLS1 := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "attached-1",
			Namespace:         "default",
			CreationTimestamp: older,
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{
				Name: "test-gateway",
			},
		},
	}

	attachedLS2 := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "attached-2",
			Namespace:         "default",
			CreationTimestamp: newer,
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{
				Name: "test-gateway",
			},
		},
	}

	unattachedLS := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unattached",
			Namespace: "default",
		},
		Spec: gwxalpha1.ListenerSetSpec{
			ParentRef: gwxalpha1.ParentGatewayReference{
				Name: "other-gateway",
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, attachedLS1, attachedLS2, unattachedLS).
		Build()

	attached, err := GetAttachedXListenerSets(context.Background(), client, gw)
	assert.NoError(t, err)
	assert.Len(t, attached, 2)

	// Should be sorted by creation time (older first)
	assert.Equal(t, "attached-1", attached[0].Name)
	assert.Equal(t, "attached-2", attached[1].Name)
}

func TestMergeListenersWithXListenerSets(t *testing.T) {
	gw := &gwv1.Gateway{
		Spec: gwv1.GatewaySpec{
			Listeners: []gwv1.Listener{
				{
					Name:     "gw-listener",
					Port:     80,
					Protocol: gwv1.HTTPProtocolType,
				},
			},
		},
	}

	listenerSets := []gwxalpha1.XListenerSet{
		{
			Spec: gwxalpha1.ListenerSetSpec{
				Listeners: []gwxalpha1.ListenerEntry{
					{
						Name:     "ls-listener-1",
						Port:     443,
						Protocol: gwv1.HTTPSProtocolType,
					},
				},
			},
		},
		{
			Spec: gwxalpha1.ListenerSetSpec{
				Listeners: []gwxalpha1.ListenerEntry{
					{
						Name:     "ls-listener-2",
						Port:     8080,
						Protocol: gwv1.HTTPProtocolType,
					},
				},
			},
		},
	}

	merged := MergeListenersWithXListenerSets(gw, listenerSets)
	assert.Len(t, merged, 3)

	// Gateway listener should be first
	assert.Equal(t, gwv1.SectionName("gw-listener"), merged[0].Name)
	assert.Equal(t, gwv1.PortNumber(80), merged[0].Port)

	// XListenerSet listeners should follow
	assert.Equal(t, gwv1.SectionName("ls-listener-1"), merged[1].Name)
	assert.Equal(t, gwv1.PortNumber(443), merged[1].Port)
	assert.Equal(t, gwv1.SectionName("ls-listener-2"), merged[2].Name)
	assert.Equal(t, gwv1.PortNumber(8080), merged[2].Port)
}

func TestIsXListenerSetAttachedToGateway(t *testing.T) {
	gw := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
	}

	tests := []struct {
		name     string
		ls       *gwxalpha1.XListenerSet
		expected bool
	}{
		{
			name: "attached - same name and namespace",
			ls: &gwxalpha1.XListenerSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: gwxalpha1.ListenerSetSpec{
					ParentRef: gwxalpha1.ParentGatewayReference{Name: "test-gateway"},
				},
			},
			expected: true,
		},
		{
			name: "not attached - different name",
			ls: &gwxalpha1.XListenerSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: gwxalpha1.ListenerSetSpec{
					ParentRef: gwxalpha1.ParentGatewayReference{Name: "other-gateway"},
				},
			},
			expected: false,
		},
		{
			name: "not attached - different namespace",
			ls: &gwxalpha1.XListenerSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: "other"},
				Spec: gwxalpha1.ListenerSetSpec{
					ParentRef: gwxalpha1.ParentGatewayReference{Name: "test-gateway"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isXListenerSetAttachedToGateway(tt.ls, gw)
			assert.Equal(t, tt.expected, result)
		})
	}
}
