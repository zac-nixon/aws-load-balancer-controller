package listenerset

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// **Validates: Requirements 1.2, 1.3, 1.4, 1.5**
//
// Property 1: Handshake Correctness
// For any Gateway with an allowedListeners policy and for any ListenerSet
// referencing that Gateway, the ListenerSet is in AcceptedSets if and only if
// its namespace satisfies the Gateway's allowedListeners policy:
//   - None/absent → all rejected
//   - Same → only same-namespace accepted
//   - All → all accepted
//   - Selector → only matching namespaces accepted

// policyKind represents the four allowedListeners namespace policies.
type policyKind int

const (
	policyNone     policyKind = 0
	policySame     policyKind = 1
	policyAll      policyKind = 2
	policySelector policyKind = 3
)

// handshakeInput is the randomly generated input for the property test.
type handshakeInput struct {
	Policy            policyKind
	GatewayNamespace  string
	LSNamespaces      []string
	LabeledNamespaces map[string]bool
}

// Generate implements quick.Generator for handshakeInput.
func (handshakeInput) Generate(rand *rand.Rand, size int) reflect.Value {
	namespaces := []string{"default", "team-a", "team-b", "team-c", "infra", "monitoring"}

	input := handshakeInput{
		Policy:            policyKind(rand.Intn(4)),
		GatewayNamespace:  namespaces[rand.Intn(len(namespaces))],
		LabeledNamespaces: make(map[string]bool),
	}

	numLS := rand.Intn(5) + 1
	input.LSNamespaces = make([]string, numLS)
	for i := 0; i < numLS; i++ {
		input.LSNamespaces[i] = namespaces[rand.Intn(len(namespaces))]
	}

	if input.Policy == policySelector {
		for _, ns := range namespaces {
			input.LabeledNamespaces[ns] = rand.Intn(2) == 1
		}
	}

	return reflect.ValueOf(input)
}

const selectorLabelKey = "gateway-access"
const selectorLabelValue = "allowed"

// buildTestScenario constructs the Gateway, ListenerSets, Namespaces, and
// fake k8s client from a handshakeInput.
func buildTestScenario(input handshakeInput) (gwv1.Gateway, []client.Object, client.Client) {
	gw := gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: input.GatewayNamespace,
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gwv1.Listener{
				{Name: "default", Port: 80, Protocol: gwv1.HTTPProtocolType},
			},
		},
	}

	// Set allowedListeners based on policy.
	switch input.Policy {
	case policyNone:
		// No allowedListeners field → rejects all.
		gw.Spec.AllowedListeners = nil
	case policySame:
		from := gwv1.NamespacesFromSame
		gw.Spec.AllowedListeners = &gwv1.AllowedListeners{
			Namespaces: &gwv1.ListenerNamespaces{
				From: &from,
			},
		}
	case policyAll:
		from := gwv1.NamespacesFromAll
		gw.Spec.AllowedListeners = &gwv1.AllowedListeners{
			Namespaces: &gwv1.ListenerNamespaces{
				From: &from,
			},
		}
	case policySelector:
		from := gwv1.NamespacesFromSelector
		gw.Spec.AllowedListeners = &gwv1.AllowedListeners{
			Namespaces: &gwv1.ListenerNamespaces{
				From: &from,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						selectorLabelKey: selectorLabelValue,
					},
				},
			},
		}
	}

	var objects []client.Object

	// Create Namespace objects (needed for Selector policy label matching).
	createdNS := make(map[string]bool)
	allNamespaces := append([]string{input.GatewayNamespace}, input.LSNamespaces...)
	for _, ns := range allNamespaces {
		if createdNS[ns] {
			continue
		}
		createdNS[ns] = true
		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}
		if input.Policy == policySelector && input.LabeledNamespaces[ns] {
			nsObj.Labels = map[string]string{
				selectorLabelKey: selectorLabelValue,
			}
		}
		objects = append(objects, nsObj)
	}

	// Create ListenerSets referencing the Gateway.
	for i, lsNS := range input.LSNamespaces {
		ls := &gwv1.ListenerSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ls-%d", i),
				Namespace: lsNS,
			},
			Spec: gwv1.ListenerSetSpec{
				ParentRef: gwv1.ParentGatewayReference{
					Name:      gwv1.ObjectName(gw.Name),
					Namespace: nsPtr(gw.Namespace),
				},
				Listeners: []gwv1.ListenerEntry{
					{
						Name:     gwv1.SectionName(fmt.Sprintf("listener-%d", i)),
						Port:     gwv1.PortNumber(8080 + i),
						Protocol: gwv1.HTTPProtocolType,
					},
				},
			},
		}
		objects = append(objects, ls)
	}

	// Build the fake k8s client with the scheme and objects.
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	gwv1.AddToScheme(k8sSchema)

	k8sClient := testclient.NewClientBuilder().
		WithScheme(k8sSchema).
		WithObjects(objects...).
		Build()

	return gw, objects, k8sClient
}

func nsPtr(ns string) *gwv1.Namespace {
	n := gwv1.Namespace(ns)
	return &n
}

// expectedAccepted returns whether a ListenerSet in the given namespace should
// be accepted by the Gateway's policy.
func expectedAccepted(input handshakeInput, lsNamespace string) bool {
	switch input.Policy {
	case policyNone:
		return false
	case policySame:
		return lsNamespace == input.GatewayNamespace
	case policyAll:
		return true
	case policySelector:
		return input.LabeledNamespaces[lsNamespace]
	default:
		return false
	}
}

func TestProperty_HandshakeCorrectness(t *testing.T) {
	property := func(input handshakeInput) bool {
		gw, _, k8sClient := buildTestScenario(input)
		logger := logr.New(&log.NullLogSink{})
		loader := NewListenerSetLoader(k8sClient, logger)

		result, err := loader.LoadListenerSetsForGateway(context.Background(), gw)
		if err != nil {
			t.Logf("unexpected error: %v", err)
			return false
		}

		// Build maps of accepted/rejected ListenerSet names for easy lookup.
		acceptedNames := make(map[string]bool)
		for _, a := range result.AcceptedSets {
			acceptedNames[a.ListenerSet.Name] = true
		}
		rejectedNames := make(map[string]bool)
		for _, r := range result.RejectedSets {
			rejectedNames[r.ListenerSet.Name] = true
		}

		// Verify each ListenerSet's classification matches the expected policy.
		for i, lsNS := range input.LSNamespaces {
			lsName := fmt.Sprintf("ls-%d", i)
			shouldAccept := expectedAccepted(input, lsNS)

			if shouldAccept && !acceptedNames[lsName] {
				t.Logf("expected ls %s (ns=%s) to be accepted (policy=%d, gwNS=%s), but it was not",
					lsName, lsNS, input.Policy, input.GatewayNamespace)
				return false
			}
			if !shouldAccept && !rejectedNames[lsName] {
				t.Logf("expected ls %s (ns=%s) to be rejected (policy=%d, gwNS=%s), but it was not",
					lsName, lsNS, input.Policy, input.GatewayNamespace)
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{
		MaxCount: 200,
	}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 1 (Handshake Correctness) failed: %v", err)
	}
}

// **Validates: Requirements 1.6**
//
// Property 2: Partition Completeness
// For any Gateway and set of ListenerSets, every ListenerSet appears in
// exactly one of AcceptedSets or RejectedSets. That is:
//   - len(AcceptedSets) + len(RejectedSets) == total ListenerSets
//   - No ListenerSet name appears in both sets

func TestProperty_PartitionCompleteness(t *testing.T) {
	property := func(input handshakeInput) bool {
		gw, _, k8sClient := buildTestScenario(input)
		logger := logr.New(&log.NullLogSink{})
		loader := NewListenerSetLoader(k8sClient, logger)

		result, err := loader.LoadListenerSetsForGateway(context.Background(), gw)
		if err != nil {
			t.Logf("unexpected error: %v", err)
			return false
		}

		totalInput := len(input.LSNamespaces)
		totalOutput := len(result.AcceptedSets) + len(result.RejectedSets)

		// Check that every input ListenerSet is accounted for.
		if totalOutput != totalInput {
			t.Logf("partition size mismatch: input=%d, accepted=%d + rejected=%d = %d",
				totalInput, len(result.AcceptedSets), len(result.RejectedSets), totalOutput)
			return false
		}

		// Check that no ListenerSet appears in both sets.
		acceptedNames := make(map[string]bool, len(result.AcceptedSets))
		for _, a := range result.AcceptedSets {
			acceptedNames[a.ListenerSet.Name] = true
		}
		for _, r := range result.RejectedSets {
			if acceptedNames[r.ListenerSet.Name] {
				t.Logf("ListenerSet %q appears in both AcceptedSets and RejectedSets", r.ListenerSet.Name)
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{
		MaxCount: 200,
	}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 2 (Partition Completeness) failed: %v", err)
	}
}
