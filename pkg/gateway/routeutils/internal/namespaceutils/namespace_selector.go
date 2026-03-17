package namespaceutils

import (
	"context"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/loadererrors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// NamespaceSelector is an internal utility
// that is responsible for transforming a label selector into the all relevant namespaces
// that match the selector criteria.
type NamespaceSelector interface {
	GetNamespacesFromSelector(context context.Context, selector *metav1.LabelSelector) (sets.Set[string], error)
}

var _ NamespaceSelector = &namespaceSelectorImpl{}

type namespaceSelectorImpl struct {
	k8sClient client.Client
}

func NewNamespaceSelector(k8sClient client.Client) NamespaceSelector {
	return &namespaceSelectorImpl{
		k8sClient: k8sClient,
	}
}

// getNamespacesFromSelector queries the Kubernetes API for all namespaces that match a selector.
func (n *namespaceSelectorImpl) GetNamespacesFromSelector(context context.Context, selector *metav1.LabelSelector) (sets.Set[string], error) {
	namespaceList := v1.NamespaceList{}

	convertedSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, loadererrors.WrapError(errors.Wrapf(err, "Unable to parse selector %s", selector), gwv1.GatewayReasonListenersNotValid, gwv1.RouteReasonUnsupportedValue, nil, nil)
	}

	err = n.k8sClient.List(context, &namespaceList, client.MatchingLabelsSelector{Selector: convertedSelector})
	if err != nil {
		return nil, err
	}

	namespaces := sets.New[string]()

	for _, ns := range namespaceList.Items {
		namespaces.Insert(ns.Name)
	}

	return namespaces, nil
}
