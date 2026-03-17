package listenerutils

import (
	"context"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/attachmentutils"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/routeutils/internal/namespaceutils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type handshakeState string

const (
	// acceptedHandshakeState - both resource and gateway agree to attachment
	acceptedHandshakeState handshakeState = "accepted"
	// gatewayRejectedHandshakeState - the gateway configuration rejects this configuration
	gatewayRejectedHandshakeState handshakeState = "rejected"
	// irrelevantResourceHandshakeState - the resource has no relation to the gateway
	irrelevantResourceHandshakeState handshakeState = "irrelevant"
)

type listenerSetLoader interface {
	retrieveListenersFromListenerSets(ctx context.Context, gateway gwv1.Gateway) ([]listenerSource, []*gwv1.ListenerSet, error)
}

type listenerSetLoaderImpl struct {
	k8sClient         client.Client
	namespaceSelector namespaceutils.NamespaceSelector
	logger            logr.Logger
}

func newListenerSetLoader(k8sClient client.Client, logger logr.Logger) listenerSetLoader {
	return &listenerSetLoaderImpl{
		k8sClient:         k8sClient,
		namespaceSelector: namespaceutils.NewNamespaceSelector(k8sClient),
		logger:            logger.WithName("listener-set-loader"),
	}
}

func (l *listenerSetLoaderImpl) retrieveListenersFromListenerSets(ctx context.Context, gateway gwv1.Gateway) ([]routeutils.listenerSource, []*gwv1.ListenerSet, error) {
	listenerSets := &gwv1.ListenerSetList{}
	err := l.k8sClient.List(ctx, listenerSets)
	if err != nil {
		return nil, nil, err
	}

	rejectedListenerSets := make([]*gwv1.ListenerSet, 0)

	var result []routeutils.listenerSource
	for i, item := range listenerSets.Items {
		handshake, err := l.listenerSetGatewayHandshake(ctx, item, gateway)
		if err != nil {
			return nil, nil, err
		}
		switch handshake {
		case acceptedHandshakeState:
			for _, listener := range item.Spec.Listeners {
				result = append(result, l.convertListenerSetListenerToGatewayListener(item, listener))
			}
			break
		case gatewayRejectedHandshakeState:
			rejectedListenerSets = append(rejectedListenerSets, &listenerSets.Items[i])
			break
		case irrelevantResourceHandshakeState:
			// Nothing to do here, the listener set and gateway have no relation.
			break
		}
	}

	return result, rejectedListenerSets, nil
}

func (l *listenerSetLoaderImpl) listenerSetGatewayHandshake(ctx context.Context, listenerSet gwv1.ListenerSet, gw gwv1.Gateway) (handshakeState, error) {
	// Check if ListenerSet is requesting attachment to this Gateway.
	attach := routeutils.doesResourceAttachToGateway(l.convertListenerSetParentRef(listenerSet.Spec.ParentRef), listenerSet.Namespace, gw)
	if !attach {
		return irrelevantResourceHandshakeState, nil
	}

	var allowedNamespaces gwv1.FromNamespaces
	var labelSelector *metav1.LabelSelector
	if gw.Spec.AllowedListeners == nil || gw.Spec.AllowedListeners.Namespaces == nil || gw.Spec.AllowedListeners.Namespaces.From == nil {
		allowedNamespaces = gwv1.NamespacesFromNone
	} else {
		allowedNamespaces = *gw.Spec.AllowedListeners.Namespaces.From
		labelSelector = gw.Spec.AllowedListeners.Namespaces.Selector
	}

	// Getting here means that the ListenerSet has requested attachment, we need to check if Gateway allows it.
	allowed, err := attachmentutils.DoesResourceAllowNamespace(ctx, allowedNamespaces, labelSelector, l.namespaceSelector, listenerSet.Namespace, gw)
	if err != nil {
		return gatewayRejectedHandshakeState, err
	}

	if allowed {
		return acceptedHandshakeState, nil
	}

	return gatewayRejectedHandshakeState, nil
}

func (l *listenerSetLoaderImpl) convertListenerSetParentRef(ref gwv1.ParentGatewayReference) gwv1.ParentReference {
	return gwv1.ParentReference{
		Group:     ref.Group,
		Kind:      ref.Kind,
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}
}

func (l *listenerSetLoaderImpl) convertListenerSetListenerToGatewayListener(listenerSet gwv1.ListenerSet, entry gwv1.ListenerEntry) routeutils.listenerSource {
	convertedListener := gwv1.Listener{
		Name:          entry.Name,
		Hostname:      entry.Hostname,
		Port:          entry.Port,
		Protocol:      entry.Protocol,
		TLS:           entry.TLS,
		AllowedRoutes: entry.AllowedRoutes,
	}
	return routeutils.listenerSource{
		parent:             listenerSet,
		parentKind:         routeutils.listenerParentKindListenerSet,
		parentCreationTime: listenerSet.CreationTimestamp,
		listener:           convertedListener,
	}
}

func retrieveGatewayListeners(gw gwv1.Gateway) []routeutils.listenerSource {
	listenerSources := make([]routeutils.listenerSource, 0)
	for i := range gw.Spec.Listeners {
		listenerSources = append(listenerSources, routeutils.listenerSource{
			parent:             gw,
			parentKind:         routeutils.listenerParentKindGateway,
			parentCreationTime: gw.CreationTimestamp,
			listener:           gw.Spec.Listeners[i],
		})
	}
	return listenerSources
}

var _ listenerSetLoader = &listenerSetLoaderImpl{}
