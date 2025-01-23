package common

import (
	"context"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type ControllerUDPRoute struct {
	route       gwalpha2.UDPRoute
	serviceRefs []*ServiceRef
}

func (c *ControllerUDPRoute) GetServiceRefs() []*ServiceRef {
	return c.serviceRefs
}

func (c *ControllerUDPRoute) GetProtocol() NetworkingProtocol {
	return NetworkingProtocolUDP
}

// ConvertKubeUDPRoutes converts Kubernetes Gateway API objects into a format the AWS LBC can handle. Also does light validation.
func ConvertKubeUDPRoutes(context context.Context, client client.Client, routes []gwalpha2.UDPRoute) ([]ControllerRoute, error) {
	controllerRoutes := make([]ControllerRoute, 0, len(routes))
	for _, route := range routes {

		// Is this the right place for this?
		ruleNameSet := sets.Set[string]{}
		serviceRefs := make([]*ServiceRef, 0)

		for _, rule := range route.Spec.Rules {
			if rule.Name != nil {
				if ruleNameSet.Has(string(*rule.Name)) {
					return nil, errors.Errorf("Duplicate rule name %s found in route %+v", *rule.Name, k8s.NamespacedName(&route))
				}
			}

			loadedRefs, err := loadServices(context, client, rule.BackendRefs, route.Namespace)
			if err != nil {
				return nil, errors.Wrap(err, "Unable to load services for route")
			}
			serviceRefs = append(serviceRefs, loadedRefs...)
		}

		controllerRoutes = append(controllerRoutes, &ControllerUDPRoute{
			route:       route,
			serviceRefs: serviceRefs,
		})
	}

	return controllerRoutes, nil
}

func ListUDPRoutes(context context.Context, client client.Client) (*gwalpha2.UDPRouteList, error) {
	routeList := &gwalpha2.UDPRouteList{}
	err := client.List(context, routeList)
	return routeList, err
}
