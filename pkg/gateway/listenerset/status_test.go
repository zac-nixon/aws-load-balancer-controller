package listenerset

import (
	"context"
	"fmt"
	"testing"

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

// Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6

func buildStatusFakeClient(objects []client.Object, statusSubresources []client.Object) client.Client {
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	gwv1.AddToScheme(k8sSchema)
	return testclient.NewClientBuilder().
		WithScheme(k8sSchema).
		WithObjects(objects...).
		WithStatusSubresource(statusSubresources...).
		Build()
}

func statusTestLogger() logr.Logger {
	return logr.New(&log.NullLogSink{})
}

func makeStatusGateway(name, namespace string) *gwv1.Gateway {
	return &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gwv1.Listener{
				{Name: "default", Port: 80, Protocol: gwv1.HTTPProtocolType},
			},
		},
	}
}

func makeStatusListenerSet(name, namespace string, entries []gwv1.ListenerEntry) (*gwv1.ListenerSet, ListenerSetData) {
	ls := &gwv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1.ListenerSetSpec{
			ParentRef: gwv1.ParentGatewayReference{
				Name: "test-gw",
			},
		},
	}
	return ls, ListenerSetData{
		ListenerSet: ls,
		Listeners:   entries,
	}
}

func statusFindCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func TestUpdateListenerSetStatuses(t *testing.T) {
	tests := []struct {
		name         string
		acceptedSets []struct {
			name    string
			entries []gwv1.ListenerEntry
		}
		rejectedSets []struct {
			name    string
			reason  string
			message string
		}
		conflictMap  map[ListenerKey]ConflictInfo
		isProgrammed bool
		checkFunc    func(t *testing.T, k8sClient client.Client)
	}{
		{
			name: "single accepted ListenerSet, isProgrammed=true",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{
					name: "ls-1",
					entries: []gwv1.ListenerEntry{
						{Name: "http", Port: 8080, Protocol: gwv1.HTTPProtocolType},
					},
				},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var ls gwv1.ListenerSet
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "ls-1", Namespace: "default"}, &ls)
				assert.NoError(t, err)

				// Top-level Accepted=True (Req 3.1)
				accepted := statusFindCondition(ls.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
				assert.NotNil(t, accepted)
				assert.Equal(t, metav1.ConditionTrue, accepted.Status)

				// Top-level Programmed=True (Req 3.3)
				programmed := statusFindCondition(ls.Status.Conditions, string(gwv1.ListenerSetConditionProgrammed))
				assert.NotNil(t, programmed)
				assert.Equal(t, metav1.ConditionTrue, programmed.Status)

				// Per-listener conditions (Req 3.6)
				assert.Len(t, ls.Status.Listeners, 1)
				entryAccepted := statusFindCondition(ls.Status.Listeners[0].Conditions, string(gwv1.ListenerEntryConditionAccepted))
				assert.NotNil(t, entryAccepted)
				assert.Equal(t, metav1.ConditionTrue, entryAccepted.Status)

				entryConflicted := statusFindCondition(ls.Status.Listeners[0].Conditions, string(gwv1.ListenerEntryConditionConflicted))
				assert.NotNil(t, entryConflicted)
				assert.Equal(t, metav1.ConditionFalse, entryConflicted.Status)

				entryProgrammed := statusFindCondition(ls.Status.Listeners[0].Conditions, string(gwv1.ListenerEntryConditionProgrammed))
				assert.NotNil(t, entryProgrammed)
				assert.Equal(t, metav1.ConditionTrue, entryProgrammed.Status)

				entryResolved := statusFindCondition(ls.Status.Listeners[0].Conditions, string(gwv1.ListenerEntryConditionResolvedRefs))
				assert.NotNil(t, entryResolved)
				assert.Equal(t, metav1.ConditionTrue, entryResolved.Status)
			},
		},
		{
			name: "single accepted ListenerSet, isProgrammed=false",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{
					name: "ls-1",
					entries: []gwv1.ListenerEntry{
						{Name: "http", Port: 8080, Protocol: gwv1.HTTPProtocolType},
					},
				},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: false,
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var ls gwv1.ListenerSet
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "ls-1", Namespace: "default"}, &ls)
				assert.NoError(t, err)

				accepted := statusFindCondition(ls.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
				assert.NotNil(t, accepted)
				assert.Equal(t, metav1.ConditionTrue, accepted.Status)

				programmed := statusFindCondition(ls.Status.Conditions, string(gwv1.ListenerSetConditionProgrammed))
				assert.NotNil(t, programmed)
				assert.Equal(t, metav1.ConditionFalse, programmed.Status)
				assert.Equal(t, string(gwv1.ListenerSetReasonPending), programmed.Reason)

				// Per-listener Programmed=False
				assert.Len(t, ls.Status.Listeners, 1)
				entryProgrammed := statusFindCondition(ls.Status.Listeners[0].Conditions, string(gwv1.ListenerEntryConditionProgrammed))
				assert.NotNil(t, entryProgrammed)
				assert.Equal(t, metav1.ConditionFalse, entryProgrammed.Status)
				assert.Equal(t, string(gwv1.ListenerEntryReasonPending), entryProgrammed.Reason)
			},
		},
		{
			name: "single rejected ListenerSet",
			rejectedSets: []struct {
				name    string
				reason  string
				message string
			}{
				{name: "ls-rejected", reason: "NotAllowed", message: "Gateway does not allow ListenerSets"},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var ls gwv1.ListenerSet
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "ls-rejected", Namespace: "default"}, &ls)
				assert.NoError(t, err)

				// Top-level Accepted=False (Req 3.2)
				accepted := statusFindCondition(ls.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
				assert.NotNil(t, accepted)
				assert.Equal(t, metav1.ConditionFalse, accepted.Status)
				assert.Equal(t, "NotAllowed", accepted.Reason)

				// Top-level Programmed=False with reason Invalid
				programmed := statusFindCondition(ls.Status.Conditions, string(gwv1.ListenerSetConditionProgrammed))
				assert.NotNil(t, programmed)
				assert.Equal(t, metav1.ConditionFalse, programmed.Status)
				assert.Equal(t, string(gwv1.ListenerSetReasonInvalid), programmed.Reason)
			},
		},
		{
			name: "mixed accepted and rejected ListenerSets",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{
					name: "ls-accepted",
					entries: []gwv1.ListenerEntry{
						{Name: "http", Port: 8080, Protocol: gwv1.HTTPProtocolType},
					},
				},
			},
			rejectedSets: []struct {
				name    string
				reason  string
				message string
			}{
				{name: "ls-rejected", reason: "NotAllowed", message: "namespace not allowed"},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var accepted gwv1.ListenerSet
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "ls-accepted", Namespace: "default"}, &accepted)
				assert.NoError(t, err)
				acceptedCond := statusFindCondition(accepted.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
				assert.Equal(t, metav1.ConditionTrue, acceptedCond.Status)

				var rejected gwv1.ListenerSet
				err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "ls-rejected", Namespace: "default"}, &rejected)
				assert.NoError(t, err)
				rejectedCond := statusFindCondition(rejected.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
				assert.Equal(t, metav1.ConditionFalse, rejectedCond.Status)
			},
		},
		{
			name: "accepted with one conflicted and one non-conflicted listener",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{
					name: "ls-mixed",
					entries: []gwv1.ListenerEntry{
						{Name: "conflicted-listener", Port: 80, Protocol: gwv1.HTTPProtocolType},
						{Name: "ok-listener", Port: 8080, Protocol: gwv1.HTTPProtocolType},
					},
				},
			},
			conflictMap: map[ListenerKey]ConflictInfo{
				{Port: 80, Hostname: ""}: {
					Reason:       gwv1.ListenerReasonHostnameConflict,
					Message:      "Conflict on port 80",
					WinnerSource: ListenerSource{Kind: "Gateway", Name: "test-gw"},
				},
			},
			isProgrammed: true,
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var ls gwv1.ListenerSet
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "ls-mixed", Namespace: "default"}, &ls)
				assert.NoError(t, err)
				assert.Len(t, ls.Status.Listeners, 2)

				// Conflicted listener (Req 3.4)
				conflictedStatus := ls.Status.Listeners[0]
				conflictedCond := statusFindCondition(conflictedStatus.Conditions, string(gwv1.ListenerEntryConditionConflicted))
				assert.Equal(t, metav1.ConditionTrue, conflictedCond.Status)
				// Conflicted listener has Programmed=False regardless of isProgrammed
				conflictedProg := statusFindCondition(conflictedStatus.Conditions, string(gwv1.ListenerEntryConditionProgrammed))
				assert.Equal(t, metav1.ConditionFalse, conflictedProg.Status)

				// Non-conflicted listener
				okStatus := ls.Status.Listeners[1]
				okConflicted := statusFindCondition(okStatus.Conditions, string(gwv1.ListenerEntryConditionConflicted))
				assert.Equal(t, metav1.ConditionFalse, okConflicted.Status)
				okProgrammed := statusFindCondition(okStatus.Conditions, string(gwv1.ListenerEntryConditionProgrammed))
				assert.Equal(t, metav1.ConditionTrue, okProgrammed.Status)
			},
		},
		{
			name: "all listeners conflicted",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{
					name: "ls-all-conflict",
					entries: []gwv1.ListenerEntry{
						{Name: "l1", Port: 80, Protocol: gwv1.HTTPProtocolType},
						{Name: "l2", Port: 443, Protocol: gwv1.HTTPSProtocolType},
					},
				},
			},
			conflictMap: map[ListenerKey]ConflictInfo{
				{Port: 80, Hostname: ""}:  {Reason: gwv1.ListenerReasonHostnameConflict, Message: "conflict 80", WinnerSource: ListenerSource{Kind: "Gateway", Name: "gw"}},
				{Port: 443, Hostname: ""}: {Reason: gwv1.ListenerReasonHostnameConflict, Message: "conflict 443", WinnerSource: ListenerSource{Kind: "Gateway", Name: "gw"}},
			},
			isProgrammed: true,
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var ls gwv1.ListenerSet
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "ls-all-conflict", Namespace: "default"}, &ls)
				assert.NoError(t, err)
				assert.Len(t, ls.Status.Listeners, 2)

				for _, listenerStatus := range ls.Status.Listeners {
					conflicted := statusFindCondition(listenerStatus.Conditions, string(gwv1.ListenerEntryConditionConflicted))
					assert.Equal(t, metav1.ConditionTrue, conflicted.Status)
					programmed := statusFindCondition(listenerStatus.Conditions, string(gwv1.ListenerEntryConditionProgrammed))
					assert.Equal(t, metav1.ConditionFalse, programmed.Status)
				}
			},
		},
		{
			name: "Gateway attachedListenerSets count equals len(acceptedSets)",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{name: "ls-a", entries: []gwv1.ListenerEntry{{Name: "l1", Port: 8080, Protocol: gwv1.HTTPProtocolType}}},
				{name: "ls-b", entries: []gwv1.ListenerEntry{{Name: "l2", Port: 8081, Protocol: gwv1.HTTPProtocolType}}},
				{name: "ls-c", entries: []gwv1.ListenerEntry{{Name: "l3", Port: 8082, Protocol: gwv1.HTTPProtocolType}}},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var gw gwv1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-gw", Namespace: "default"}, &gw)
				assert.NoError(t, err)
				// Req 3.5
				assert.NotNil(t, gw.Status.AttachedListenerSets)
				assert.Equal(t, int32(3), *gw.Status.AttachedListenerSets)
			},
		},
		{
			name:         "Gateway attachedListenerSets count with zero accepted",
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			rejectedSets: []struct {
				name    string
				reason  string
				message string
			}{
				{name: "ls-rej", reason: "NotAllowed", message: "not allowed"},
			},
			checkFunc: func(t *testing.T, k8sClient client.Client) {
				var gw gwv1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-gw", Namespace: "default"}, &gw)
				assert.NoError(t, err)
				assert.NotNil(t, gw.Status.AttachedListenerSets)
				assert.Equal(t, int32(0), *gw.Status.AttachedListenerSets)
			},
		},
		{
			name: "idempotency - calling twice yields same result",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{
					name: "ls-idem",
					entries: []gwv1.ListenerEntry{
						{Name: "http", Port: 8080, Protocol: gwv1.HTTPProtocolType},
					},
				},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			// checkFunc is nil; idempotency is verified in the test runner below
		},
		{
			name: "skip-if-unchanged optimization for Gateway attachedListenerSets",
			acceptedSets: []struct {
				name    string
				entries []gwv1.ListenerEntry
			}{
				{name: "ls-skip", entries: []gwv1.ListenerEntry{{Name: "l1", Port: 9090, Protocol: gwv1.HTTPProtocolType}}},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			// checkFunc is nil; skip-if-unchanged is verified in the test runner below
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gw := makeStatusGateway("test-gw", "default")
			var objects []client.Object
			var statusSubresources []client.Object
			var acceptedSets []ListenerSetData
			var rejectedSets []RejectedListenerSet

			objects = append(objects, gw)
			statusSubresources = append(statusSubresources, gw)

			for _, as := range tc.acceptedSets {
				ls, lsData := makeStatusListenerSet(as.name, "default", as.entries)
				objects = append(objects, ls)
				statusSubresources = append(statusSubresources, ls)
				acceptedSets = append(acceptedSets, lsData)
			}

			for _, rs := range tc.rejectedSets {
				ls := &gwv1.ListenerSet{
					ObjectMeta: metav1.ObjectMeta{Name: rs.name, Namespace: "default"},
					Spec: gwv1.ListenerSetSpec{
						ParentRef: gwv1.ParentGatewayReference{Name: "test-gw"},
					},
				}
				objects = append(objects, ls)
				statusSubresources = append(statusSubresources, ls)
				rejectedSets = append(rejectedSets, RejectedListenerSet{
					ListenerSet: ls,
					Reason:      rs.reason,
					Message:     rs.message,
				})
			}

			k8sClient := buildStatusFakeClient(objects, statusSubresources)
			statusMgr := NewListenerSetStatusManager(k8sClient, statusTestLogger())
			ctx := context.Background()

			err := statusMgr.UpdateListenerSetStatuses(ctx, gw, acceptedSets, rejectedSets, tc.conflictMap, tc.isProgrammed)
			assert.NoError(t, err)

			// Special handling for idempotency test: call a second time and verify no error + same result
			if tc.name == "idempotency - calling twice yields same result" {
				err = statusMgr.UpdateListenerSetStatuses(ctx, gw, acceptedSets, rejectedSets, tc.conflictMap, tc.isProgrammed)
				assert.NoError(t, err)

				var ls gwv1.ListenerSet
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "ls-idem", Namespace: "default"}, &ls)
				assert.NoError(t, err)
				accepted := statusFindCondition(ls.Status.Conditions, string(gwv1.ListenerSetConditionAccepted))
				assert.NotNil(t, accepted)
				assert.Equal(t, metav1.ConditionTrue, accepted.Status)

				var gwFetched gwv1.Gateway
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-gw", Namespace: "default"}, &gwFetched)
				assert.NoError(t, err)
				assert.NotNil(t, gwFetched.Status.AttachedListenerSets)
				assert.Equal(t, int32(1), *gwFetched.Status.AttachedListenerSets)
			}

			// Special handling for skip-if-unchanged: pre-set the count, call again, verify no error
			if tc.name == "skip-if-unchanged optimization for Gateway attachedListenerSets" {
				// After the first call, gw.Status.AttachedListenerSets should be set to 1.
				// Re-fetch the gateway to get the updated status.
				var gwFetched gwv1.Gateway
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-gw", Namespace: "default"}, &gwFetched)
				assert.NoError(t, err)
				assert.NotNil(t, gwFetched.Status.AttachedListenerSets)
				assert.Equal(t, int32(1), *gwFetched.Status.AttachedListenerSets)

				// Call again with the same gw (which now has the correct count).
				// The skip-if-unchanged path should be taken for the Gateway patch.
				err = statusMgr.UpdateListenerSetStatuses(ctx, &gwFetched, acceptedSets, rejectedSets, tc.conflictMap, tc.isProgrammed)
				assert.NoError(t, err, "skip-if-unchanged path should not error")
			}

			if tc.checkFunc != nil {
				tc.checkFunc(t, k8sClient)
			}
		})
	}
}

func TestBuildListenerEntryStatuses(t *testing.T) {
	now := metav1.Now()
	generation := int64(1)

	tests := []struct {
		name         string
		listeners    []gwv1.ListenerEntry
		conflictMap  map[ListenerKey]ConflictInfo
		isProgrammed bool
		checkFunc    func(t *testing.T, statuses []gwv1.ListenerEntryStatus)
	}{
		{
			name: "non-conflicted listener with isProgrammed=true has 4 conditions",
			listeners: []gwv1.ListenerEntry{
				{Name: "http", Port: 8080, Protocol: gwv1.HTTPProtocolType},
			},
			conflictMap:  map[ListenerKey]ConflictInfo{},
			isProgrammed: true,
			checkFunc: func(t *testing.T, statuses []gwv1.ListenerEntryStatus) {
				assert.Len(t, statuses, 1)
				assert.Equal(t, gwv1.SectionName("http"), statuses[0].Name)
				assert.Len(t, statuses[0].Conditions, 4)
			},
		},
		{
			name: "conflicted listener has Programmed=False and Conflicted=True",
			listeners: []gwv1.ListenerEntry{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
			},
			conflictMap: map[ListenerKey]ConflictInfo{
				{Port: 80, Hostname: ""}: {
					Reason:  gwv1.ListenerReasonHostnameConflict,
					Message: "conflict",
				},
			},
			isProgrammed: true,
			checkFunc: func(t *testing.T, statuses []gwv1.ListenerEntryStatus) {
				assert.Len(t, statuses, 1)
				conflicted := statusFindCondition(statuses[0].Conditions, string(gwv1.ListenerEntryConditionConflicted))
				assert.Equal(t, metav1.ConditionTrue, conflicted.Status)
				assert.Equal(t, string(gwv1.ListenerReasonHostnameConflict), conflicted.Reason)

				programmed := statusFindCondition(statuses[0].Conditions, string(gwv1.ListenerEntryConditionProgrammed))
				assert.Equal(t, metav1.ConditionFalse, programmed.Status)
			},
		},
		{
			name: "listener with hostname uses correct conflict key",
			listeners: []gwv1.ListenerEntry{
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType, Hostname: hostnamePtr("example.com")},
			},
			conflictMap: map[ListenerKey]ConflictInfo{
				{Port: 443, Hostname: "example.com"}: {
					Reason:  gwv1.ListenerReasonHostnameConflict,
					Message: "hostname conflict",
				},
			},
			isProgrammed: true,
			checkFunc: func(t *testing.T, statuses []gwv1.ListenerEntryStatus) {
				assert.Len(t, statuses, 1)
				conflicted := statusFindCondition(statuses[0].Conditions, string(gwv1.ListenerEntryConditionConflicted))
				assert.Equal(t, metav1.ConditionTrue, conflicted.Status)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			statuses := buildListenerEntryStatuses(tc.listeners, tc.conflictMap, tc.isProgrammed, generation, now)
			tc.checkFunc(t, statuses)
		})
	}
}

func TestSetListenerSetCondition(t *testing.T) {
	t.Run("adds new condition to empty slice", func(t *testing.T) {
		var conditions []metav1.Condition
		setListenerSetCondition(&conditions, metav1.Condition{
			Type:   "TestType",
			Status: metav1.ConditionTrue,
			Reason: "TestReason",
		})
		assert.Len(t, conditions, 1)
		assert.Equal(t, "TestType", conditions[0].Type)
	})

	t.Run("updates existing condition of same type", func(t *testing.T) {
		conditions := []metav1.Condition{
			{Type: "TestType", Status: metav1.ConditionFalse, Reason: "OldReason"},
		}
		setListenerSetCondition(&conditions, metav1.Condition{
			Type:   "TestType",
			Status: metav1.ConditionTrue,
			Reason: "NewReason",
		})
		assert.Len(t, conditions, 1)
		assert.Equal(t, metav1.ConditionTrue, conditions[0].Status)
		assert.Equal(t, "NewReason", conditions[0].Reason)
	})

	t.Run("nil conditions pointer is a no-op", func(t *testing.T) {
		// Should not panic
		setListenerSetCondition(nil, metav1.Condition{
			Type:   "TestType",
			Status: metav1.ConditionTrue,
		})
	})
}

func TestHostnameStr(t *testing.T) {
	t.Run("nil hostname returns empty string", func(t *testing.T) {
		assert.Equal(t, "", hostnameStr(nil))
	})

	t.Run("non-nil hostname returns string value", func(t *testing.T) {
		h := gwv1.Hostname("example.com")
		assert.Equal(t, "example.com", hostnameStr(&h))
	})
}

func TestUpdateListenerSetStatuses_EmptyInputs(t *testing.T) {
	gw := makeStatusGateway("test-gw", "default")
	k8sClient := buildStatusFakeClient(
		[]client.Object{gw},
		[]client.Object{gw},
	)
	statusMgr := NewListenerSetStatusManager(k8sClient, statusTestLogger())

	err := statusMgr.UpdateListenerSetStatuses(
		context.Background(), gw, nil, nil, map[ListenerKey]ConflictInfo{}, true,
	)
	assert.NoError(t, err)

	// Gateway should get attachedListenerSets = 0
	var gwFetched gwv1.Gateway
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-gw", Namespace: "default"}, &gwFetched)
	assert.NoError(t, err)
	assert.NotNil(t, gwFetched.Status.AttachedListenerSets)
	assert.Equal(t, int32(0), *gwFetched.Status.AttachedListenerSets)
}

// Suppress unused import warning for fmt
var _ = fmt.Sprintf
