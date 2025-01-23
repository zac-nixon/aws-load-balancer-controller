package common

import (
	"context"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type ControllerTCPRoute struct {
	route       gwalpha2.TCPRoute
	serviceRefs []*ServiceRef
}

func (c *ControllerTCPRoute) GetServiceRefs() []*ServiceRef {
	return c.serviceRefs
}

func (c *ControllerTCPRoute) GetProtocol() NetworkingProtocol {
	return NetworkingProtocolTCP
}

// ConvertKubeTCPRoutes converts Kubernetes Gateway API objects into a format the AWS LBC can handle. Also does light validation.
func ConvertKubeTCPRoutes(context context.Context, client client.Client, routes []gwalpha2.TCPRoute) ([]ControllerRoute, error) {
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

		controllerRoutes = append(controllerRoutes, &ControllerTCPRoute{
			route:       route,
			serviceRefs: serviceRefs,
		})
	}

	return controllerRoutes, nil
}

// ListTCPRoutes list all tcp routes in the cluster
func ListTCPRoutes(context context.Context, client client.Client) (*gwalpha2.TCPRouteList, error) {
	routeList := &gwalpha2.TCPRouteList{}
	err := client.List(context, routeList)
	return routeList, err
}
