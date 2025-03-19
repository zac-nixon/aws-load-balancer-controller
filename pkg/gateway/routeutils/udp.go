package routeutils

import (
	"context"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

var _ RouteRule = &convertedUDPRouteRule{}

type convertedUDPRouteRule struct {
	rule     *gwalpha2.UDPRouteRule
	backends []Backend
}

func convertUDPRouteRule(rule *gwalpha2.UDPRouteRule, backends []Backend) RouteRule {
	return &convertedUDPRouteRule{
		rule:     rule,
		backends: backends,
	}
}

func (t *convertedUDPRouteRule) GetSectionName() *gwv1.SectionName {
	return t.rule.Name
}

func (t *convertedUDPRouteRule) GetBackends() []Backend {
	return t.backends
}

func (t *convertedUDPRouteRule) GetHostnames() []string {
	// Not supported for UDP route rules
	return []string{}
}

type udpRouteDescription struct {
	route *gwalpha2.UDPRoute
	rules []RouteRule
}

func (udpRoute *udpRouteDescription) GetAttachedRules() []RouteRule {
	return udpRoute.rules
}

func (udpRoute *udpRouteDescription) loadAttachedRules(ctx context.Context, k8sClient client.Client) (RouteDescriptor, error) {
	convertedRules := make([]RouteRule, 0)
	for _, rule := range udpRoute.route.Spec.Rules {
		convertedBackends := make([]Backend, 0)

		for _, backend := range rule.BackendRefs {
			convertedBackend, err := commonBackendLoader(ctx, k8sClient, backend, udpRoute.GetRouteNamespacedName(), udpRoute.GetRouteKind())
			if err != nil {
				return nil, err
			}
			convertedBackends = append(convertedBackends, *convertedBackend)
		}

		convertedRules = append(convertedRules, convertUDPRouteRule(&rule, convertedBackends))
	}

	udpRoute.rules = convertedRules
	return udpRoute, nil
}

func (udpRoute *udpRouteDescription) GetParentRefs() []gwv1.ParentReference {
	return udpRoute.route.Spec.ParentRefs
}

func (udpRoute *udpRouteDescription) GetRouteKind() string {
	return UDPRouteKind
}

func convertUDPRoute(r gwalpha2.UDPRoute) *udpRouteDescription {
	return &udpRouteDescription{route: &r}
}

func (udpRoute *udpRouteDescription) GetRouteNamespacedName() types.NamespacedName {
	return k8s.NamespacedName(udpRoute.route)
}

func (udpRoute *udpRouteDescription) GetRawRoute() interface{} {
	return udpRoute.route
}

var _ RouteDescriptor = &udpRouteDescription{}

func ListUDPRoutes(context context.Context, k8sClient client.Client) ([]preLoadRouteDescriptor, error) {
	routeList := &gwalpha2.UDPRouteList{}
	err := k8sClient.List(context, routeList)
	if err != nil {
		return nil, err
	}

	result := make([]preLoadRouteDescriptor, 0)

	for _, item := range routeList.Items {
		result = append(result, convertUDPRoute(item))
	}

	return result, err
}
