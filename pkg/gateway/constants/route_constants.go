package constants

type RouteKind string

// Route Kinds
const (
	TCPRouteKind  RouteKind = "TCPRoute"
	UDPRouteKind  RouteKind = "UDPRoute"
	TLSRouteKind  RouteKind = "TLSRoute"
	HTTPRouteKind RouteKind = "HTTPRoute"
	GRPCRouteKind RouteKind = "GRPCRoute"
)
