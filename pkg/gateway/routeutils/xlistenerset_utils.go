package routeutils

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwxalpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
)

// GetAttachedXListenerSets returns XListenerSets that reference the given Gateway, sorted by precedence
func GetAttachedXListenerSets(ctx context.Context, k8sClient client.Client, gw *gwv1.Gateway) ([]gwxalpha1.XListenerSet, error) {
	listenerSets, err := ListXListenerSets(ctx, k8sClient)
	if err != nil {
		return nil, err
	}

	var attached []gwxalpha1.XListenerSet
	for _, ls := range listenerSets {
		if isXListenerSetAttachedToGateway(&ls, gw) {
			attached = append(attached, ls)
		}
	}

	// Sort by creation time (oldest first), then by namespace/name for precedence
	sort.Slice(attached, func(i, j int) bool {
		if !attached[i].CreationTimestamp.Equal(&attached[j].CreationTimestamp) {
			return attached[i].CreationTimestamp.Before(&attached[j].CreationTimestamp)
		}
		return types.NamespacedName{
			Namespace: attached[i].Namespace,
			Name:      attached[i].Name,
		}.String() < types.NamespacedName{
			Namespace: attached[j].Namespace,
			Name:      attached[j].Name,
		}.String()
	})

	return attached, nil
}

// isXListenerSetAttachedToGateway checks if XListenerSet references the Gateway
func isXListenerSetAttachedToGateway(ls *gwxalpha1.XListenerSet, gw *gwv1.Gateway) bool {
	parentRef := ls.Spec.ParentRef

	if parentRef.Name != gwv1.ObjectName(gw.Name) {
		return false
	}

	// If parentRef.Namespace is nil, it defaults to the XListenerSet's namespace
	parentNamespace := ls.Namespace
	if parentRef.Namespace != nil {
		parentNamespace = string(*parentRef.Namespace)
	}
	if parentNamespace != gw.Namespace {
		return false
	}

	group := "gateway.networking.k8s.io"
	if parentRef.Group != nil {
		group = string(*parentRef.Group)
	}

	kind := "Gateway"
	if parentRef.Kind != nil {
		kind = string(*parentRef.Kind)
	}

	return group == "gateway.networking.k8s.io" && kind == "Gateway"
}

// MergeListenersWithXListenerSets merges Gateway listeners with XListenerSet listeners per GEP-1713
func MergeListenersWithXListenerSets(gw *gwv1.Gateway, listenerSets []gwxalpha1.XListenerSet) []gwv1.Listener {
	var merged []gwv1.Listener

	// Gateway listeners first (highest precedence)
	merged = append(merged, gw.Spec.Listeners...)

	// XListenerSet listeners in precedence order
	for _, ls := range listenerSets {
		for _, entry := range ls.Spec.Listeners {
			listener := gwv1.Listener{
				Name:          gwv1.SectionName(entry.Name),
				Hostname:      (*gwv1.Hostname)(entry.Hostname),
				Port:          gwv1.PortNumber(entry.Port),
				Protocol:      entry.Protocol,
				TLS:           (*gwv1.GatewayTLSConfig)(entry.TLS),
				AllowedRoutes: (*gwv1.AllowedRoutes)(entry.AllowedRoutes),
			}
			merged = append(merged, listener)
		}
	}

	return merged
}
