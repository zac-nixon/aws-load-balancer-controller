package nlb

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func ListTCPRoutes(context context.Context, client client.Client) (*gwalpha2.TCPRouteList, error) {
	routeList := &gwalpha2.TCPRouteList{}
	err := client.List(context, routeList)
	return routeList, err
}
