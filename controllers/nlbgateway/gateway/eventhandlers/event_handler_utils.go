package eventhandlers

import (
	"context"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func GetImpactedGatewaysFromParentRefs(ctx context.Context, client client.Client, parentRefs []gwv1.ParentReference, resourceNamespace string) ([]types.NamespacedName, error) {
	processedSet := sets.Set[types.NamespacedName]{}
	result := make([]types.NamespacedName, 0)
	for _, parent := range parentRefs {
		// TODO -- This logic is duplicated _alot_
		namespace := resourceNamespace
		if parent.Namespace != nil {
			namespace = string(*parent.Namespace)
		}
		nsn := types.NamespacedName{
			Namespace: namespace,
			Name:      string(parent.Name),
		}

		if processedSet.Has(nsn) {
			continue
		}

		processedSet.Insert(nsn)
		result = append(result, nsn)

		gw := &gwv1.Gateway{}
		err := client.Get(ctx, nsn, gw)
		if err != nil {
			return nil, errors.Errorf("Unable to retrieve parent gateway ref for gateway %v", nsn)
		}
	}
	return result, nil
}
