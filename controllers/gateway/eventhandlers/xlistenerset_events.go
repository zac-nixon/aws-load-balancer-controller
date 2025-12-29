package eventhandlers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/gatewayutils"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwxalpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
)

// NewEnqueueRequestsForXListenerSetEventHandler creates handler for XListenerSet resources
func NewEnqueueRequestsForXListenerSetEventHandler(
	k8sClient client.Client, eventRecorder record.EventRecorder, gwController string, logger logr.Logger) handler.TypedEventHandler[*gwxalpha1.XListenerSet, reconcile.Request] {
	return &enqueueRequestsForXListenerSetEvent{
		k8sClient:     k8sClient,
		eventRecorder: eventRecorder,
		gwController:  gwController,
		logger:        logger,
	}
}

var _ handler.TypedEventHandler[*gwxalpha1.XListenerSet, reconcile.Request] = (*enqueueRequestsForXListenerSetEvent)(nil)

// enqueueRequestsForXListenerSetEvent handles XListenerSet events
type enqueueRequestsForXListenerSetEvent struct {
	k8sClient     client.Client
	eventRecorder record.EventRecorder
	gwController  string
	logger        logr.Logger
}

func (h *enqueueRequestsForXListenerSetEvent) Create(ctx context.Context, e event.TypedCreateEvent[*gwxalpha1.XListenerSet], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	ls := e.Object
	h.logger.V(1).Info("enqueue xlistenerset create event", "xlistenerset", k8s.NamespacedName(ls))
	h.enqueueImpactedGateway(ctx, ls, queue)
}

func (h *enqueueRequestsForXListenerSetEvent) Update(ctx context.Context, e event.TypedUpdateEvent[*gwxalpha1.XListenerSet], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	oldLS := e.ObjectOld
	newLS := e.ObjectNew
	h.logger.V(1).Info("enqueue xlistenerset update event", "xlistenerset", k8s.NamespacedName(newLS))

	// Enqueue the new parent Gateway
	h.enqueueImpactedGateway(ctx, newLS, queue)

	// If parentRef changed, also enqueue the old parent Gateway to remove the listeners
	if oldLS != nil && !h.sameParentRef(oldLS.Spec.ParentRef, newLS.Spec.ParentRef) {
		h.logger.V(1).Info("parentRef changed, enqueueing old parent gateway", "xlistenerset", k8s.NamespacedName(newLS))
		h.enqueueImpactedGateway(ctx, oldLS, queue)
	}
}

func (h *enqueueRequestsForXListenerSetEvent) Delete(ctx context.Context, e event.TypedDeleteEvent[*gwxalpha1.XListenerSet], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	ls := e.Object
	h.logger.V(1).Info("enqueue xlistenerset delete event", "xlistenerset", k8s.NamespacedName(ls))
	h.enqueueImpactedGateway(ctx, ls, queue)
}

func (h *enqueueRequestsForXListenerSetEvent) Generic(ctx context.Context, e event.TypedGenericEvent[*gwxalpha1.XListenerSet], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	ls := e.Object
	h.logger.V(1).Info("enqueue xlistenerset generic event", "xlistenerset", k8s.NamespacedName(ls))
	h.enqueueImpactedGateway(ctx, ls, queue)
}

func (h *enqueueRequestsForXListenerSetEvent) enqueueImpactedGateway(ctx context.Context, ls *gwxalpha1.XListenerSet, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if ls == nil {
		return
	}

	// Get the parent Gateway referenced by this XListenerSet
	parentRef := ls.Spec.ParentRef
	gatewayNamespace := ls.Namespace
	if parentRef.Namespace != nil {
		gatewayNamespace = string(*parentRef.Namespace)
	}

	gatewayName := string(parentRef.Name)
	gatewayKey := client.ObjectKey{
		Namespace: gatewayNamespace,
		Name:      gatewayName,
	}

	// Fetch the Gateway to verify it exists and is managed by this controller
	var gw gwv1.Gateway
	if err := h.k8sClient.Get(ctx, gatewayKey, &gw); err != nil {
		h.logger.V(1).Info("failed to get parent gateway for xlistenerset", "xlistenerset", k8s.NamespacedName(ls), "gateway", gatewayKey, "error", err)
		return
	}

	// Check if Gateway is managed by this controller
	if !gatewayutils.IsGatewayManagedByLBController(ctx, h.k8sClient, &gw, h.gwController) {
		h.logger.V(1).Info("gateway not managed by this controller", "xlistenerset", k8s.NamespacedName(ls), "gateway", k8s.NamespacedName(&gw))
		return
	}

	// Enqueue the parent Gateway for reconciliation
	h.logger.V(1).Info("enqueue parent gateway for xlistenerset event", "xlistenerset", k8s.NamespacedName(ls), "gateway", k8s.NamespacedName(&gw))
	queue.Add(reconcile.Request{NamespacedName: k8s.NamespacedName(&gw)})
}

// sameParentRef compares two ParentGatewayReferences to see if they reference the same Gateway
func (h *enqueueRequestsForXListenerSetEvent) sameParentRef(old, new gwxalpha1.ParentGatewayReference) bool {
	if old.Name != new.Name {
		return false
	}

	// Compare namespaces (handle nil cases)
	oldNS := ""
	if old.Namespace != nil {
		oldNS = string(*old.Namespace)
	}
	newNS := ""
	if new.Namespace != nil {
		newNS = string(*new.Namespace)
	}

	return oldNS == newNS
}
