package listenerset

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6

func buildFakeClient(objects ...client.Object) client.Client {
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	gwv1.AddToScheme(k8sSchema)
	return testclient.NewClientBuilder().
		WithScheme(k8sSchema).
		WithObjects(objects...).
		Build()
}

func testLogger() logr.Logger {
	return logr.New(&log.NullLogSink{})
}

func makeGateway(name, namespace string, allowedListeners *gwv1.AllowedListeners) gwv1.Gateway {
	return gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gwv1.Listener{
				{Name: "default", Port: 80, Protocol: gwv1.HTTPProtocolType},
			},
			AllowedListeners: allowedListeners,
		},
	}
}

func makeListenerSet(name, namespace, gwName, gwNamespace string, port gwv1.PortNumber) *gwv1.ListenerSet {
	ns := gwv1.Namespace(gwNamespace)
	return &gwv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1.ListenerSetSpec{
			ParentRef: gwv1.ParentGatewayReference{
				Name:      gwv1.ObjectName(gwName),
				Namespace: &ns,
			},
			Listeners: []gwv1.ListenerEntry{
				{
					Name:     gwv1.SectionName(fmt.Sprintf("%s-listener", name)),
					Port:     port,
					Protocol: gwv1.HTTPProtocolType,
				},
			},
		},
	}
}

func makeNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func fromPtr(f gwv1.FromNamespaces) *gwv1.FromNamespaces {
	return &f
}

func TestLoadListenerSetsForGateway(t *testing.T) {
	tests := []struct {
		name             string
		gateway          gwv1.Gateway
		objects          []client.Object
		expectedAccepted int
		expectedRejected int
		checkFunc        func(t *testing.T, result *ListenerSetLoadResult)
	}{
		{
			name: "no ListenerSets exist - empty result",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{From: fromPtr(gwv1.NamespacesFromAll)},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
			},
			expectedAccepted: 0,
			expectedRejected: 0,
		},
		{
			name:    "Gateway with nil allowedListeners - all rejected",
			gateway: makeGateway("gw", "default", nil),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeListenerSet("ls-1", "default", "gw", "default", 8080),
				makeListenerSet("ls-2", "default", "gw", "default", 8081),
			},
			expectedAccepted: 0,
			expectedRejected: 2,
			checkFunc: func(t *testing.T, result *ListenerSetLoadResult) {
				for _, r := range result.RejectedSets {
					assert.Equal(t, "NotAllowed", r.Reason)
					assert.Contains(t, r.Message, "does not allow ListenerSets")
				}
			},
		},
		{
			name: "Policy=Same with same-namespace LS - accepted",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{From: fromPtr(gwv1.NamespacesFromSame)},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeListenerSet("ls-same", "default", "gw", "default", 8080),
			},
			expectedAccepted: 1,
			expectedRejected: 0,
			checkFunc: func(t *testing.T, result *ListenerSetLoadResult) {
				assert.Equal(t, "ls-same", result.AcceptedSets[0].ListenerSet.Name)
			},
		},
		{
			name: "Policy=Same with different-namespace LS - rejected",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{From: fromPtr(gwv1.NamespacesFromSame)},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeNamespace("other-ns", nil),
				makeListenerSet("ls-other", "other-ns", "gw", "default", 8080),
			},
			expectedAccepted: 0,
			expectedRejected: 1,
			checkFunc: func(t *testing.T, result *ListenerSetLoadResult) {
				assert.Equal(t, "ls-other", result.RejectedSets[0].ListenerSet.Name)
				assert.Equal(t, "NotAllowed", result.RejectedSets[0].Reason)
			},
		},
		{
			name: "Policy=All - all accepted regardless of namespace",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{From: fromPtr(gwv1.NamespacesFromAll)},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeNamespace("team-a", nil),
				makeNamespace("team-b", nil),
				makeListenerSet("ls-default", "default", "gw", "default", 8080),
				makeListenerSet("ls-team-a", "team-a", "gw", "default", 8081),
				makeListenerSet("ls-team-b", "team-b", "gw", "default", 8082),
			},
			expectedAccepted: 3,
			expectedRejected: 0,
		},
		{
			name: "Policy=Selector with matching labels - accepted",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{
					From: fromPtr(gwv1.NamespacesFromSelector),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"},
					},
				},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeNamespace("prod-ns", map[string]string{"env": "prod"}),
				makeListenerSet("ls-prod", "prod-ns", "gw", "default", 8080),
			},
			expectedAccepted: 1,
			expectedRejected: 0,
			checkFunc: func(t *testing.T, result *ListenerSetLoadResult) {
				assert.Equal(t, "ls-prod", result.AcceptedSets[0].ListenerSet.Name)
			},
		},
		{
			name: "Policy=Selector with non-matching labels - rejected",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{
					From: fromPtr(gwv1.NamespacesFromSelector),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"},
					},
				},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeNamespace("dev-ns", map[string]string{"env": "dev"}),
				makeListenerSet("ls-dev", "dev-ns", "gw", "default", 8080),
			},
			expectedAccepted: 0,
			expectedRejected: 1,
			checkFunc: func(t *testing.T, result *ListenerSetLoadResult) {
				assert.Equal(t, "ls-dev", result.RejectedSets[0].ListenerSet.Name)
			},
		},
		{
			name: "Policy=Selector with nil selector - rejected",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{
					From:     fromPtr(gwv1.NamespacesFromSelector),
					Selector: nil,
				},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeNamespace("any-ns", map[string]string{"env": "prod"}),
				makeListenerSet("ls-any", "any-ns", "gw", "default", 8080),
			},
			expectedAccepted: 0,
			expectedRejected: 1,
		},
		{
			name: "ListenerSet referencing a different Gateway - not included",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{From: fromPtr(gwv1.NamespacesFromAll)},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeListenerSet("ls-other-gw", "default", "other-gateway", "default", 8080),
			},
			expectedAccepted: 0,
			expectedRejected: 0,
		},
		{
			name: "Mixed scenario - some accepted, some rejected",
			gateway: makeGateway("gw", "default", &gwv1.AllowedListeners{
				Namespaces: &gwv1.ListenerNamespaces{From: fromPtr(gwv1.NamespacesFromSame)},
			}),
			objects: []client.Object{
				makeNamespace("default", nil),
				makeNamespace("other-ns", nil),
				makeListenerSet("ls-same-1", "default", "gw", "default", 8080),
				makeListenerSet("ls-same-2", "default", "gw", "default", 8081),
				makeListenerSet("ls-diff-1", "other-ns", "gw", "default", 8082),
			},
			expectedAccepted: 2,
			expectedRejected: 1,
			checkFunc: func(t *testing.T, result *ListenerSetLoadResult) {
				acceptedNames := make(map[string]bool)
				for _, a := range result.AcceptedSets {
					acceptedNames[a.ListenerSet.Name] = true
				}
				assert.True(t, acceptedNames["ls-same-1"])
				assert.True(t, acceptedNames["ls-same-2"])
				assert.Equal(t, "ls-diff-1", result.RejectedSets[0].ListenerSet.Name)

				// Verify partition completeness (Req 1.6)
				total := len(result.AcceptedSets) + len(result.RejectedSets)
				assert.Equal(t, 3, total)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k8sClient := buildFakeClient(tc.objects...)
			loader := NewListenerSetLoader(k8sClient, testLogger())

			result, err := loader.LoadListenerSetsForGateway(context.Background(), tc.gateway)
			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tc.expectedAccepted, len(result.AcceptedSets), "accepted count mismatch")
			assert.Equal(t, tc.expectedRejected, len(result.RejectedSets), "rejected count mismatch")

			// Verify partition completeness for all tests (Req 1.6)
			totalLS := tc.expectedAccepted + tc.expectedRejected
			assert.Equal(t, totalLS, len(result.AcceptedSets)+len(result.RejectedSets),
				"partition completeness: every LS must be in exactly one of accepted or rejected")

			if tc.checkFunc != nil {
				tc.checkFunc(t, result)
			}
		})
	}
}
