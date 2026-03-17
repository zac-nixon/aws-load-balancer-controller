package routereconciler

import gwv1 "sigs.k8s.io/gateway-api/apis/v1"

// RouteData
// RouteStatusInfo: contains status condition info
// RouteMetadata: contains route metadata: name, namespace, kind and generation
// ParentRef: contains gateway parent reference information
type RouteData struct {
	RouteStatusInfo RouteStatusInfo
	RouteMetadata   RouteMetadata
	ParentRef       gwv1.ParentReference
}

type RouteStatusInfo struct {
	Accepted     bool
	ResolvedRefs bool
	Reason       string
	Message      string
}

type RouteMetadata struct {
	RouteName       string
	RouteNamespace  string
	RouteKind       string
	RouteGeneration int64
}
