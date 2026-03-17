package routeutils

import (
	"context"

	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/routedescriptor"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	targetGroupNameBackend string = "TargetGroupName"
)

// RouteKind to Route Loader. These functions will pull data directly from the kube api or local cache.
var allRoutes = map[constants.RouteKind]func(context context.Context, client client.Client, opts ...client.ListOption) ([]backendutils.preLoadRouteDescriptor, error){
	constants.TCPRouteKind:  backendutils.ListTCPRoutes,
	constants.UDPRouteKind:  backendutils.ListUDPRoutes,
	constants.TLSRouteKind:  backendutils.ListTLSRoutes,
	constants.HTTPRouteKind: backendutils.ListHTTPRoutes,
	constants.GRPCRouteKind: backendutils.ListGRPCRoutes,
}
