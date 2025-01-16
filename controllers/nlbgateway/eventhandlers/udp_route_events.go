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
	gwalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func NewEnqueueRequestsForUDPRouteEvent(logger logr.Logger, client client.Client, gatewayConfig config.NLBGatewayConfig) handler.EventHandler {
	return &enqueueRequestsForUDPRouteEvent{
		logger:        logger,
		client:        client,
		gatewayConfig: gatewayConfig,
	}
}

type enqueueRequestsForUDPRouteEvent struct {
	logger        logr.Logger
	client        client.Client
	gatewayConfig config.NLBGatewayConfig
}

func (h *enqueueRequestsForUDPRouteEvent) Create(ctx context.Context, e event.CreateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	route := e.Object.(*gwalpha2.UDPRoute)
	h.enqueueImpactedGateway(ctx, queue, route)
}

func (h *enqueueRequestsForUDPRouteEvent) Update(ctx context.Context, e event.UpdateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	route := e.ObjectNew.(*gwalpha2.UDPRoute)
	h.enqueueImpactedGateway(ctx, queue, route)
}

func (h *enqueueRequestsForUDPRouteEvent) Delete(ctx context.Context, e event.DeleteEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	route := e.Object.(*gwalpha2.UDPRoute)
	h.enqueueImpactedGateway(ctx, queue, route)
}

func (h *enqueueRequestsForUDPRouteEvent) Generic(ctx context.Context, e event.GenericEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
}

func (h *enqueueRequestsForUDPRouteEvent) enqueueImpactedGateway(
	_ context.Context,
	queue workqueue.TypedRateLimitingInterface[reconcile.Request],
	route *gwalpha2.UDPRoute,
) {

	// TODO -- Need to filter to only gateways we care about.
	for _, parentGateway := range route.Spec.ParentRefs {
		gatewayName := string(parentGateway.Name)
		gatewayNamespace := route.Namespace

		if parentGateway.Namespace != nil {
			gatewayNamespace = string(*parentGateway.Namespace)
		}

		h.logger.Info("Found matching gateway", "gw", types.NamespacedName{Namespace: gatewayNamespace, Name: gatewayName})
		queue.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: gatewayNamespace,
				Name:      gatewayName,
			},
		})
	}
}
