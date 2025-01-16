package eventhandlers

import (
	"context"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// NewEnqueueRequestsForGatewayClassEvent detects changes to gateway classes and enqueues all gateway objects that
// would effected by a change in the gateway class.
func NewEnqueueRequestsForGatewayClassEvent(log logr.Logger, client client.Client, config config.NLBGatewayConfig) handler.EventHandler {
	return &enqueueRequestsForGatewayClassEvent{
		log:    log,
		client: client,
		config: config,
	}
}

type enqueueRequestsForGatewayClassEvent struct {
	log    logr.Logger
	client client.Client
	config config.NLBGatewayConfig
}

func (h *enqueueRequestsForGatewayClassEvent) Create(ctx context.Context, e event.CreateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueImpactedGateway(ctx, queue, e.Object.(*gwv1.GatewayClass))
}

func (h *enqueueRequestsForGatewayClassEvent) Update(ctx context.Context, e event.UpdateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueImpactedGateway(ctx, queue, e.ObjectNew.(*gwv1.GatewayClass))
}

// No delete for this event handler, deletions for gateway class should be finalized and not allowed as long as gateways
// reference the gateway class.

func (h *enqueueRequestsForGatewayClassEvent) Delete(ctx context.Context, e event.DeleteEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
}

func (h *enqueueRequestsForGatewayClassEvent) Generic(ctx context.Context, e event.GenericEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
}

func (h *enqueueRequestsForGatewayClassEvent) enqueueImpactedGateway(
	ctx context.Context,
	queue workqueue.TypedRateLimitingInterface[reconcile.Request],
	gwClass *gwv1.GatewayClass,
) {
	gwList := &gwv1.GatewayList{}
	err := h.client.List(ctx, gwList)
	if err != nil {
		h.log.Error(err, "unable to list GatewayClasses")
		return
	}

	for _, gw := range gwList.Items {
		if string(gw.Spec.GatewayClassName) == gwClass.Name {
			if string(gwClass.Spec.ControllerName) == h.config.ControllerName {
				queue.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: gw.Namespace,
						Name:      gw.Name,
					},
				})
			}
		}
	}
}
