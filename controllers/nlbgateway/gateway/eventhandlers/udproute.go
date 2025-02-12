package eventhandlers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// TODO -- Look at using generics here. (maybe not possible)

// NewEnqueueRequestsForUDPRouteEvent detects changes to gateway classes and enqueues all gateway objects that
// would effected by a change in the gateway class.
func NewEnqueueRequestsForUDPRouteEvent(log logr.Logger, client client.Client, config config.NLBGatewayConfig) handler.EventHandler {
	return &enqueueRequestsForUDPRouteEvent{
		log:    log,
		client: client,
		config: config,
	}
}

type enqueueRequestsForUDPRouteEvent struct {
	log    logr.Logger
	client client.Client
	config config.NLBGatewayConfig
}

func (h *enqueueRequestsForUDPRouteEvent) Create(ctx context.Context, e event.CreateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueImpactedGateway(ctx, queue, e.Object.(*gwalpha2.UDPRoute))
}

func (h *enqueueRequestsForUDPRouteEvent) Update(ctx context.Context, e event.UpdateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueImpactedGateway(ctx, queue, e.ObjectNew.(*gwalpha2.UDPRoute))
}

// No delete for this event handler, deletions for gateway class should be finalized and not allowed as long as gateways
// reference the gateway class.

func (h *enqueueRequestsForUDPRouteEvent) Delete(ctx context.Context, e event.DeleteEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueImpactedGateway(ctx, queue, e.Object.(*gwalpha2.UDPRoute))
}

func (h *enqueueRequestsForUDPRouteEvent) Generic(ctx context.Context, e event.GenericEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
}

func (h *enqueueRequestsForUDPRouteEvent) enqueueImpactedGateway(
	ctx context.Context,
	queue workqueue.TypedRateLimitingInterface[reconcile.Request],
	route *gwalpha2.UDPRoute,
) {
	gateways, err := GetImpactedGatewaysFromParentRefs(ctx, h.client, route.Spec.ParentRefs, route.Namespace)

	if err != nil {
		h.log.Error(err, fmt.Sprintf("Failed to enqueue impacted gateways that reference route %+v", route))
		return
	}

	for _, gw := range gateways {
		queue.Add(reconcile.Request{
			NamespacedName: gw,
		})
	}
}
