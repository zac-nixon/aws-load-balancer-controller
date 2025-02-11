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
func ConvertKubeTCPRoutes(context context.Context, client client.Client, route gwalpha2.TCPRoute) (ControllerRoute, error) {
	// Is this the right place for this?
	ruleNameSet := sets.Set[string]{}
	serviceRefs := make([]*ServiceRef, 0)
	for _, rule := range route.Spec.Rules {
		if rule.Name != nil {
			ruleName := string(*rule.Name)
			if ruleNameSet.Has(ruleName) {
				return nil, errors.Errorf("Duplicate rule name %s found in route %+v", *rule.Name, k8s.NamespacedName(&route))
			}
			ruleNameSet.Insert(ruleName)
		}

		if len(rule.BackendRefs) > 1 {
			return nil, errors.Errorf("UDP routes only support 1 backend ref.")
		}

		loadedRefs, err := loadServices(context, client, rule.BackendRefs, route.Namespace)
		if err != nil {
			return nil, errors.Wrap(err, "Unable to load services for route")
		}
		serviceRefs = append(serviceRefs, loadedRefs...)
	}

	return &ControllerTCPRoute{
		route:       route,
		serviceRefs: serviceRefs,
	}, nil
}

// ListTCPRoutes list all tcp routes in the cluster
func ListTCPRoutes(context context.Context, client client.Client) (*gwalpha2.TCPRouteList, error) {
	routeList := &gwalpha2.TCPRouteList{}
	err := client.List(context, routeList)
	return routeList, err
}
