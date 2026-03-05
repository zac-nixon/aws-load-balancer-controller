package listenerset

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ ListenerSetLoader = &listenerSetLoaderImpl{}

// listenerSetLoaderImpl implements ListenerSetLoader.
type listenerSetLoaderImpl struct {
	k8sClient client.Client
	logger    logr.Logger
}

// NewListenerSetLoader creates a new ListenerSetLoader.
func NewListenerSetLoader(k8sClient client.Client, logger logr.Logger) ListenerSetLoader {
	return &listenerSetLoaderImpl{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

// LoadListenerSetsForGateway discovers and validates ListenerSets for a Gateway.
func (l *listenerSetLoaderImpl) LoadListenerSetsForGateway(
	ctx context.Context, gw gwv1.Gateway,
) (*ListenerSetLoadResult, error) {
	result := &ListenerSetLoadResult{}

	// Step 1: List all ListenerSets that reference this Gateway.
	allSets, err := l.listAllListenerSetsReferencingGateway(ctx, gw)
	if err != nil {
		return nil, fmt.Errorf("listing ListenerSets for Gateway %s/%s: %w", gw.Namespace, gw.Name, err)
	}

	if len(allSets) == 0 {
		return result, nil
	}

	// Step 2: Check if Gateway allows ListenerSets at all.
	if !gatewayAllowsListenerSets(gw) {
		for i := range allSets {
			result.RejectedSets = append(result.RejectedSets, RejectedListenerSet{
				ListenerSet: &allSets[i],
				Reason:      "NotAllowed",
				Message:     "Gateway does not allow ListenerSets",
			})
		}
		return result, nil
	}

	// Step 3: Filter by namespace policy.
	fromPolicy := *gw.Spec.AllowedListeners.Namespaces.From
	for i := range allSets {
		ls := &allSets[i]
		accepted, err := l.isListenerSetAccepted(ctx, gw, ls, fromPolicy)
		if err != nil {
			return nil, fmt.Errorf("evaluating ListenerSet %s/%s: %w", ls.Namespace, ls.Name, err)
		}

		if accepted {
			result.AcceptedSets = append(result.AcceptedSets, ListenerSetData{
				ListenerSet: ls,
				Listeners:   ls.Spec.Listeners,
			})
		} else {
			result.RejectedSets = append(result.RejectedSets, RejectedListenerSet{
				ListenerSet: ls,
				Reason:      "NotAllowed",
				Message:     fmt.Sprintf("ListenerSet namespace %q not allowed by Gateway policy", ls.Namespace),
			})
		}
	}

	return result, nil
}

// gatewayAllowsListenerSets returns true if the Gateway's allowedListeners
// policy permits any ListenerSets. Returns false when allowedListeners is nil,
// namespaces is nil, from is nil, or from is None.
func gatewayAllowsListenerSets(gw gwv1.Gateway) bool {
	if gw.Spec.AllowedListeners == nil {
		return false
	}
	if gw.Spec.AllowedListeners.Namespaces == nil {
		return false
	}
	if gw.Spec.AllowedListeners.Namespaces.From == nil {
		return false
	}
	return *gw.Spec.AllowedListeners.Namespaces.From != gwv1.NamespacesFromNone
}

// isListenerSetAccepted evaluates whether a ListenerSet is accepted by the
// Gateway's namespace policy.
func (l *listenerSetLoaderImpl) isListenerSetAccepted(
	ctx context.Context,
	gw gwv1.Gateway,
	ls *gwv1.ListenerSet,
	fromPolicy gwv1.FromNamespaces,
) (bool, error) {
	switch fromPolicy {
	case gwv1.NamespacesFromSame:
		return ls.Namespace == gw.Namespace, nil
	case gwv1.NamespacesFromAll:
		return true, nil
	case gwv1.NamespacesFromSelector:
		return l.matchesNamespaceSelector(ctx, ls.Namespace, gw.Spec.AllowedListeners.Namespaces.Selector)
	default:
		return false, nil
	}
}

// matchesNamespaceSelector fetches the namespace and checks if its labels
// match the given label selector.
func (l *listenerSetLoaderImpl) matchesNamespaceSelector(
	ctx context.Context,
	namespaceName string,
	selector *metav1.LabelSelector,
) (bool, error) {
	if selector == nil {
		return false, nil
	}

	ns := &corev1.Namespace{}
	if err := l.k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, ns); err != nil {
		return false, fmt.Errorf("fetching namespace %q: %w", namespaceName, err)
	}

	parsedSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("parsing label selector: %w", err)
	}

	return parsedSelector.Matches(labels.Set(ns.Labels)), nil
}

// listAllListenerSetsReferencingGateway lists all ListenerSets whose parentRef
// matches the given Gateway by name, namespace, group, and kind.
func (l *listenerSetLoaderImpl) listAllListenerSetsReferencingGateway(
	ctx context.Context, gw gwv1.Gateway,
) ([]gwv1.ListenerSet, error) {
	listenerSetList := &gwv1.ListenerSetList{}
	if err := l.k8sClient.List(ctx, listenerSetList); err != nil {
		return nil, err
	}

	var matched []gwv1.ListenerSet
	for _, ls := range listenerSetList.Items {
		if parentRefMatchesGateway(ls.Spec.ParentRef, ls.Namespace, gw) {
			matched = append(matched, ls)
		}
	}
	return matched, nil
}

// parentRefMatchesGateway checks whether a ListenerSet's parentRef references
// the given Gateway, accounting for defaulted group, kind, and namespace.
func parentRefMatchesGateway(ref gwv1.ParentGatewayReference, lsNamespace string, gw gwv1.Gateway) bool {
	// Check name.
	if string(ref.Name) != gw.Name {
		return false
	}

	// Check namespace: defaults to the ListenerSet's namespace if not set.
	refNamespace := lsNamespace
	if ref.Namespace != nil {
		refNamespace = string(*ref.Namespace)
	}
	if refNamespace != gw.Namespace {
		return false
	}

	// Check group: defaults to "gateway.networking.k8s.io".
	refGroup := gwv1.Group("gateway.networking.k8s.io")
	if ref.Group != nil {
		refGroup = *ref.Group
	}
	if string(refGroup) != "gateway.networking.k8s.io" {
		return false
	}

	// Check kind: defaults to "Gateway".
	refKind := gwv1.Kind("Gateway")
	if ref.Kind != nil {
		refKind = *ref.Kind
	}
	if string(refKind) != "Gateway" {
		return false
	}

	return true
}
