package routeutils

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/listenerset"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/shared_constants"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ============================================================================
// Shared test helpers and generators
// ============================================================================

var (
	routeTestPorts      = []int32{80, 443, 8080, 8443, 9090}
	routeTestProtocols  = []gwv1.ProtocolType{gwv1.HTTPProtocolType, gwv1.HTTPSProtocolType}
	routeTestHostnames  = []string{"", "example.com", "api.example.com", "test.io"}
	routeTestNamespaces = []string{"default", "team-a", "team-b", "team-c"}
	routeTestLSNames    = []string{"ls-alpha", "ls-beta", "ls-gamma", "ls-delta"}
	routeTestBaseTime   = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
)

func ptrHostname(h string) *gwv1.Hostname {
	if h == "" {
		return nil
	}
	hostname := gwv1.Hostname(h)
	return &hostname
}

func ptrSectionName(s string) *gwv1.SectionName {
	sn := gwv1.SectionName(s)
	return &sn
}

func ptrKind(k string) *gwv1.Kind {
	kind := gwv1.Kind(k)
	return &kind
}

func ptrNamespace(ns string) *gwv1.Namespace {
	n := gwv1.Namespace(ns)
	return &n
}

func ptrGroup(g string) *gwv1.Group {
	group := gwv1.Group(g)
	return &group
}

// buildTestListenerSetData creates a ListenerSetData with the given parameters.
func buildTestListenerSetData(name, namespace string, timestamp time.Time, entries []gwv1.ListenerEntry) listenerset.ListenerSetData {
	return listenerset.ListenerSetData{
		ListenerSet: &gwv1.ListenerSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				CreationTimestamp: metav1.NewTime(timestamp),
			},
		},
		Listeners: entries,
	}
}

// buildMergedListener creates a MergedListener from a ListenerEntry and source info.
func buildMergedListener(entry gwv1.ListenerEntry, sourceKind, sourceName, sourceNS string) listenerset.MergedListener {
	return listenerset.MergedListener{
		Listener: gwv1.Listener{
			Name:     entry.Name,
			Port:     entry.Port,
			Protocol: entry.Protocol,
			Hostname: entry.Hostname,
		},
		Source: listenerset.ListenerSource{
			Kind:      sourceKind,
			Name:      sourceName,
			Namespace: sourceNS,
		},
	}
}

// ============================================================================
// Property 7: Route Scope Isolation
// **Validates: Requirements 5.3, 5.4**
//
// For any route with a parentRef of kind ListenerSet referencing a specific
// ListenerSet, the route is matched only against listeners belonging to that
// ListenerSet, never against the parent Gateway's listeners or other
// ListenerSets' listeners. When a sectionName is specified, only the named
// listener within that ListenerSet is considered.
// ============================================================================

type scopeIsolationInput struct {
	// TargetLSIndex is the index of the ListenerSet the route targets (0 or 1).
	TargetLSIndex int
	// UseSectionName controls whether the parentRef has a sectionName.
	UseSectionName bool
	// SectionNameIndex is the index of the listener within the target LS to use as sectionName.
	SectionNameIndex int
	// ListenerSets defines 2 ListenerSets, each with 1-3 listeners.
	ListenerSets [2]testLSForScope
	// GatewayListeners defines 1-2 Gateway listeners.
	GatewayListeners []testListenerForScope
}

type testLSForScope struct {
	Name      string
	Namespace string
	Listeners []testListenerForScope
}

type testListenerForScope struct {
	Name     string
	Port     int32
	Protocol gwv1.ProtocolType
	Hostname string
}

func (scopeIsolationInput) Generate(r *rand.Rand, size int) reflect.Value {
	input := scopeIsolationInput{
		TargetLSIndex:  r.Intn(2),
		UseSectionName: r.Intn(2) == 1,
	}

	// Generate 2 ListenerSets with unique ports to avoid conflicts.
	portPool := []int32{8080, 8081, 8082, 9090, 9091, 9092}
	r.Shuffle(len(portPool), func(i, j int) { portPool[i], portPool[j] = portPool[j], portPool[i] })
	portIdx := 0

	for i := 0; i < 2; i++ {
		ls := testLSForScope{
			Name:      routeTestLSNames[i],
			Namespace: routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		}
		numListeners := r.Intn(3) + 1
		for j := 0; j < numListeners; j++ {
			ls.Listeners = append(ls.Listeners, testListenerForScope{
				Name:     fmt.Sprintf("listener-%d-%d", i, j),
				Port:     portPool[portIdx%len(portPool)],
				Protocol: routeTestProtocols[r.Intn(len(routeTestProtocols))],
				Hostname: routeTestHostnames[r.Intn(len(routeTestHostnames))],
			})
			portIdx++
		}
		input.ListenerSets[i] = ls
	}

	input.SectionNameIndex = r.Intn(len(input.ListenerSets[input.TargetLSIndex].Listeners))

	// Generate 1-2 Gateway listeners on different ports.
	numGW := r.Intn(2) + 1
	for i := 0; i < numGW; i++ {
		input.GatewayListeners = append(input.GatewayListeners, testListenerForScope{
			Name:     fmt.Sprintf("gw-listener-%d", i),
			Port:     portPool[portIdx%len(portPool)],
			Protocol: routeTestProtocols[r.Intn(len(routeTestProtocols))],
			Hostname: routeTestHostnames[r.Intn(len(routeTestHostnames))],
		})
		portIdx++
	}

	return reflect.ValueOf(input)
}

func TestProperty_RouteScopeIsolation(t *testing.T) {
	property := func(input scopeIsolationInput) bool {
		// Build ListenerSetData and MergedListeners for both ListenerSets + Gateway.
		var allAccepted []listenerset.ListenerSetData
		var allMerged []listenerset.MergedListener

		// Add Gateway listeners to merged list.
		for _, gl := range input.GatewayListeners {
			allMerged = append(allMerged, listenerset.MergedListener{
				Listener: gwv1.Listener{
					Name:     gwv1.SectionName(gl.Name),
					Port:     gwv1.PortNumber(gl.Port),
					Protocol: gl.Protocol,
					Hostname: ptrHostname(gl.Hostname),
				},
				Source: listenerset.ListenerSource{
					Kind:      "Gateway",
					Name:      "test-gw",
					Namespace: "default",
				},
			})
		}

		// Add ListenerSet listeners.
		for i := 0; i < 2; i++ {
			ls := input.ListenerSets[i]
			var entries []gwv1.ListenerEntry
			for _, tl := range ls.Listeners {
				entry := gwv1.ListenerEntry{
					Name:     gwv1.SectionName(tl.Name),
					Port:     gwv1.PortNumber(tl.Port),
					Protocol: tl.Protocol,
					Hostname: ptrHostname(tl.Hostname),
				}
				entries = append(entries, entry)
				allMerged = append(allMerged, buildMergedListener(entry, shared_constants.ListenerSetKind, ls.Name, ls.Namespace))
			}
			lsData := buildTestListenerSetData(ls.Name, ls.Namespace, routeTestBaseTime, entries)
			allAccepted = append(allAccepted, lsData)
		}

		// Build the route context.
		lsCtx := &listenerSetRouteContext{
			AcceptedListenerSets: make(map[types.NamespacedName]listenerset.ListenerSetData),
			MergedListeners:      allMerged,
			ConflictMap:          make(map[listenerset.ListenerKey]listenerset.ConflictInfo),
		}
		for _, lsData := range allAccepted {
			key := types.NamespacedName{Name: lsData.ListenerSet.Name, Namespace: lsData.ListenerSet.Namespace}
			lsCtx.AcceptedListenerSets[key] = lsData
		}

		// Build parentRef targeting the chosen ListenerSet.
		targetLS := input.ListenerSets[input.TargetLSIndex]
		targetLSData := allAccepted[input.TargetLSIndex]
		parentRef := gwv1.ParentReference{
			Kind: ptrKind(shared_constants.ListenerSetKind),
			Name: gwv1.ObjectName(targetLS.Name),
		}
		if input.UseSectionName {
			parentRef.SectionName = ptrSectionName(targetLS.Listeners[input.SectionNameIndex].Name)
		}

		// Call resolveListenerSetRouteAttachment.
		matched := resolveListenerSetRouteAttachment(parentRef, targetLSData, lsCtx)

		// Verify: all matched listeners belong to the target ListenerSet.
		for _, ml := range matched {
			if ml.Source.Kind != shared_constants.ListenerSetKind ||
				ml.Source.Name != targetLS.Name ||
				ml.Source.Namespace != targetLS.Namespace {
				t.Logf("Matched listener from wrong source: %s/%s/%s, expected %s/%s",
					ml.Source.Kind, ml.Source.Name, ml.Source.Namespace, targetLS.Name, targetLS.Namespace)
				return false
			}
		}

		// Verify: with sectionName, at most one listener is returned.
		if input.UseSectionName {
			if len(matched) > 1 {
				t.Logf("With sectionName, expected at most 1 match, got %d", len(matched))
				return false
			}
			if len(matched) == 1 {
				expectedName := targetLS.Listeners[input.SectionNameIndex].Name
				if string(matched[0].Listener.Name) != expectedName {
					t.Logf("SectionName match returned wrong listener: got %s, expected %s",
						matched[0].Listener.Name, expectedName)
					return false
				}
			}
		}

		// Verify: without sectionName, all listeners from the target LS are returned.
		if !input.UseSectionName {
			if len(matched) != len(targetLS.Listeners) {
				t.Logf("Without sectionName, expected %d matches, got %d",
					len(targetLS.Listeners), len(matched))
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 7 (Route Scope Isolation) failed: %v", err)
	}
}

// ============================================================================
// Property 8: Route Discovery Correctness
// **Validates: Requirements 5.1, 5.2**
//
// For any route with a parentRef of kind ListenerSet, the route is discovered
// during reconciliation if and only if the referenced ListenerSet is in
// AcceptedSets. isListenerSetParentRef correctly identifies ListenerSet
// parentRefs, and resolveListenerSetKey correctly resolves the namespace.
// ============================================================================

type discoveryInput struct {
	// ParentRefs is a list of parentRefs with random kinds.
	ParentRefs []testParentRef
}

type testParentRef struct {
	Kind      string // "ListenerSet", "Gateway", or ""
	Name      string
	Namespace string // empty means use route namespace
}

func (discoveryInput) Generate(r *rand.Rand, size int) reflect.Value {
	input := discoveryInput{}
	kinds := []string{shared_constants.ListenerSetKind, "Gateway", ""}
	numRefs := r.Intn(5) + 1
	for i := 0; i < numRefs; i++ {
		ref := testParentRef{
			Kind: kinds[r.Intn(len(kinds))],
			Name: fmt.Sprintf("ref-%d", i),
		}
		if r.Intn(2) == 1 {
			ref.Namespace = routeTestNamespaces[r.Intn(len(routeTestNamespaces))]
		}
		input.ParentRefs = append(input.ParentRefs, ref)
	}
	return reflect.ValueOf(input)
}

func TestProperty_RouteDiscoveryCorrectness(t *testing.T) {
	property := func(input discoveryInput) bool {
		routeNamespace := "default"

		for _, tpr := range input.ParentRefs {
			// Build a gwv1.ParentReference.
			parentRef := gwv1.ParentReference{
				Name: gwv1.ObjectName(tpr.Name),
			}
			if tpr.Kind != "" {
				parentRef.Kind = ptrKind(tpr.Kind)
			}
			if tpr.Namespace != "" {
				parentRef.Namespace = ptrNamespace(tpr.Namespace)
			}

			// Test isListenerSetParentRef.
			isLS := isListenerSetParentRef(parentRef)
			expectedIsLS := tpr.Kind == shared_constants.ListenerSetKind
			if isLS != expectedIsLS {
				t.Logf("isListenerSetParentRef(%q) = %v, expected %v", tpr.Kind, isLS, expectedIsLS)
				return false
			}

			// Test resolveListenerSetKey for ListenerSet parentRefs.
			if isLS {
				key := resolveListenerSetKey(parentRef, routeNamespace)
				expectedNS := routeNamespace
				if tpr.Namespace != "" {
					expectedNS = tpr.Namespace
				}
				if key.Name != tpr.Name {
					t.Logf("resolveListenerSetKey name = %q, expected %q", key.Name, tpr.Name)
					return false
				}
				if key.Namespace != expectedNS {
					t.Logf("resolveListenerSetKey namespace = %q, expected %q", key.Namespace, expectedNS)
					return false
				}
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 8 (Route Discovery Correctness) failed: %v", err)
	}
}

// ============================================================================
// Property 9: Conflicted Listener Route Rejection
// **Validates: Requirement 5.5**
//
// For any route targeting a listener that is in the ConflictMap, the route is
// not attached to that listener's port. resolveListenerSetRouteAttachment
// returns listeners from the LS, but the caller checks ConflictMap. We verify
// that when a listener IS in the ConflictMap, it is correctly excluded from
// the merged list (findMergedListenerForEntry returns nil for conflicted
// listeners since they are not in MergedListeners).
// ============================================================================

type conflictRejectionInput struct {
	// NumListeners is the number of listeners in the ListenerSet (1-4).
	NumListeners int
	// ConflictedIndices marks which listeners are conflicted.
	ConflictedIndices []bool
	// UseSectionName controls whether the parentRef has a sectionName.
	UseSectionName bool
	// SectionNameIndex is the listener index to use as sectionName.
	SectionNameIndex int
}

func (conflictRejectionInput) Generate(r *rand.Rand, size int) reflect.Value {
	numListeners := r.Intn(4) + 1
	input := conflictRejectionInput{
		NumListeners:   numListeners,
		UseSectionName: r.Intn(2) == 1,
	}
	input.ConflictedIndices = make([]bool, numListeners)
	for i := 0; i < numListeners; i++ {
		input.ConflictedIndices[i] = r.Intn(2) == 1
	}
	input.SectionNameIndex = r.Intn(numListeners)
	return reflect.ValueOf(input)
}

func TestProperty_ConflictedListenerRouteRejection(t *testing.T) {
	property := func(input conflictRejectionInput) bool {
		lsName := "test-ls"
		lsNamespace := "default"

		// Build listener entries and merged listeners, excluding conflicted ones from merged.
		var entries []gwv1.ListenerEntry
		var mergedListeners []listenerset.MergedListener
		conflictMap := make(map[listenerset.ListenerKey]listenerset.ConflictInfo)

		for i := 0; i < input.NumListeners; i++ {
			port := gwv1.PortNumber(8080 + i)
			entry := gwv1.ListenerEntry{
				Name:     gwv1.SectionName(fmt.Sprintf("listener-%d", i)),
				Port:     port,
				Protocol: gwv1.HTTPProtocolType,
			}
			entries = append(entries, entry)

			lKey := listenerset.ListenerKey{
				Port:     int32(port),
				Hostname: "",
			}

			if input.ConflictedIndices[i] {
				// Conflicted: add to conflict map, do NOT add to merged listeners.
				conflictMap[lKey] = listenerset.ConflictInfo{
					Reason:  gwv1.ListenerReasonHostnameConflict,
					Message: "test conflict",
				}
			} else {
				// Not conflicted: add to merged listeners.
				mergedListeners = append(mergedListeners, buildMergedListener(
					entry, shared_constants.ListenerSetKind, lsName, lsNamespace,
				))
			}
		}

		lsData := buildTestListenerSetData(lsName, lsNamespace, routeTestBaseTime, entries)

		lsCtx := &listenerSetRouteContext{
			AcceptedListenerSets: map[types.NamespacedName]listenerset.ListenerSetData{
				{Name: lsName, Namespace: lsNamespace}: lsData,
			},
			MergedListeners: mergedListeners,
			ConflictMap:     conflictMap,
		}

		// Build parentRef.
		parentRef := gwv1.ParentReference{
			Kind: ptrKind(shared_constants.ListenerSetKind),
			Name: gwv1.ObjectName(lsName),
		}
		if input.UseSectionName {
			parentRef.SectionName = ptrSectionName(fmt.Sprintf("listener-%d", input.SectionNameIndex))
		}

		// Call resolveListenerSetRouteAttachment.
		matched := resolveListenerSetRouteAttachment(parentRef, lsData, lsCtx)

		// Verify: no matched listener should be conflicted.
		for _, ml := range matched {
			lKey := listenerset.ListenerKey{
				Port:     int32(ml.Listener.Port),
				Hostname: hostnameToString(ml.Listener.Hostname),
			}
			if _, conflicted := conflictMap[lKey]; conflicted {
				t.Logf("Matched listener on port %d is in ConflictMap but was returned", ml.Listener.Port)
				return false
			}
		}

		// Verify: count of matched listeners equals non-conflicted listeners
		// (for the targeted scope).
		if input.UseSectionName {
			idx := input.SectionNameIndex
			if input.ConflictedIndices[idx] {
				// Targeted listener is conflicted — should get 0 matches.
				if len(matched) != 0 {
					t.Logf("SectionName targets conflicted listener, expected 0 matches, got %d", len(matched))
					return false
				}
			} else {
				if len(matched) != 1 {
					t.Logf("SectionName targets non-conflicted listener, expected 1 match, got %d", len(matched))
					return false
				}
			}
		} else {
			expectedCount := 0
			for i := 0; i < input.NumListeners; i++ {
				if !input.ConflictedIndices[i] {
					expectedCount++
				}
			}
			if len(matched) != expectedCount {
				t.Logf("Without sectionName, expected %d non-conflicted matches, got %d", expectedCount, len(matched))
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 9 (Conflicted Listener Route Rejection) failed: %v", err)
	}
}

// ============================================================================
// Property 10: AllowedRoutes Namespace Relativity
// **Validates: Requirements 6.1, 6.2, 6.3, 6.4**
//
// For any route attached to a ListenerSet listener, the allowedRoutes namespace
// check uses the ListenerSet's namespace as the reference point (not the
// Gateway's namespace). When allowedRoutes.namespaces.from is Same, the route's
// namespace must equal the ListenerSet's namespace. When allowedRoutes is
// absent, the default is same-namespace-as-ListenerSet. When from is All, any
// namespace is permitted.
// ============================================================================

// mockPreLoadRoute is a minimal mock implementing preLoadRouteDescriptor for property tests.
type mockPreLoadRoute struct {
	namespacedName types.NamespacedName
	kind           RouteKind
	parentRefs     []gwv1.ParentReference
	hostnames      []gwv1.Hostname
	generation     int64
}

func (m *mockPreLoadRoute) GetRouteNamespacedName() types.NamespacedName { return m.namespacedName }
func (m *mockPreLoadRoute) GetRouteKind() RouteKind                      { return m.kind }
func (m *mockPreLoadRoute) GetRouteIdentifier() string {
	return m.namespacedName.Namespace + "/" + m.namespacedName.Name
}
func (m *mockPreLoadRoute) GetHostnames() []gwv1.Hostname         { return m.hostnames }
func (m *mockPreLoadRoute) GetParentRefs() []gwv1.ParentReference { return m.parentRefs }
func (m *mockPreLoadRoute) GetRawRoute() interface{}              { return nil }
func (m *mockPreLoadRoute) GetBackendRefs() []gwv1.BackendRef     { return nil }
func (m *mockPreLoadRoute) GetRouteGeneration() int64             { return m.generation }
func (m *mockPreLoadRoute) GetRouteCreateTimestamp() time.Time    { return routeTestBaseTime }
func (m *mockPreLoadRoute) GetRouteListenerRuleConfigRefs() []gwv1.LocalObjectReference {
	return nil
}
func (m *mockPreLoadRoute) GetCompatibleHostnamesByPort() map[int32][]gwv1.Hostname  { return nil }
func (m *mockPreLoadRoute) setCompatibleHostnamesByPort(_ map[int32][]gwv1.Hostname) {}
func (m *mockPreLoadRoute) loadAttachedRules(_ context.Context, _ client.Client) (RouteDescriptor, []routeLoadError) {
	return nil, nil
}

var _ preLoadRouteDescriptor = &mockPreLoadRoute{}

type namespaceRelativityInput struct {
	RouteNamespace      string
	ListenerSetNS       string
	GatewayNS           string
	AllowedRoutesPolicy int // 0=nil, 1=Same, 2=All
}

func (namespaceRelativityInput) Generate(r *rand.Rand, size int) reflect.Value {
	return reflect.ValueOf(namespaceRelativityInput{
		RouteNamespace:      routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		ListenerSetNS:       routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		GatewayNS:           routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		AllowedRoutesPolicy: r.Intn(3),
	})
}

func TestProperty_AllowedRoutesNamespaceRelativity(t *testing.T) {
	property := func(input namespaceRelativityInput) bool {
		route := &mockPreLoadRoute{
			namespacedName: types.NamespacedName{Name: "test-route", Namespace: input.RouteNamespace},
			kind:           HTTPRouteKind,
		}

		listener := gwv1.Listener{
			Name:     "test-listener",
			Port:     8080,
			Protocol: gwv1.HTTPProtocolType,
		}

		switch input.AllowedRoutesPolicy {
		case 0:
			// nil allowedRoutes — default is Same namespace as ListenerSet.
			listener.AllowedRoutes = nil
		case 1:
			// Same namespace.
			from := gwv1.NamespacesFromSame
			listener.AllowedRoutes = &gwv1.AllowedRoutes{
				Namespaces: &gwv1.RouteNamespaces{
					From: &from,
				},
			}
		case 2:
			// All namespaces.
			from := gwv1.NamespacesFromAll
			listener.AllowedRoutes = &gwv1.AllowedRoutes{
				Namespaces: &gwv1.RouteNamespaces{
					From: &from,
				},
			}
		}

		allowed := isRouteAllowedByListenerSetListener(
			context.Background(), nil, route, listener, input.ListenerSetNS,
		)

		// Compute expected result.
		var expected bool
		switch input.AllowedRoutesPolicy {
		case 0: // nil → Same as ListenerSet NS
			expected = input.RouteNamespace == input.ListenerSetNS
		case 1: // Same → Same as ListenerSet NS (NOT Gateway NS)
			expected = input.RouteNamespace == input.ListenerSetNS
		case 2: // All → always allowed
			expected = true
		}

		if allowed != expected {
			t.Logf("AllowedRoutes policy=%d, routeNS=%q, lsNS=%q, gwNS=%q: got %v, expected %v",
				input.AllowedRoutesPolicy, input.RouteNamespace, input.ListenerSetNS, input.GatewayNS, allowed, expected)
			return false
		}

		// Key property: when policy is Same, the check uses ListenerSet NS, not Gateway NS.
		// If routeNS == gwNS but routeNS != lsNS, the route should be REJECTED.
		if input.AllowedRoutesPolicy == 1 &&
			input.RouteNamespace == input.GatewayNS &&
			input.RouteNamespace != input.ListenerSetNS {
			if allowed {
				t.Logf("CRITICAL: Same policy allowed route in GW namespace but not LS namespace")
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 10 (AllowedRoutes Namespace Relativity) failed: %v", err)
	}
}

// ============================================================================
// Property 11: ReferenceGrant Independence
// **Validates: Requirements 7.1, 7.2, 7.3**
//
// For any route in a different namespace referencing a ListenerSet, the route
// requires a ReferenceGrant in the ListenerSet's namespace. The current
// implementation rejects ALL cross-namespace routes to ListenerSets (with a
// TODO for full ReferenceGrant checking). We verify: if route namespace !=
// ListenerSet namespace, the route is rejected with RefNotPermitted.
// ============================================================================

type refGrantInput struct {
	RouteNamespace string
	LSNamespace    string
	LSName         string
}

func (refGrantInput) Generate(r *rand.Rand, size int) reflect.Value {
	return reflect.ValueOf(refGrantInput{
		RouteNamespace: routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		LSNamespace:    routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		LSName:         routeTestLSNames[r.Intn(len(routeTestLSNames))],
	})
}

func TestProperty_ReferenceGrantIndependence(t *testing.T) {
	property := func(input refGrantInput) bool {
		isCrossNamespace := input.RouteNamespace != input.LSNamespace

		// Build a minimal scenario: one ListenerSet with one listener, one route.
		entry := gwv1.ListenerEntry{
			Name:     "listener-0",
			Port:     8080,
			Protocol: gwv1.HTTPProtocolType,
		}
		lsData := buildTestListenerSetData(input.LSName, input.LSNamespace, routeTestBaseTime, []gwv1.ListenerEntry{entry})
		lsKey := types.NamespacedName{Name: input.LSName, Namespace: input.LSNamespace}

		mergedListeners := []listenerset.MergedListener{
			buildMergedListener(entry, shared_constants.ListenerSetKind, input.LSName, input.LSNamespace),
		}

		lsCtx := buildListenerSetRouteContext(
			[]listenerset.ListenerSetData{lsData},
			&listenerset.MergeResult{
				MergedListeners: mergedListeners,
				ConflictMap:     make(map[listenerset.ListenerKey]listenerset.ConflictInfo),
			},
		)

		// Build a mock route.
		route := &mockPreLoadRoute{
			namespacedName: types.NamespacedName{Name: "test-route", Namespace: input.RouteNamespace},
			kind:           HTTPRouteKind,
			parentRefs: []gwv1.ParentReference{
				{
					Kind:      ptrKind(shared_constants.ListenerSetKind),
					Name:      gwv1.ObjectName(input.LSName),
					Namespace: ptrNamespace(input.LSNamespace),
				},
			},
		}

		// Simulate the logic from loadRoutesForListenerSetParentRefs:
		// After resolveListenerSetRouteAttachment returns matched listeners,
		// for each matched listener, check if cross-namespace.
		matchedListeners := resolveListenerSetRouteAttachment(route.parentRefs[0], lsData, lsCtx)

		var rejectedCrossNS bool
		var attachedPorts []int32
		for _, ml := range matchedListeners {
			// Check conflict (none in this test).
			lKey := listenerset.ListenerKey{
				Port:     int32(ml.Listener.Port),
				Hostname: hostnameToString(ml.Listener.Hostname),
			}
			if _, conflicted := lsCtx.ConflictMap[lKey]; conflicted {
				continue
			}

			// Check allowedRoutes (same namespace, so allowed).
			if !isRouteAllowedByListenerSetListener(context.Background(), nil, route, ml.Listener, input.LSNamespace) {
				continue
			}

			// Check cross-namespace (the key property).
			if route.namespacedName.Namespace != lsData.ListenerSet.Namespace {
				rejectedCrossNS = true
				continue
			}

			attachedPorts = append(attachedPorts, int32(ml.Listener.Port))
		}

		_ = lsKey

		if isCrossNamespace {
			// Cross-namespace routes must be rejected.
			if len(attachedPorts) > 0 {
				t.Logf("Cross-namespace route (routeNS=%q, lsNS=%q) was attached, expected rejection",
					input.RouteNamespace, input.LSNamespace)
				return false
			}
			// For cross-namespace with matching allowedRoutes, rejectedCrossNS should be true.
			// But if allowedRoutes rejected it first (different namespace with Same policy),
			// rejectedCrossNS may be false. Both are valid rejections.
		} else {
			// Same-namespace routes should be attached (no cross-NS rejection).
			if rejectedCrossNS {
				t.Logf("Same-namespace route was rejected as cross-namespace")
				return false
			}
			if len(attachedPorts) != 1 {
				t.Logf("Same-namespace route expected 1 attached port, got %d", len(attachedPorts))
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 11 (ReferenceGrant Independence) failed: %v", err)
	}
}

// ============================================================================
// Property 12: Route Status ParentRef Fidelity
// **Validates: Requirement 8.1**
//
// For any route attached via a ListenerSet parentRef, the route's status
// parentRef kind is ListenerSet and the name is the ListenerSet's name (not
// the parent Gateway's name).
// ============================================================================

type parentRefFidelityInput struct {
	LSName      string
	LSNamespace string
	RouteName   string
	RouteNS     string
	RouteKind   RouteKind
	Generation  int64
	HasPort     bool
	Port        int32
	HasSection  bool
	SectionName string
}

func (parentRefFidelityInput) Generate(r *rand.Rand, size int) reflect.Value {
	kinds := []RouteKind{HTTPRouteKind, GRPCRouteKind, TCPRouteKind}
	input := parentRefFidelityInput{
		LSName:      routeTestLSNames[r.Intn(len(routeTestLSNames))],
		LSNamespace: routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		RouteName:   fmt.Sprintf("route-%d", r.Intn(100)),
		RouteNS:     routeTestNamespaces[r.Intn(len(routeTestNamespaces))],
		RouteKind:   kinds[r.Intn(len(kinds))],
		Generation:  int64(r.Intn(50) + 1),
		HasPort:     r.Intn(2) == 1,
		Port:        routeTestPorts[r.Intn(len(routeTestPorts))],
		HasSection:  r.Intn(2) == 1,
		SectionName: fmt.Sprintf("section-%d", r.Intn(10)),
	}
	return reflect.ValueOf(input)
}

func TestProperty_RouteStatusParentRefFidelity(t *testing.T) {
	property := func(input parentRefFidelityInput) bool {
		lsKey := types.NamespacedName{Name: input.LSName, Namespace: input.LSNamespace}
		routeNN := types.NamespacedName{Name: input.RouteName, Namespace: input.RouteNS}

		var port *gwv1.PortNumber
		if input.HasPort {
			p := gwv1.PortNumber(input.Port)
			port = &p
		}
		var sectionName *gwv1.SectionName
		if input.HasSection {
			sectionName = ptrSectionName(input.SectionName)
		}

		rd := generateListenerSetRouteData(
			true, true,
			string(gwv1.RouteConditionAccepted),
			RouteStatusInfoAcceptedMessage,
			routeNN, input.RouteKind, input.Generation,
			lsKey, port, sectionName,
		)

		// Verify ParentRef kind is ListenerSet.
		assert.NotNil(t, rd.ParentRef.Kind)
		if rd.ParentRef.Kind == nil || string(*rd.ParentRef.Kind) != shared_constants.ListenerSetKind {
			t.Logf("ParentRef.Kind = %v, expected %q", rd.ParentRef.Kind, shared_constants.ListenerSetKind)
			return false
		}

		// Verify ParentRef name is the ListenerSet's name.
		if string(rd.ParentRef.Name) != input.LSName {
			t.Logf("ParentRef.Name = %q, expected %q", rd.ParentRef.Name, input.LSName)
			return false
		}

		// Verify ParentRef namespace is the ListenerSet's namespace.
		if rd.ParentRef.Namespace == nil || string(*rd.ParentRef.Namespace) != input.LSNamespace {
			t.Logf("ParentRef.Namespace = %v, expected %q", rd.ParentRef.Namespace, input.LSNamespace)
			return false
		}

		// Verify ParentRef group is gateway API group.
		if rd.ParentRef.Group == nil || string(*rd.ParentRef.Group) != shared_constants.GatewayAPIResourcesGroup {
			t.Logf("ParentRef.Group = %v, expected %q", rd.ParentRef.Group, shared_constants.GatewayAPIResourcesGroup)
			return false
		}

		// Verify route metadata is preserved.
		if rd.RouteMetadata.RouteName != input.RouteName {
			t.Logf("RouteName = %q, expected %q", rd.RouteMetadata.RouteName, input.RouteName)
			return false
		}
		if rd.RouteMetadata.RouteNamespace != input.RouteNS {
			t.Logf("RouteNamespace = %q, expected %q", rd.RouteMetadata.RouteNamespace, input.RouteNS)
			return false
		}
		if rd.RouteMetadata.RouteKind != string(input.RouteKind) {
			t.Logf("RouteKind = %q, expected %q", rd.RouteMetadata.RouteKind, input.RouteKind)
			return false
		}
		if rd.RouteMetadata.RouteGeneration != input.Generation {
			t.Logf("RouteGeneration = %d, expected %d", rd.RouteMetadata.RouteGeneration, input.Generation)
			return false
		}

		// Verify port and sectionName are passed through.
		if input.HasPort {
			if rd.ParentRef.Port == nil || int32(*rd.ParentRef.Port) != input.Port {
				t.Logf("ParentRef.Port mismatch")
				return false
			}
		} else {
			if rd.ParentRef.Port != nil {
				t.Logf("ParentRef.Port should be nil")
				return false
			}
		}

		if input.HasSection {
			if rd.ParentRef.SectionName == nil || string(*rd.ParentRef.SectionName) != input.SectionName {
				t.Logf("ParentRef.SectionName mismatch")
				return false
			}
		} else {
			if rd.ParentRef.SectionName != nil {
				t.Logf("ParentRef.SectionName should be nil")
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 12 (Route Status ParentRef Fidelity) failed: %v", err)
	}
}

// ============================================================================
// Property 13: Route Origin Completeness
// **Validates: Requirement 8.2**
//
// For any route in the LoaderResult, there exists a corresponding RouteOrigin
// entry that identifies whether the route was attached via a Gateway or
// ListenerSet parentRef. We test this by simulating the logic of
// loadRoutesForListenerSetParentRefs: for every route that gets attached to a
// port, a RouteOrigin entry must be recorded.
// ============================================================================

type routeOriginInput struct {
	// NumRoutes is the number of routes (1-3).
	NumRoutes int
	// Routes defines the routes with their parentRefs.
	Routes []testRouteForOrigin
	// LSName and LSNamespace define the ListenerSet.
	LSName      string
	LSNamespace string
}

type testRouteForOrigin struct {
	Name      string
	Namespace string
	// TargetsLS controls whether this route targets the ListenerSet.
	TargetsLS bool
	// HasSectionName controls whether the parentRef has a sectionName.
	HasSectionName bool
}

func (routeOriginInput) Generate(r *rand.Rand, size int) reflect.Value {
	lsNS := routeTestNamespaces[r.Intn(len(routeTestNamespaces))]
	numRoutes := r.Intn(3) + 1
	input := routeOriginInput{
		NumRoutes:   numRoutes,
		LSName:      routeTestLSNames[r.Intn(len(routeTestLSNames))],
		LSNamespace: lsNS,
	}
	for i := 0; i < numRoutes; i++ {
		input.Routes = append(input.Routes, testRouteForOrigin{
			Name:           fmt.Sprintf("route-%d", i),
			Namespace:      lsNS, // Same namespace to avoid cross-NS rejection.
			TargetsLS:      r.Intn(2) == 1,
			HasSectionName: r.Intn(2) == 1,
		})
	}
	return reflect.ValueOf(input)
}

func TestProperty_RouteOriginCompleteness(t *testing.T) {
	property := func(input routeOriginInput) bool {
		lsKey := types.NamespacedName{Name: input.LSName, Namespace: input.LSNamespace}

		// Build a ListenerSet with one listener.
		entry := gwv1.ListenerEntry{
			Name:     "listener-0",
			Port:     8080,
			Protocol: gwv1.HTTPProtocolType,
		}
		lsData := buildTestListenerSetData(input.LSName, input.LSNamespace, routeTestBaseTime, []gwv1.ListenerEntry{entry})

		mergedListeners := []listenerset.MergedListener{
			buildMergedListener(entry, shared_constants.ListenerSetKind, input.LSName, input.LSNamespace),
		}

		lsCtx := buildListenerSetRouteContext(
			[]listenerset.ListenerSetData{lsData},
			&listenerset.MergeResult{
				MergedListeners: mergedListeners,
				ConflictMap:     make(map[listenerset.ListenerKey]listenerset.ConflictInfo),
			},
		)

		// Simulate the route processing logic from loadRoutesForListenerSetParentRefs.
		routesByPort := make(map[int32][]string) // port -> route names
		routeOrigins := make(map[types.NamespacedName][]RouteOrigin)

		for _, tr := range input.Routes {
			if !tr.TargetsLS {
				continue
			}

			routeNN := types.NamespacedName{Name: tr.Name, Namespace: tr.Namespace}

			parentRef := gwv1.ParentReference{
				Kind: ptrKind(shared_constants.ListenerSetKind),
				Name: gwv1.ObjectName(input.LSName),
			}
			if tr.HasSectionName {
				parentRef.SectionName = ptrSectionName("listener-0")
			}

			// Check if LS is accepted.
			_, accepted := lsCtx.AcceptedListenerSets[lsKey]
			if !accepted {
				continue
			}

			// Resolve attachment.
			matchedListeners := resolveListenerSetRouteAttachment(parentRef, lsData, lsCtx)
			if len(matchedListeners) == 0 {
				continue
			}

			var attachedPorts []int32
			for _, ml := range matchedListeners {
				lKey := listenerset.ListenerKey{
					Port:     int32(ml.Listener.Port),
					Hostname: hostnameToString(ml.Listener.Hostname),
				}
				if _, conflicted := lsCtx.ConflictMap[lKey]; conflicted {
					continue
				}
				// Same namespace, so allowedRoutes passes.
				if routeNN.Namespace != lsData.ListenerSet.Namespace {
					continue
				}
				port := int32(ml.Listener.Port)
				attachedPorts = append(attachedPorts, port)
				routesByPort[port] = append(routesByPort[port], tr.Name)
			}

			if len(attachedPorts) > 0 {
				routeOrigins[routeNN] = append(routeOrigins[routeNN], RouteOrigin{
					ParentRefKind:        shared_constants.ListenerSetKind,
					ParentRefName:        lsKey.Name,
					ParentRefNamespace:   lsKey.Namespace,
					SectionName:          parentRef.SectionName,
					MatchedListenerPorts: attachedPorts,
				})
			}
		}

		// Property: for every route attached to a port, there must be a RouteOrigin.
		for port, routeNames := range routesByPort {
			for _, rName := range routeNames {
				// Find the route's namespace (all routes use lsNamespace in this test).
				routeNN := types.NamespacedName{Name: rName, Namespace: input.LSNamespace}
				origins, hasOrigin := routeOrigins[routeNN]
				if !hasOrigin {
					t.Logf("Route %s attached to port %d but has no RouteOrigin entry", rName, port)
					return false
				}

				// Verify at least one origin covers this port.
				portCovered := false
				for _, origin := range origins {
					for _, p := range origin.MatchedListenerPorts {
						if p == port {
							portCovered = true
							break
						}
					}
					if portCovered {
						break
					}
				}
				if !portCovered {
					t.Logf("Route %s attached to port %d but no RouteOrigin covers that port", rName, port)
					return false
				}

				// Verify origin references ListenerSet, not Gateway.
				for _, origin := range origins {
					if origin.ParentRefKind != shared_constants.ListenerSetKind {
						t.Logf("RouteOrigin for %s has kind %q, expected %q",
							rName, origin.ParentRefKind, shared_constants.ListenerSetKind)
						return false
					}
					if origin.ParentRefName != input.LSName {
						t.Logf("RouteOrigin for %s has name %q, expected %q",
							rName, origin.ParentRefName, input.LSName)
						return false
					}
				}
			}
		}

		// Verify no phantom origins (origins without attached ports).
		for routeNN, origins := range routeOrigins {
			for _, origin := range origins {
				for _, port := range origin.MatchedListenerPorts {
					routeNames, hasPort := routesByPort[port]
					if !hasPort {
						t.Logf("RouteOrigin for %s references port %d but no route attached there", routeNN.Name, port)
						return false
					}
					found := false
					for _, rn := range routeNames {
						if rn == routeNN.Name {
							found = true
							break
						}
					}
					if !found {
						t.Logf("RouteOrigin for %s references port %d but route not in routesByPort", routeNN.Name, port)
						return false
					}
				}
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Property 13 (Route Origin Completeness) failed: %v", err)
	}
}
