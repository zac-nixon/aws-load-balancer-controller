package attachmentutils

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/namespaceutils"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/routeconstants"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// DoesResourceAttachToGateway checks if a target resource (route, listenerset) wishes to connect to the gateway.
// this is done by following Gateway API conventions on the parent reference found within target resource.
func DoesResourceAttachToGateway(parentRef gwv1.ParentReference, resourceNamespace string, gw gwv1.Gateway) bool {
	// Default for kind is Gateway.
	if parentRef.Kind != nil && *parentRef.Kind != routeconstants.GatewayKind {
		return false
	}

	var namespaceToCompare string

	if parentRef.Namespace != nil {
		namespaceToCompare = string(*parentRef.Namespace)
	} else {
		namespaceToCompare = resourceNamespace
	}

	nameCheck := string(parentRef.Name) == gw.Name
	nsCheck := gw.Namespace == namespaceToCompare
	return nameCheck && nsCheck
}

func DoesResourceAllowNamespace(ctx context.Context, fromNamespaces gwv1.FromNamespaces, labelSelector *metav1.LabelSelector, nsSelector namespaceutils.NamespaceSelector, resourceNamespace string, gw gwv1.Gateway) (bool, error) {
	switch fromNamespaces {
	case gwv1.NamespacesFromNone:
		return false, nil
	case gwv1.NamespacesFromSame:
		return gw.Namespace == resourceNamespace, nil
	case gwv1.NamespacesFromAll:
		return true, nil
	case gwv1.NamespacesFromSelector:
		if labelSelector == nil {
			return false, nil
		}
		// This should be executed off the client-go cache, hence we do not need to perform local caching.
		namespaces, err := nsSelector.GetNamespacesFromSelector(ctx, labelSelector)
		if err != nil {
			return false, err
		}

		if !namespaces.Has(resourceNamespace) {
			return false, nil
		}
		return true, nil
	default:
		// Unclear what to do in this case, we'll just filter out this route.
		return false, nil
	}
}
