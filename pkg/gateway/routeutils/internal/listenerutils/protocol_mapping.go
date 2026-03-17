package listenerutils

import (
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// DefaultProtocolToRouteKindMap Default protocol map used to infer accepted route kinds when a listener doesn't specify the `allowedRoutes` field.
var DefaultProtocolToRouteKindMap = map[gwv1.ProtocolType][]constants.RouteKind{
	gwv1.TCPProtocolType:   {constants.TCPRouteKind},
	gwv1.UDPProtocolType:   {constants.UDPRouteKind},
	gwv1.TLSProtocolType:   {constants.TLSRouteKind, constants.TCPRouteKind},
	gwv1.HTTPProtocolType:  {constants.HTTPRouteKind},
	gwv1.HTTPSProtocolType: {constants.HTTPRouteKind, constants.GRPCRouteKind},
}
