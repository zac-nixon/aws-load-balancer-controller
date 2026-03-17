package loadererrors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
)

// GenerateInvalidMessageWithRouteDetails utility to generate a helpful error message that contains route identifier information.
func GenerateInvalidMessageWithRouteDetails(initialMessage string, routeKind constants.RouteKind, routeIdentifier types.NamespacedName) string {
	return fmt.Sprintf("%s. Invalid data can be found in route (%s, %s:%s)", initialMessage, routeKind, routeIdentifier.Namespace, routeIdentifier.Name)
}
