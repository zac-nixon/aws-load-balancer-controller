package routeutils

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gwxalpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
)

// ListXListenerSets loads all XListenerSets from the cluster following routeutils pattern
func ListXListenerSets(ctx context.Context, k8sClient client.Client, opts ...client.ListOption) ([]gwxalpha1.XListenerSet, error) {
	listenerSetList := &gwxalpha1.XListenerSetList{}
	err := k8sClient.List(ctx, listenerSetList, opts...)
	if err != nil {
		return nil, err
	}

	return listenerSetList.Items, nil
}
