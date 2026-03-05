package listenerset

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6

func hostnamePtr(h string) *gwv1.Hostname {
	hostname := gwv1.Hostname(h)
	return &hostname
}

func makeTestGateway(name, namespace string, ts time.Time, listeners []gwv1.Listener) gwv1.Gateway {
	return gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.NewTime(ts),
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners:        listeners,
		},
	}
}

func makeTestListenerSetData(name, namespace string, ts time.Time, entries []gwv1.ListenerEntry) ListenerSetData {
	return ListenerSetData{
		ListenerSet: &gwv1.ListenerSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				CreationTimestamp: metav1.NewTime(ts),
			},
		},
		Listeners: entries,
	}
}

var testBaseTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

func TestMergeListeners(t *testing.T) {
	merger := NewListenerMerger()

	tests := []struct {
		name              string
		gateway           gwv1.Gateway
		listenerSets      []ListenerSetData
		expectedMerged    int
		expectedConflicts int
		checkFunc         func(t *testing.T, result *MergeResult)
	}{
		{
			name: "no ListenerSets - only Gateway listeners in MergedListeners",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType, Hostname: hostnamePtr("example.com")},
			}),
			listenerSets:      nil,
			expectedMerged:    2,
			expectedConflicts: 0,
			checkFunc: func(t *testing.T, result *MergeResult) {
				for _, ml := range result.MergedListeners {
					assert.Equal(t, "Gateway", ml.Source.Kind)
					assert.Equal(t, "gw", ml.Source.Name)
				}
			},
		},
		{
			name: "one ListenerSet with no conflicts - all listeners merged",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-1", "default", testBaseTime.Add(time.Second), []gwv1.ListenerEntry{
					{Name: "extra", Port: 8080, Protocol: gwv1.HTTPProtocolType},
				}),
			},
			expectedMerged:    2,
			expectedConflicts: 0,
			checkFunc: func(t *testing.T, result *MergeResult) {
				assert.Equal(t, "Gateway", result.MergedListeners[0].Source.Kind)
				assert.Equal(t, "ListenerSet", result.MergedListeners[1].Source.Kind)
				assert.Equal(t, "ls-1", result.MergedListeners[1].Source.Name)
			},
		},
		{
			name: "Gateway listener wins over LS listener on same port+protocol+hostname (Req 2.2)",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-1", "default", testBaseTime.Add(time.Second), []gwv1.ListenerEntry{
					{Name: "also-http", Port: 80, Protocol: gwv1.HTTPProtocolType},
				}),
			},
			expectedMerged:    1,
			expectedConflicts: 1,
			checkFunc: func(t *testing.T, result *MergeResult) {
				assert.Equal(t, "Gateway", result.MergedListeners[0].Source.Kind)
				lk := ListenerKey{Port: 80, Hostname: ""}
				ci, exists := result.ConflictMap[lk]
				assert.True(t, exists)
				assert.Equal(t, gwv1.ListenerReasonHostnameConflict, ci.Reason)
				assert.Equal(t, "Gateway", ci.WinnerSource.Kind)
			},
		},
		{
			name: "older ListenerSet wins over newer on same port+protocol+hostname (Req 2.3)",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-old", "default", testBaseTime.Add(1*time.Second), []gwv1.ListenerEntry{
					{Name: "listener-a", Port: 8080, Protocol: gwv1.HTTPProtocolType},
				}),
				makeTestListenerSetData("ls-new", "default", testBaseTime.Add(10*time.Second), []gwv1.ListenerEntry{
					{Name: "listener-b", Port: 8080, Protocol: gwv1.HTTPProtocolType},
				}),
			},
			expectedMerged:    2, // GW listener + ls-old listener
			expectedConflicts: 1,
			checkFunc: func(t *testing.T, result *MergeResult) {
				// Find the merged listener on port 8080
				var found bool
				for _, ml := range result.MergedListeners {
					if int32(ml.Listener.Port) == 8080 {
						assert.Equal(t, "ls-old", ml.Source.Name)
						found = true
					}
				}
				assert.True(t, found, "ls-old listener should be in MergedListeners")
				lk := ListenerKey{Port: 8080, Hostname: ""}
				ci, exists := result.ConflictMap[lk]
				assert.True(t, exists)
				assert.Equal(t, "ls-old", ci.WinnerSource.Name)
			},
		},
		{
			name: "alphabetical tiebreaker when timestamps are equal (Req 2.1)",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-beta", "default", testBaseTime.Add(5*time.Second), []gwv1.ListenerEntry{
					{Name: "listener-beta", Port: 9090, Protocol: gwv1.HTTPProtocolType},
				}),
				makeTestListenerSetData("ls-alpha", "default", testBaseTime.Add(5*time.Second), []gwv1.ListenerEntry{
					{Name: "listener-alpha", Port: 9090, Protocol: gwv1.HTTPProtocolType},
				}),
			},
			expectedMerged:    2, // GW listener + ls-alpha listener
			expectedConflicts: 1,
			checkFunc: func(t *testing.T, result *MergeResult) {
				for _, ml := range result.MergedListeners {
					if int32(ml.Listener.Port) == 9090 {
						assert.Equal(t, "ls-alpha", ml.Source.Name, "alphabetically earlier name should win")
					}
				}
				lk := ListenerKey{Port: 9090, Hostname: ""}
				ci, exists := result.ConflictMap[lk]
				assert.True(t, exists)
				assert.Equal(t, "ls-alpha", ci.WinnerSource.Name)
			},
		},
		{
			name: "port+protocol conflict - same port different protocol marks LS as conflicted (Req 2.4)",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-1", "default", testBaseTime.Add(time.Second), []gwv1.ListenerEntry{
					{Name: "tls", Port: 443, Protocol: gwv1.TLSProtocolType},
				}),
			},
			expectedMerged:    1,
			expectedConflicts: 1,
			checkFunc: func(t *testing.T, result *MergeResult) {
				assert.Equal(t, "Gateway", result.MergedListeners[0].Source.Kind)
				lk := ListenerKey{Port: 443, Hostname: ""}
				ci, exists := result.ConflictMap[lk]
				assert.True(t, exists)
				assert.Equal(t, gwv1.ListenerReasonProtocolConflict, ci.Reason)
				assert.Equal(t, "Gateway", ci.WinnerSource.Kind)
			},
		},
		{
			name: "multiple ListenerSets with mixed conflicts",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-a", "default", testBaseTime.Add(1*time.Second), []gwv1.ListenerEntry{
					{Name: "a-8080", Port: 8080, Protocol: gwv1.HTTPProtocolType},
					{Name: "a-9090", Port: 9090, Protocol: gwv1.HTTPProtocolType},
				}),
				makeTestListenerSetData("ls-b", "default", testBaseTime.Add(2*time.Second), []gwv1.ListenerEntry{
					{Name: "b-8080", Port: 8080, Protocol: gwv1.HTTPProtocolType}, // conflicts with ls-a
					{Name: "b-3000", Port: 3000, Protocol: gwv1.HTTPProtocolType}, // no conflict
				}),
				makeTestListenerSetData("ls-c", "default", testBaseTime.Add(3*time.Second), []gwv1.ListenerEntry{
					{Name: "c-80", Port: 80, Protocol: gwv1.HTTPProtocolType}, // conflicts with GW
				}),
			},
			expectedMerged:    4, // GW:80 + ls-a:8080 + ls-a:9090 + ls-b:3000
			expectedConflicts: 2, // ls-b:8080 + ls-c:80
			checkFunc: func(t *testing.T, result *MergeResult) {
				mergedPorts := make(map[int32]string)
				for _, ml := range result.MergedListeners {
					mergedPorts[int32(ml.Listener.Port)] = ml.Source.Name
				}
				assert.Equal(t, "gw", mergedPorts[80])
				assert.Equal(t, "ls-a", mergedPorts[8080])
				assert.Equal(t, "ls-a", mergedPorts[9090])
				assert.Equal(t, "ls-b", mergedPorts[3000])

				// ls-b:8080 conflicted with ls-a
				lk8080 := ListenerKey{Port: 8080, Hostname: ""}
				ci, exists := result.ConflictMap[lk8080]
				assert.True(t, exists)
				assert.Equal(t, "ls-a", ci.WinnerSource.Name)

				// ls-c:80 conflicted with GW
				lk80 := ListenerKey{Port: 80, Hostname: ""}
				ci80, exists := result.ConflictMap[lk80]
				assert.True(t, exists)
				assert.Equal(t, "Gateway", ci80.WinnerSource.Kind)
			},
		},
		{
			name: "Gateway listener never in ConflictMap (Req 2.6)",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
				{Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-1", "default", testBaseTime.Add(time.Second), []gwv1.ListenerEntry{
					{Name: "ls-http", Port: 80, Protocol: gwv1.HTTPProtocolType},
					{Name: "ls-https", Port: 443, Protocol: gwv1.HTTPSProtocolType},
				}),
			},
			expectedMerged:    2,
			expectedConflicts: 2,
			checkFunc: func(t *testing.T, result *MergeResult) {
				// All merged listeners must be from Gateway
				for _, ml := range result.MergedListeners {
					assert.Equal(t, "Gateway", ml.Source.Kind)
				}
				// All conflict winners must be Gateway
				for _, ci := range result.ConflictMap {
					assert.Equal(t, "Gateway", ci.WinnerSource.Kind)
				}
			},
		},
		{
			name: "conservation: merged + conflicted == total (Req 2.5)",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-1", "default", testBaseTime.Add(1*time.Second), []gwv1.ListenerEntry{
					{Name: "a", Port: 80, Protocol: gwv1.HTTPProtocolType},
					{Name: "b", Port: 8080, Protocol: gwv1.HTTPProtocolType},
				}),
				makeTestListenerSetData("ls-2", "default", testBaseTime.Add(2*time.Second), []gwv1.ListenerEntry{
					{Name: "c", Port: 8080, Protocol: gwv1.HTTPProtocolType},
					{Name: "d", Port: 9090, Protocol: gwv1.HTTPProtocolType},
				}),
			},
			expectedMerged:    3, // GW:80 + ls-1:8080 + ls-2:9090
			expectedConflicts: 2, // ls-1:80 (conflicts with GW) + ls-2:8080 (conflicts with ls-1)
			checkFunc: func(t *testing.T, result *MergeResult) {
				totalInput := 1 + 2 + 2 // GW + ls-1 + ls-2
				// Count individual conflicted listeners
				conflictedCount := 0
				mergedCount := len(result.MergedListeners)

				// Build merged set for lookup
				type lid struct {
					Kind  string
					Name  string
					LName string
				}
				mergedSet := make(map[lid]bool)
				for _, ml := range result.MergedListeners {
					mergedSet[lid{ml.Source.Kind, ml.Source.Name, string(ml.Listener.Name)}] = true
				}

				// Check ls-1 listeners
				if !mergedSet[lid{"ListenerSet", "ls-1", "a"}] {
					conflictedCount++
				}
				if !mergedSet[lid{"ListenerSet", "ls-1", "b"}] {
					conflictedCount++
				}
				// Check ls-2 listeners
				if !mergedSet[lid{"ListenerSet", "ls-2", "c"}] {
					conflictedCount++
				}
				if !mergedSet[lid{"ListenerSet", "ls-2", "d"}] {
					conflictedCount++
				}

				assert.Equal(t, totalInput, mergedCount+conflictedCount,
					"conservation: merged + conflicted must equal total input listeners")
			},
		},
		{
			name: "hostname conflict on same port - different hostnames coexist",
			gateway: makeTestGateway("gw", "default", testBaseTime, []gwv1.Listener{
				{Name: "http-example", Port: 80, Protocol: gwv1.HTTPProtocolType, Hostname: hostnamePtr("example.com")},
			}),
			listenerSets: []ListenerSetData{
				makeTestListenerSetData("ls-1", "default", testBaseTime.Add(time.Second), []gwv1.ListenerEntry{
					{Name: "http-api", Port: 80, Protocol: gwv1.HTTPProtocolType, Hostname: hostnamePtr("api.example.com")},
				}),
			},
			expectedMerged:    2,
			expectedConflicts: 0,
			checkFunc: func(t *testing.T, result *MergeResult) {
				assert.Equal(t, "Gateway", result.MergedListeners[0].Source.Kind)
				assert.Equal(t, "ListenerSet", result.MergedListeners[1].Source.Kind)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := merger.MergeListeners(tc.gateway, tc.listenerSets)

			assert.NotNil(t, result)
			assert.Equal(t, tc.expectedMerged, len(result.MergedListeners), "merged count mismatch")
			assert.Equal(t, tc.expectedConflicts, len(result.ConflictMap), "conflict count mismatch")

			if tc.checkFunc != nil {
				tc.checkFunc(t, result)
			}
		})
	}
}
