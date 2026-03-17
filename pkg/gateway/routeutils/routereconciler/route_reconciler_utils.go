package routereconciler

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// This file contains utils used for gateway api route reconciler

type RouteReconciler interface {
	Run()
	Enqueue(routeData RouteData)
}

type RouteReconcilerSubmitter interface {
	Enqueue(routeData RouteData)
}

// constants

const (
	RouteStatusInfoAcceptedMessage                   = "Route is accepted by Gateway"
	RouteStatusInfoRejectedMessageNoMatchingHostname = "Listener does not allow route attachment, no matching hostname"
	RouteStatusInfoRejectedMessageNamespaceNotMatch  = "Listener does not allow route attachment, namespace does not match between listener and route"
	RouteStatusInfoRejectedMessageKindNotMatch       = "Listener does not allow route attachment, kind does not match between listener and route"
	RouteStatusInfoRejectedParentRefNotExist         = "ParentRefDoesNotExist"
	RouteStatusInfoRejectedMessageParentNotMatch     = "Route parentRef does not match listener"
)

func GenerateRouteData(accepted bool, resolvedRefs bool, reason string, message string, routeNamespaceName types.NamespacedName, routeKind constants.RouteKind, routeGeneration int64, gw gwv1.Gateway, port *gwv1.PortNumber, sectionName *gwv1.SectionName) RouteData {
	namespace := gwv1.Namespace(gw.Namespace)
	group := gwv1.Group(gw.GroupVersionKind().Group)
	kind := gwv1.Kind(gw.GroupVersionKind().Kind)
	return RouteData{
		RouteStatusInfo: RouteStatusInfo{
			Accepted:     accepted,
			ResolvedRefs: resolvedRefs,
			Reason:       reason,
			Message:      message,
		},
		RouteMetadata: RouteMetadata{
			RouteName:       routeNamespaceName.Name,
			RouteNamespace:  routeNamespaceName.Namespace,
			RouteKind:       string(routeKind),
			RouteGeneration: routeGeneration,
		},
		ParentRef: gwv1.ParentReference{
			Group:       &group,
			Kind:        &kind,
			Name:        gwv1.ObjectName(gw.Name),
			Namespace:   &namespace,
			Port:        port,
			SectionName: sectionName,
		},
	}
}
