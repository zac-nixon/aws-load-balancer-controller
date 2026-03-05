package listenerset

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.6**
//
// Property 5: Status Consistency
// For any ListenerSet processed during reconciliation:
//   - If it is in AcceptedSets, its top-level Accepted condition is True and each
//     of its listeners has per-listener conditions set (including Conflicted=True
//     for listeners in the ConflictMap).
//   - If it is in RejectedSets, its top-level Accepted condition is False.
//   - When isProgrammed is true, all accepted ListenerSets have Programmed=True.
//   - Conflicted listeners have Programmed=False regardless of isProgrammed.

// statusInput is the randomly generated input for the status consistency property test.
type statusInput struct {
	// AcceptedSets defines 0-3 accepted ListenerSets, each with 1-3 listeners.
	AcceptedSets []statusTestListenerSet
	// RejectedSets defines 0-3 rejected ListenerSets.
	RejectedSets []statusTestRejectedSet
	// ConflictedKeys defines which listener keys (from accepted sets) are conflicted.
	ConflictedKeys []ListenerKey
	// IsProgrammed controls the isProgrammed flag passed to UpdateListenerSetStatuses.
	IsProgrammed bool
}

// statusTestListenerSet is a simplified ListenerSet for status test generation.
type statusTestListenerSet struct {
	Name      string
	Namespace string
	Listeners []statusTestListener
}

// statusTestListener is a simplified listener entry for status test generation.
type statusTestListener struct {
	Name     string
	Port     int32
	Hostname string
}

// statusTestRejectedSet is a simplified rejected ListenerSet for status test generation.
type statusTestRejectedSet struct {
	Name      string
	Namespace string
	Reason    string
	Message   string
}

var statusPorts = []int32{80, 443, 8080, 8443, 9090}
var statusHostnames = []string{"", "example.com", "api.example.com", "test.example.com"}
var statusReasons = []string{"NotAllowed", "InvalidParentRef", "NamespaceNotAllowed"}

// Generate implements quick.Generator for statusInput.
func (statusInput) Generate(r *rand.Rand, size int) reflect.Value {
	input := statusInput{
		IsProgrammed: r.Intn(2) == 1,
	}

	// Track used port+hostname combinations to avoid duplicates within a single set.
	usedKeys := make(map[ListenerKey]bool)

	// Generate 0-3 accepted ListenerSets.
	numAccepted := r.Intn(4)
	for i := 0; i < numAccepted; i++ {
		ls := statusTestListenerSet{
			Name:      fmt.Sprintf("accepted-ls-%d", i),
			Namespace: "default",
		}
		numListeners := r.Intn(3) + 1
		for j := 0; j < numListeners; j++ {
			port := statusPorts[r.Intn(len(statusPorts))]
			hostname := statusHostnames[r.Intn(len(statusHostnames))]
			key := ListenerKey{Port: port, Hostname: hostname}
			// Ensure unique port+hostname across all accepted listeners for clean conflict tracking.
			for usedKeys[key] {
				port = statusPorts[r.Intn(len(statusPorts))]
				hostname = statusHostnames[r.Intn(len(statusHostnames))]
				key = ListenerKey{Port: port, Hostname: hostname}
			}
			usedKeys[key] = true
			ls.Listeners = append(ls.Listeners, statusTestListener{
				Name:     fmt.Sprintf("listener-%d-%d", i, j),
				Port:     port,
				Hostname: hostname,
			})
		}
		input.AcceptedSets = append(input.AcceptedSets, ls)
	}

	// Randomly mark some accepted listener keys as conflicted.
	var allKeys []ListenerKey
	for _, ls := range input.AcceptedSets {
		for _, l := range ls.Listeners {
			allKeys = append(allKeys, ListenerKey{Port: l.Port, Hostname: l.Hostname})
		}
	}
	for _, key := range allKeys {
		if r.Intn(3) == 0 { // ~33% chance of conflict
			input.ConflictedKeys = append(input.ConflictedKeys, key)
		}
	}

	// Generate 0-3 rejected ListenerSets.
	numRejected := r.Intn(4)
	for i := 0; i < numRejected; i++ {
		input.RejectedSets = append(input.RejectedSets, statusTestRejectedSet{
			Name:      fmt.Sprintf("rejected-ls-%d", i),
			Namespace: "default",
			Reason:    statusReasons[r.Intn(len(statusReasons))],
			Message:   fmt.Sprintf("Rejected reason %d", i),
		})
	}

	return reflect.ValueOf(input)
}

// buildStatusScenario constructs the Gateway, accepted/rejected ListenerSets,
// conflict map, and fake k8s client from a statusInput.
func buildStatusScenario(input statusInput) (
	*gwv1.Gateway,
	[]ListenerSetData,
	[]RejectedListenerSet,
	map[ListenerKey]ConflictInfo,
	client.Client,
) {
	gw := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gwv1.Listener{
				{Name: "default", Port: 80, Protocol: gwv1.HTTPProtocolType},
			},
		},
	}

	var acceptedSets []ListenerSetData
	var rejectedSets []RejectedListenerSet
	var statusSubresources []client.Object
	var objects []client.Object

	objects = append(objects, gw)
	statusSubresources = append(statusSubresources, gw)

	// Build accepted ListenerSets.
	for _, als := range input.AcceptedSets {
		ls := &gwv1.ListenerSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      als.Name,
				Namespace: als.Namespace,
			},
			Spec: gwv1.ListenerSetSpec{
				ParentRef: gwv1.ParentGatewayReference{
					Name: gwv1.ObjectName(gw.Name),
				},
			},
		}
		var entries []gwv1.ListenerEntry
		for _, tl := range als.Listeners {
			entry := gwv1.ListenerEntry{
				Name:     gwv1.SectionName(tl.Name),
				Port:     gwv1.PortNumber(tl.Port),
				Protocol: gwv1.HTTPProtocolType,
			}
			if tl.Hostname != "" {
				h := gwv1.Hostname(tl.Hostname)
				entry.Hostname = &h
			}
			entries = append(entries, entry)
		}
		objects = append(objects, ls)
		statusSubresources = append(statusSubresources, ls)
		acceptedSets = append(acceptedSets, ListenerSetData{
			ListenerSet: ls,
			Listeners:   entries,
		})
	}

	// Build rejected ListenerSets.
	for _, rls := range input.RejectedSets {
		ls := &gwv1.ListenerSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rls.Name,
				Namespace: rls.Namespace,
			},
			Spec: gwv1.ListenerSetSpec{
				ParentRef: gwv1.ParentGatewayReference{
					Name: gwv1.ObjectName(gw.Name),
				},
			},
		}
		objects = append(objects, ls)
		statusSubresources = append(statusSubresources, ls)
		rejectedSets = append(rejectedSets, RejectedListenerSet{
			ListenerSet: ls,
			Reason:      rls.Reason,
			Message:     rls.Message,
		})
	}

	// Build conflict map.
	conflictMap := make(map[ListenerKey]ConflictInfo)
	for _, key := range input.ConflictedKeys {
		conflictMap[key] = ConflictInfo{
			Reason:  gwv1.ListenerReasonHostnameConflict,
			Message: fmt.Sprintf("Conflict on port %d hostname %q", key.Port, key.Hostname),
			WinnerSource: ListenerSource{
				Kind: "Gateway",
				Name: gw.Name,
			},
		}
	}

	// Build the fake k8s client.
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	gwv1.AddToScheme(k8sSchema)

	k8sClient := testclient.NewClientBuilder().
		WithScheme(k8sSchema).
		WithObjects(objects...).
		WithStatusSubresource(statusSubresources...).
		Build()

	return gw, acceptedSets, rejectedSets, conflictMap, k8sClient
}

// findCondition finds a condition by type in a slice of conditions.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func TestProperty_StatusConsistency(t *testing.T) {
	property := func(input statusInput) bool {
		gw, acceptedSets, rejectedSets, conflictMap, k8sClient := buildStatusScenario(input)
		logger := logr.New(&log.NullLogSink{})
		statusMgr := NewListenerSetStatusManager(k8sClient, logger)

		ctx := context.Background()
		err := statusMgr.UpdateListenerSetStatuses(ctx, gw, acceptedSets, rejectedSets, conflictMap, input.IsProgrammed)
		if err != nil {
			t.Logf("unexpected error: %v", err)
			return false
		}

		// Re-fetch each accepted ListenerSet and verify conditions.
		for _, lsData := range acceptedSets {
			var fetched gwv1.ListenerSet
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      lsData.ListenerSet.Name,
				Namespace: lsData.ListenerSet.Namespace,
			}, &fetched)
			if err != nil {
				t.Logf("failed to fetch accepted ListenerSet %s: %v", lsData.ListenerSet.Name, err)
				return false
			}

			// Verification 1: Top-level Accepted=True (Requirement 3.1).
			acceptedCond := findCondition(fetched.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
			if acceptedCond == nil {
				t.Logf("accepted ListenerSet %s missing Accepted condition", lsData.ListenerSet.Name)
				return false
			}
			if acceptedCond.Status != metav1.ConditionTrue {
				t.Logf("accepted ListenerSet %s has Accepted=%s, expected True",
					lsData.ListenerSet.Name, acceptedCond.Status)
				return false
			}

			// Verification 2: Top-level Programmed matches isProgrammed (Requirement 3.3).
			programmedCond := findCondition(fetched.Status.Conditions, string(gwv1.ListenerSetConditionProgrammed))
			if programmedCond == nil {
				t.Logf("accepted ListenerSet %s missing Programmed condition", lsData.ListenerSet.Name)
				return false
			}
			if input.IsProgrammed && programmedCond.Status != metav1.ConditionTrue {
				t.Logf("accepted ListenerSet %s has Programmed=%s when isProgrammed=true",
					lsData.ListenerSet.Name, programmedCond.Status)
				return false
			}
			if !input.IsProgrammed && programmedCond.Status != metav1.ConditionFalse {
				t.Logf("accepted ListenerSet %s has Programmed=%s when isProgrammed=false",
					lsData.ListenerSet.Name, programmedCond.Status)
				return false
			}

			// Verification 3: Per-listener conditions (Requirement 3.4, 3.6).
			if len(fetched.Status.Listeners) != len(lsData.Listeners) {
				t.Logf("accepted ListenerSet %s has %d listener statuses, expected %d",
					lsData.ListenerSet.Name, len(fetched.Status.Listeners), len(lsData.Listeners))
				return false
			}

			for idx, entry := range lsData.Listeners {
				listenerStatus := fetched.Status.Listeners[idx]

				// Per-listener Accepted=True always for accepted sets.
				entryAccepted := findCondition(listenerStatus.Conditions, string(gwv1.ListenerEntryConditionAccepted))
				if entryAccepted == nil || entryAccepted.Status != metav1.ConditionTrue {
					t.Logf("accepted ListenerSet %s listener %s missing or wrong Accepted condition",
						lsData.ListenerSet.Name, entry.Name)
					return false
				}

				// Check if this listener is conflicted.
				lKey := ListenerKey{
					Port:     int32(entry.Port),
					Hostname: hostnameStr(entry.Hostname),
				}
				_, isConflicted := conflictMap[lKey]

				// Per-listener Conflicted condition matches ConflictMap (Requirement 3.4).
				conflictedCond := findCondition(listenerStatus.Conditions, string(gwv1.ListenerEntryConditionConflicted))
				if conflictedCond == nil {
					t.Logf("accepted ListenerSet %s listener %s missing Conflicted condition",
						lsData.ListenerSet.Name, entry.Name)
					return false
				}
				if isConflicted && conflictedCond.Status != metav1.ConditionTrue {
					t.Logf("accepted ListenerSet %s listener %s should be Conflicted=True but got %s",
						lsData.ListenerSet.Name, entry.Name, conflictedCond.Status)
					return false
				}
				if !isConflicted && conflictedCond.Status != metav1.ConditionFalse {
					t.Logf("accepted ListenerSet %s listener %s should be Conflicted=False but got %s",
						lsData.ListenerSet.Name, entry.Name, conflictedCond.Status)
					return false
				}

				// Per-listener Programmed: False when conflicted, else matches isProgrammed.
				entryProgrammed := findCondition(listenerStatus.Conditions, string(gwv1.ListenerEntryConditionProgrammed))
				if entryProgrammed == nil {
					t.Logf("accepted ListenerSet %s listener %s missing Programmed condition",
						lsData.ListenerSet.Name, entry.Name)
					return false
				}
				if isConflicted {
					// Conflicted listeners always have Programmed=False.
					if entryProgrammed.Status != metav1.ConditionFalse {
						t.Logf("conflicted listener %s in %s has Programmed=%s, expected False",
							entry.Name, lsData.ListenerSet.Name, entryProgrammed.Status)
						return false
					}
				} else if input.IsProgrammed {
					if entryProgrammed.Status != metav1.ConditionTrue {
						t.Logf("non-conflicted listener %s in %s has Programmed=%s when isProgrammed=true",
							entry.Name, lsData.ListenerSet.Name, entryProgrammed.Status)
						return false
					}
				} else {
					if entryProgrammed.Status != metav1.ConditionFalse {
						t.Logf("non-conflicted listener %s in %s has Programmed=%s when isProgrammed=false",
							entry.Name, lsData.ListenerSet.Name, entryProgrammed.Status)
						return false
					}
				}

				// Per-listener ResolvedRefs=True always.
				resolvedRefs := findCondition(listenerStatus.Conditions, string(gwv1.ListenerEntryConditionResolvedRefs))
				if resolvedRefs == nil || resolvedRefs.Status != metav1.ConditionTrue {
					t.Logf("accepted ListenerSet %s listener %s missing or wrong ResolvedRefs condition",
						lsData.ListenerSet.Name, entry.Name)
					return false
				}
			}
		}

		// Re-fetch each rejected ListenerSet and verify conditions.
		for _, rejected := range rejectedSets {
			var fetched gwv1.ListenerSet
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      rejected.ListenerSet.Name,
				Namespace: rejected.ListenerSet.Namespace,
			}, &fetched)
			if err != nil {
				t.Logf("failed to fetch rejected ListenerSet %s: %v", rejected.ListenerSet.Name, err)
				return false
			}

			// Verification 4: Top-level Accepted=False (Requirement 3.2).
			acceptedCond := findCondition(fetched.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
			if acceptedCond == nil {
				t.Logf("rejected ListenerSet %s missing Accepted condition", rejected.ListenerSet.Name)
				return false
			}
			if acceptedCond.Status != metav1.ConditionFalse {
				t.Logf("rejected ListenerSet %s has Accepted=%s, expected False",
					rejected.ListenerSet.Name, acceptedCond.Status)
				return false
			}

			// Verification 5: Top-level Programmed=False for rejected sets.
			programmedCond := findCondition(fetched.Status.Conditions, string(gwv1.ListenerSetConditionProgrammed))
			if programmedCond == nil {
				t.Logf("rejected ListenerSet %s missing Programmed condition", rejected.ListenerSet.Name)
				return false
			}
			if programmedCond.Status != metav1.ConditionFalse {
				t.Logf("rejected ListenerSet %s has Programmed=%s, expected False",
					rejected.ListenerSet.Name, programmedCond.Status)
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{
		MaxCount: 200,
	}
	if err := quick.Check(property, cfg); err != nil {
		ce, ok := err.(*quick.CheckError)
		if ok {
			assert.Fail(t, fmt.Sprintf("Property 5 (Status Consistency) failed after %d tests: %v\nCounterexample: %+v",
				ce.Count, ce, ce.In))
		} else {
			assert.Fail(t, fmt.Sprintf("Property 5 (Status Consistency) failed: %v", err))
		}
	}
}

// **Validates: Requirement 3.5**
//
// Property 6: Attached ListenerSets Count
// For any Gateway with ListenerSets, the Gateway status attachedListenerSets
// count equals len(acceptedSets) after UpdateListenerSetStatuses is called.
func TestProperty_AttachedListenerSetsCount(t *testing.T) {
	property := func(input statusInput) bool {
		gw, acceptedSets, rejectedSets, conflictMap, k8sClient := buildStatusScenario(input)
		logger := logr.New(&log.NullLogSink{})
		statusMgr := NewListenerSetStatusManager(k8sClient, logger)

		ctx := context.Background()
		err := statusMgr.UpdateListenerSetStatuses(ctx, gw, acceptedSets, rejectedSets, conflictMap, input.IsProgrammed)
		if err != nil {
			t.Logf("unexpected error: %v", err)
			return false
		}

		// Re-fetch the Gateway from the fake client.
		var fetched gwv1.Gateway
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		}, &fetched)
		if err != nil {
			t.Logf("failed to fetch Gateway: %v", err)
			return false
		}

		// Verify AttachedListenerSets is not nil.
		if fetched.Status.AttachedListenerSets == nil {
			t.Logf("Gateway AttachedListenerSets is nil, expected %d", len(acceptedSets))
			return false
		}

		// Verify AttachedListenerSets equals len(acceptedSets).
		expected := int32(len(acceptedSets))
		actual := *fetched.Status.AttachedListenerSets
		if actual != expected {
			t.Logf("Gateway AttachedListenerSets = %d, expected %d", actual, expected)
			return false
		}

		return true
	}

	cfg := &quick.Config{
		MaxCount: 200,
	}
	if err := quick.Check(property, cfg); err != nil {
		ce, ok := err.(*quick.CheckError)
		if ok {
			assert.Fail(t, fmt.Sprintf("Property 6 (Attached ListenerSets Count) failed after %d tests: %v\nCounterexample: %+v",
				ce.Count, ce, ce.In))
		} else {
			assert.Fail(t, fmt.Sprintf("Property 6 (Attached ListenerSets Count) failed: %v", err))
		}
	}
}
