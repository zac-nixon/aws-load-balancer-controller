package routeutils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/routedescriptor"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListL4Routes retrieves all Layer 4 routes (TCP, UDP, TLS) from the cluster.
func ListL4Routes(ctx context.Context, k8sClient client.Client) ([]backendutils.preLoadRouteDescriptor, error) {
	l4Routes := make([]backendutils.preLoadRouteDescriptor, 0)
	var failedRoutes []constants.RouteKind
	tcpRoutes, err := backendutils.ListTCPRoutes(ctx, k8sClient)
	if err != nil {
		failedRoutes = append(failedRoutes, constants.TCPRouteKind)
	}
	l4Routes = append(l4Routes, tcpRoutes...)
	udpRoutes, err := backendutils.ListUDPRoutes(ctx, k8sClient)
	if err != nil {
		failedRoutes = append(failedRoutes, constants.UDPRouteKind)
	}
	l4Routes = append(l4Routes, udpRoutes...)
	tlsRoutes, err := backendutils.ListTLSRoutes(ctx, k8sClient)
	if err != nil {
		failedRoutes = append(failedRoutes, constants.TLSRouteKind)
	}
	l4Routes = append(l4Routes, tlsRoutes...)
	if len(failedRoutes) > 0 {
		err = fmt.Errorf("failed to list L4 routes, %v", failedRoutes)
	}
	return l4Routes, err
}

// ListL7Routes retrieves all Layer 7 routes (HTTP, gRPC) from the cluster.
func ListL7Routes(ctx context.Context, k8sClient client.Client) ([]backendutils.preLoadRouteDescriptor, error) {
	l7Routes := make([]backendutils.preLoadRouteDescriptor, 0)
	var failedRoutes []constants.RouteKind
	httpRoutes, err := backendutils.ListHTTPRoutes(ctx, k8sClient)
	if err != nil {
		failedRoutes = append(failedRoutes, constants.HTTPRouteKind)
	}
	l7Routes = append(l7Routes, httpRoutes...)
	grpcRoutes, err := backendutils.ListGRPCRoutes(ctx, k8sClient)
	if err != nil {
		failedRoutes = append(failedRoutes, constants.GRPCRouteKind)
	}
	l7Routes = append(l7Routes, grpcRoutes...)
	if len(failedRoutes) > 0 {
		err = fmt.Errorf("failed to list L7 routes, %v", failedRoutes)
	}
	return l7Routes, err
}

// FilterRoutesBySvc filters a slice of routes based on service reference.
// Returns a new slice containing only routes that reference the specified service.
func FilterRoutesBySvc(routes []backendutils.preLoadRouteDescriptor, svc *corev1.Service) []backendutils.preLoadRouteDescriptor {
	if svc == nil || len(routes) == 0 {
		return []backendutils.preLoadRouteDescriptor{}
	}
	filteredRoutes := make([]backendutils.preLoadRouteDescriptor, 0, len(routes))
	svcID := types.NamespacedName{
		Namespace: svc.Namespace,
		Name:      svc.Name,
	}
	for _, route := range routes {
		if isServiceReferredByRoute(route, svcID) {
			filteredRoutes = append(filteredRoutes, route)
		}
	}
	return filteredRoutes
}

// isServiceReferredByRoute checks if a route references a specific service.
// Assuming we are only supporting services as backendRefs on Routes
func isServiceReferredByRoute(route backendutils.preLoadRouteDescriptor, svcID types.NamespacedName) bool {
	for _, backendRef := range route.GetBackendRefs() {
		if backendRef.Kind != nil && *backendRef.Kind != "Service" {
			continue
		}
		namespace := route.GetRouteNamespacedName().Namespace
		if backendRef.Namespace != nil {
			namespace = string(*backendRef.Namespace)
		}

		if string(backendRef.Name) == svcID.Name && namespace == svcID.Namespace {
			return true
		}
	}
	return false
}

func generateInvalidMessageWithRouteDetails(initialMessage string, routeKind constants.RouteKind, routeIdentifier types.NamespacedName) string {
	return fmt.Sprintf("%s. Invalid data can be found in route (%s, %s:%s)", initialMessage, routeKind, routeIdentifier.Namespace, routeIdentifier.Name)
}
