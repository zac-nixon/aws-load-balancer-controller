package routeutils

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	gateway_constants "sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type mockMapper struct {
	t                  *testing.T
	expectedRoutes     []preLoadRouteDescriptor
	mapToReturn        map[int][]preLoadRouteDescriptor
	listenerRouteCount map[gwv1.SectionName]int32
	routeStatusUpdates []RouteData
}

func (m *mockMapper) mapGatewayAndRoutes(context context.Context, gw gwv1.Gateway, routes []preLoadRouteDescriptor) (map[int][]preLoadRouteDescriptor, map[int32]map[string][]gwv1.Hostname, []RouteData, map[string][]gwv1.ParentReference, map[gwv1.SectionName]int32, error) {
	assert.ElementsMatch(m.t, m.expectedRoutes, routes)
	matchedParentRefs := make(map[string][]gwv1.ParentReference)
	for _, routeList := range m.mapToReturn {
		for _, route := range routeList {
			routeKey := route.GetRouteIdentifier()
			matchedParentRefs[routeKey] = []gwv1.ParentReference{{}}
		}
	}
	return m.mapToReturn, make(map[int32]map[string][]gwv1.Hostname), m.routeStatusUpdates, matchedParentRefs, m.listenerRouteCount, nil
}

var _ RouteDescriptor = &mockRoute{}

type mockRoute struct {
	namespacedName            types.NamespacedName
	routeKind                 RouteKind
	generation                int64
	hostnames                 []gwv1.Hostname
	CompatibleHostnamesByPort map[int32][]gwv1.Hostname
}

func (m *mockRoute) GetCompatibleHostnamesByPort() map[int32][]gwv1.Hostname {
	return m.CompatibleHostnamesByPort
}

func (m *mockRoute) setCompatibleHostnamesByPort(hostnamesByPort map[int32][]gwv1.Hostname) {
	m.CompatibleHostnamesByPort = hostnamesByPort
}

func (m *mockRoute) loadAttachedRules(context context.Context, k8sClient client.Client) (RouteDescriptor, []routeLoadError) {
	return m, nil
}

func (m *mockRoute) GetRouteNamespacedName() types.NamespacedName {
	return m.namespacedName
}

func (m *mockRoute) GetRouteKind() RouteKind {
	return m.routeKind
}

func (m *mockRoute) GetHostnames() []gwv1.Hostname {
	return m.hostnames
}

func (m *mockRoute) GetParentRefs() []gwv1.ParentReference {
	//TODO implement me
	panic("implement me")
}

func (m *mockRoute) GetBackendRefs() []gwv1.BackendRef {
	//TODO implement me
	panic("implement me")
}
func (m *mockRoute) GetRouteListenerRuleConfigRefs() []gwv1.LocalObjectReference {
	//TODO implement me
	panic("implement me")
}

func (m *mockRoute) GetRouteGeneration() int64 {
	return m.generation
}

func (m *mockRoute) GetRawRoute() interface{} {
	//TODO implement me
	panic("implement me")
}

func (m *mockRoute) GetAttachedRules() []RouteRule {
	//TODO implement me
	panic("implement me")
}

func (m *mockRoute) GetRouteCreateTimestamp() time.Time {
	panic("implement me")
}

func (m *mockRoute) GetRouteIdentifier() string {
	return string(m.GetRouteKind()) + "-" + m.GetRouteNamespacedName().String()
}

func Test_LoadRoutesForGateway(t *testing.T) {
	testNamespace := gwv1.Namespace("gw-ns")
	testHostname := gwv1.Hostname("example.com")

	preLoadHTTPRoutes := []preLoadRouteDescriptor{
		&mockRoute{
			namespacedName: types.NamespacedName{
				Namespace: "http1-ns",
				Name:      "http1",
			},
			routeKind: HTTPRouteKind,
		},
		&mockRoute{
			namespacedName: types.NamespacedName{
				Namespace: "http2-ns",
				Name:      "http2",
			},
			routeKind: HTTPRouteKind,
		},
		&mockRoute{
			namespacedName: types.NamespacedName{
				Namespace: "http3-ns",
				Name:      "http3",
			},
			routeKind: HTTPRouteKind,
		},
	}

	loadedHTTPRoutes := make([]RouteDescriptor, 0)
	for _, preload := range preLoadHTTPRoutes {
		r, _ := preload.loadAttachedRules(nil, nil)
		loadedHTTPRoutes = append(loadedHTTPRoutes, r)
	}

	preLoadTCPRoutes := []preLoadRouteDescriptor{
		&mockRoute{
			namespacedName: types.NamespacedName{
				Namespace: "tcp1-ns",
				Name:      "tcp1",
			},
			routeKind: TCPRouteKind,
		},
		&mockRoute{
			namespacedName: types.NamespacedName{
				Namespace: "tcp2-ns",
				Name:      "tcp2",
			},
			routeKind: TCPRouteKind,
		},
		&mockRoute{
			namespacedName: types.NamespacedName{
				Namespace: "tcp3-ns",
				Name:      "tcp3",
			},
			routeKind: TCPRouteKind,
		},
	}

	loadedTCPRoutes := make([]RouteDescriptor, 0)
	for _, preload := range preLoadTCPRoutes {
		r, _ := preload.loadAttachedRules(nil, nil)
		loadedTCPRoutes = append(loadedTCPRoutes, r)
	}

	allRouteLoaders := map[RouteKind]func(ctx context.Context, k8sClient client.Client, opts ...client.ListOption) ([]preLoadRouteDescriptor, error){
		HTTPRouteKind: func(ctx context.Context, k8sClient client.Client, opts ...client.ListOption) ([]preLoadRouteDescriptor, error) {
			return preLoadHTTPRoutes, nil
		},
		TCPRouteKind: func(ctx context.Context, k8sClient client.Client, opts ...client.ListOption) ([]preLoadRouteDescriptor, error) {
			return preLoadTCPRoutes, nil
		},
	}

	testCases := []struct {
		name                       string
		acceptedKinds              sets.Set[RouteKind]
		gatewayListeners           []gwv1.Listener
		expectedMap                map[int32][]RouteDescriptor
		expectedPreloadMap         map[int][]preLoadRouteDescriptor
		expectedPreMappedRoutes    []preLoadRouteDescriptor
		mapperRouteStatusUpdates   []RouteData
		expectedReconcileQueue     map[string]bool // generateRouteDataCacheKey -> succeeded
		expectError                bool
		expectedListenerConfigSize int                       // expected number of ports in ListenerConfig
		expectedListenerEntries    map[int32][]ListenerEntry // expected listener entries by port
	}{
		{
			name:                       "filter allows no routes",
			acceptedKinds:              make(sets.Set[RouteKind]),
			expectedPreMappedRoutes:    make([]preLoadRouteDescriptor, 0),
			expectedMap:                make(map[int32][]RouteDescriptor),
			expectedReconcileQueue:     map[string]bool{},
			gatewayListeners:           []gwv1.Listener{},
			expectedListenerConfigSize: 0,
			expectedListenerEntries:    map[int32][]ListenerEntry{},
		},
		{
			name:                    "filter only allows http route",
			acceptedKinds:           sets.New[RouteKind](HTTPRouteKind),
			expectedPreMappedRoutes: preLoadHTTPRoutes,
			expectedPreloadMap: map[int][]preLoadRouteDescriptor{
				80: preLoadHTTPRoutes,
			},
			expectedMap: map[int32][]RouteDescriptor{
				80: loadedHTTPRoutes,
			},
			expectedReconcileQueue: map[string]bool{
				"http1-http1-ns-HTTPRoute-gw-gw-ns--": true,
				"http2-http2-ns-HTTPRoute-gw-gw-ns--": true,
				"http3-http3-ns-HTTPRoute-gw-gw-ns--": true,
			},
			gatewayListeners: []gwv1.Listener{
				{
					Name:     "http-listener",
					Port:     80,
					Protocol: gwv1.HTTPProtocolType,
					Hostname: &testHostname,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(HTTPRouteKind)},
						},
					},
				},
			},
			expectedListenerConfigSize: 1,
			expectedListenerEntries: map[int32][]ListenerEntry{
				80: {
					{
						Port:        80,
						Protocol:    gwv1.HTTPProtocolType,
						SectionName: "http-listener",
						Hostname:    &testHostname,
						Source:      ListenerSourceGateway,
					},
				},
			},
		},
		{
			name:                    "filter only allows http route, multiple ports",
			acceptedKinds:           sets.New[RouteKind](HTTPRouteKind),
			expectedPreMappedRoutes: preLoadHTTPRoutes,
			expectedPreloadMap: map[int][]preLoadRouteDescriptor{
				80:  preLoadHTTPRoutes,
				443: preLoadHTTPRoutes,
			},
			expectedMap: map[int32][]RouteDescriptor{
				80:  loadedHTTPRoutes,
				443: loadedHTTPRoutes,
			},
			expectedReconcileQueue: map[string]bool{
				"http1-http1-ns-HTTPRoute-gw-gw-ns--": true,
				"http2-http2-ns-HTTPRoute-gw-gw-ns--": true,
				"http3-http3-ns-HTTPRoute-gw-gw-ns--": true,
			},
			gatewayListeners: []gwv1.Listener{
				{
					Name:     "http-listener",
					Port:     80,
					Protocol: gwv1.HTTPProtocolType,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(HTTPRouteKind)},
						},
					},
				},
				{
					Name:     "https-listener",
					Port:     443,
					Protocol: gwv1.HTTPSProtocolType,
					Hostname: &testHostname,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(HTTPRouteKind)},
						},
					},
				},
			},
			expectedListenerConfigSize: 2,
			expectedListenerEntries: map[int32][]ListenerEntry{
				80: {
					{
						Port:        80,
						Protocol:    gwv1.HTTPProtocolType,
						SectionName: "http-listener",
						Hostname:    nil,
						Source:      ListenerSourceGateway,
					},
				},
				443: {
					{
						Port:        443,
						Protocol:    gwv1.HTTPSProtocolType,
						SectionName: "https-listener",
						Hostname:    &testHostname,
						Source:      ListenerSourceGateway,
					},
				},
			},
		},
		{
			name:                    "filter only allows tcp route",
			acceptedKinds:           sets.New[RouteKind](TCPRouteKind),
			expectedPreMappedRoutes: preLoadTCPRoutes,
			expectedPreloadMap: map[int][]preLoadRouteDescriptor{
				80: preLoadTCPRoutes,
			},
			expectedMap: map[int32][]RouteDescriptor{
				80: loadedTCPRoutes,
			},
			expectedReconcileQueue: map[string]bool{
				"tcp1-tcp1-ns-TCPRoute-gw-gw-ns--": true,
				"tcp2-tcp2-ns-TCPRoute-gw-gw-ns--": true,
				"tcp3-tcp3-ns-TCPRoute-gw-gw-ns--": true,
			},
			gatewayListeners: []gwv1.Listener{
				{
					Name:     "tcp-listener",
					Port:     80,
					Protocol: gwv1.TCPProtocolType,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(TCPRouteKind)},
						},
					},
				},
			},
			expectedListenerConfigSize: 1,
			expectedListenerEntries: map[int32][]ListenerEntry{
				80: {
					{
						Port:        80,
						Protocol:    gwv1.TCPProtocolType,
						SectionName: "tcp-listener",
						Hostname:    nil,
						Source:      ListenerSourceGateway,
					},
				},
			},
		},
		{
			name:                    "filter allows both route kinds",
			acceptedKinds:           sets.New[RouteKind](TCPRouteKind, HTTPRouteKind),
			expectedPreMappedRoutes: append(preLoadHTTPRoutes, preLoadTCPRoutes...),
			expectedPreloadMap: map[int][]preLoadRouteDescriptor{
				80:  preLoadTCPRoutes,
				443: preLoadHTTPRoutes,
			},
			expectedMap: map[int32][]RouteDescriptor{
				80:  loadedTCPRoutes,
				443: loadedHTTPRoutes,
			},
			expectedReconcileQueue: map[string]bool{
				"http1-http1-ns-HTTPRoute-gw-gw-ns--": true,
				"http2-http2-ns-HTTPRoute-gw-gw-ns--": true,
				"http3-http3-ns-HTTPRoute-gw-gw-ns--": true,
				"tcp1-tcp1-ns-TCPRoute-gw-gw-ns--":    true,
				"tcp2-tcp2-ns-TCPRoute-gw-gw-ns--":    true,
				"tcp3-tcp3-ns-TCPRoute-gw-gw-ns--":    true,
			},
			gatewayListeners: []gwv1.Listener{
				{
					Name:     "tcp-listener",
					Port:     80,
					Protocol: gwv1.TCPProtocolType,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(TCPRouteKind)},
						},
					},
				},
				{
					Name:     "https-listener",
					Port:     443,
					Protocol: gwv1.HTTPSProtocolType,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(HTTPRouteKind)},
						},
					},
				},
			},
			expectedListenerConfigSize: 2,
			expectedListenerEntries: map[int32][]ListenerEntry{
				80: {
					{
						Port:        80,
						Protocol:    gwv1.TCPProtocolType,
						SectionName: "tcp-listener",
						Hostname:    nil,
						Source:      ListenerSourceGateway,
					},
				},
				443: {
					{
						Port:        443,
						Protocol:    gwv1.HTTPSProtocolType,
						SectionName: "https-listener",
						Hostname:    nil,
						Source:      ListenerSourceGateway,
					},
				},
			},
		},
		{
			name:                    "failed route should lead to only failed version status getting published",
			acceptedKinds:           sets.New[RouteKind](TCPRouteKind, HTTPRouteKind),
			expectedPreMappedRoutes: append(preLoadHTTPRoutes, preLoadTCPRoutes...),
			expectedPreloadMap: map[int][]preLoadRouteDescriptor{
				80:  preLoadTCPRoutes,
				443: preLoadHTTPRoutes,
			},
			expectedMap: map[int32][]RouteDescriptor{
				80:  loadedTCPRoutes,
				443: loadedHTTPRoutes,
			},
			expectedReconcileQueue: map[string]bool{
				"http1-http1-ns-HTTPRoute-gw-gw-ns--": true,
				"http2-http2-ns-HTTPRoute-gw-gw-ns--": true,
				"http3-http3-ns-HTTPRoute-gw-gw-ns--": true,
				"tcp1-tcp1-ns-TCPRoute-gw-gw-ns--":    true,
				"tcp2-tcp2-ns-TCPRoute-gw-gw-ns--":    false,
				"tcp3-tcp3-ns-TCPRoute-gw-gw-ns--":    true,
			},
			mapperRouteStatusUpdates: []RouteData{
				{
					RouteStatusInfo: RouteStatusInfo{
						Accepted: false,
					},
					RouteMetadata: RouteMetadata{
						RouteName:       "tcp2",
						RouteNamespace:  "tcp2-ns",
						RouteKind:       string(TCPRouteKind),
						RouteGeneration: 0,
					},
					ParentRef: gwv1.ParentReference{
						Name:      "gw",
						Namespace: &testNamespace,
					},
				},
			},
			gatewayListeners: []gwv1.Listener{
				{
					Name:     "tcp-listener",
					Port:     80,
					Protocol: gwv1.TCPProtocolType,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(TCPRouteKind)},
						},
					},
				},
				{
					Name:     "https-listener",
					Port:     443,
					Protocol: gwv1.HTTPSProtocolType,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(HTTPRouteKind)},
						},
					},
				},
			},
			expectedListenerConfigSize: 2,
			expectedListenerEntries: map[int32][]ListenerEntry{
				80: {
					{
						Port:        80,
						Protocol:    gwv1.TCPProtocolType,
						SectionName: "tcp-listener",
						Hostname:    nil,
						Source:      ListenerSourceGateway,
					},
				},
				443: {
					{
						Port:        443,
						Protocol:    gwv1.HTTPSProtocolType,
						SectionName: "https-listener",
						Hostname:    nil,
						Source:      ListenerSourceGateway,
					},
				},
			},
		},
		{
			name:                    "multiple failed routes",
			acceptedKinds:           sets.New[RouteKind](HTTPRouteKind),
			expectedPreMappedRoutes: preLoadHTTPRoutes,
			expectedPreloadMap: map[int][]preLoadRouteDescriptor{
				80: preLoadHTTPRoutes,
			},
			expectedMap: map[int32][]RouteDescriptor{
				80: loadedHTTPRoutes,
			},
			expectedReconcileQueue: map[string]bool{
				"http1-http1-ns-HTTPRoute-gw-gw-ns--": false,
				"http2-http2-ns-HTTPRoute-gw-gw-ns--": true,
				"http3-http3-ns-HTTPRoute-gw-gw-ns--": false,
			},
			mapperRouteStatusUpdates: []RouteData{
				{
					RouteStatusInfo: RouteStatusInfo{
						Accepted: false,
					},
					RouteMetadata: RouteMetadata{
						RouteName:       "http1",
						RouteNamespace:  "http1-ns",
						RouteKind:       string(HTTPRouteKind),
						RouteGeneration: 0,
					},
					ParentRef: gwv1.ParentReference{
						Name:      "gw",
						Namespace: &testNamespace,
					},
				},
				{
					RouteStatusInfo: RouteStatusInfo{
						Accepted: false,
					},
					RouteMetadata: RouteMetadata{
						RouteName:       "http3",
						RouteNamespace:  "http3-ns",
						RouteKind:       string(HTTPRouteKind),
						RouteGeneration: 0,
					},
					ParentRef: gwv1.ParentReference{
						Name:      "gw",
						Namespace: &testNamespace,
					},
				},
			},
			gatewayListeners: []gwv1.Listener{
				{
					Name:     "http-listener",
					Port:     80,
					Protocol: gwv1.HTTPProtocolType,
					AllowedRoutes: &gwv1.AllowedRoutes{
						Kinds: []gwv1.RouteGroupKind{
							{Kind: gwv1.Kind(HTTPRouteKind)},
						},
					},
				},
			},
			expectedListenerConfigSize: 1,
			expectedListenerEntries: map[int32][]ListenerEntry{
				80: {
					{
						Port:        80,
						Protocol:    gwv1.HTTPProtocolType,
						SectionName: "http-listener",
						Hostname:    nil,
						Source:      ListenerSourceGateway,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			routeReconciler := NewMockRouteReconciler()
			loader := loaderImpl{
				mapper: &mockMapper{
					t:                  t,
					expectedRoutes:     tc.expectedPreMappedRoutes,
					mapToReturn:        tc.expectedPreloadMap,
					routeStatusUpdates: tc.mapperRouteStatusUpdates,
				},
				allRouteLoaders: allRouteLoaders,
				logger:          logr.Discard(),
				routeSubmitter:  routeReconciler,
			}

			filter := &routeFilterImpl{acceptedKinds: tc.acceptedKinds}
			result, err := loader.LoadRoutesForGateway(context.Background(), gwv1.Gateway{
				ObjectMeta: v1.ObjectMeta{
					Name:      "gw",
					Namespace: "gw-ns",
				},
				Spec: gwv1.GatewaySpec{
					Listeners: tc.gatewayListeners,
				},
			}, filter, gateway_constants.ALBGatewayController)
			if tc.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.expectedMap, result.Routes)
			assert.Equal(t, len(tc.expectedReconcileQueue), len(routeReconciler.Enqueued))

			for _, actual := range routeReconciler.Enqueued {
				ak := generateRouteDataCacheKey(actual.RouteData)

				v, ok := tc.expectedReconcileQueue[ak]
				assert.True(t, ok)
				assert.Equal(t, v, actual.RouteData.RouteStatusInfo.Accepted, ak)
			}

			// Verify ListenerConfig is populated correctly (Requirement 1.2)
			assert.NotNil(t, result.ListenerConfig, "ListenerConfig should not be nil")
			assert.Equal(t, tc.expectedListenerConfigSize, len(result.ListenerConfig.GetAllPorts()), "ListenerConfig should have expected number of ports")

			// Verify each listener entry matches expected values
			for port, expectedEntries := range tc.expectedListenerEntries {
				actualEntries := result.ListenerConfig.GetEntriesForPort(port)
				assert.Equal(t, len(expectedEntries), len(actualEntries), "Port %d should have expected number of entries", port)

				for i, expected := range expectedEntries {
					if i < len(actualEntries) {
						actual := actualEntries[i]
						assert.Equal(t, expected.Port, actual.Port, "Port should match")
						assert.Equal(t, expected.Protocol, actual.Protocol, "Protocol should match for port %d", port)
						assert.Equal(t, expected.SectionName, actual.SectionName, "SectionName should match for port %d", port)
						assert.Equal(t, expected.Source, actual.Source, "Source should be ListenerSourceGateway for port %d", port)

						// Handle hostname comparison (can be nil)
						if expected.Hostname == nil {
							assert.Nil(t, actual.Hostname, "Hostname should be nil for port %d", port)
						} else {
							assert.NotNil(t, actual.Hostname, "Hostname should not be nil for port %d", port)
							assert.Equal(t, *expected.Hostname, *actual.Hostname, "Hostname should match for port %d", port)
						}
					}
				}
			}

		})
	}
}
