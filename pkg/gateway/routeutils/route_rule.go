package routeutils

import (
	elbv2gw "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/backendutils"
)

// RouteRule is a type agnostic representation of Routing Rules.
type RouteRule interface {
	GetRawRouteRule() interface{}
	GetBackends() []backendutils.Backend
	GetListenerRuleConfig() *elbv2gw.ListenerRuleConfiguration
}
