package gateway

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	nlbgwv1beta1 "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/controllers/nlbgateway/gateway/eventhandlers"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/nlb"
	nlbgatewaymodel "sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/nlb/model"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// TODO - Remove references to ServiceEventReason..

const (
	// the groupVersion used by Gateway & GatewayClass resources.
	gatewayResourceGroupVersion = "gateway.networking.k8s.io/v1"

	// the groupVersion used by TCPRoute and UDPRoute
	routeResourceGroupVersion = "gateway.networking.k8s.io/v1alpha2"

	// the finalizer we attach the Gateway object
	gatewayFinalizer = "gateway.k8s.aws/nlb-finalizer"
)

// NewNLBGatewayReconciler constructs a reconciler that responds to gateway object changes
func NewNLBGatewayReconciler(k8sClient client.Client, eventRecorder record.EventRecorder, controllerConfig config.ControllerConfig, finalizerManager k8s.FinalizerManager, configurationGetter nlb.GatewayConfigurationGetter, logger logr.Logger) *nlbGatewayClassReconciler {

	// TODO -- Add stack marshaller and deployer here. sg provider

	return &nlbGatewayClassReconciler{
		k8sClient:        k8sClient,
		finalizerManager: finalizerManager,
		eventRecorder:    eventRecorder,
		logger:           logger,

		config:              controllerConfig.NLBGatewayConfig,
		configurationGetter: configurationGetter,
	}
}

// nlbGatewayClassReconciler reconciles an NLB Gateway Classes .
type nlbGatewayClassReconciler struct {
	k8sClient           client.Client
	modelBuilder        nlbgatewaymodel.ModelBuilder
	backendSGProvider   networking.BackendSGProvider
	stackMarshaller     deploy.StackMarshaller
	stackDeployer       deploy.StackDeployer
	finalizerManager    k8s.FinalizerManager
	eventRecorder       record.EventRecorder
	logger              logr.Logger
	config              config.NLBGatewayConfig
	configurationGetter nlb.GatewayConfigurationGetter
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update

func (r *nlbGatewayClassReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	return runtime.HandleReconcileError(r.reconcileHelper(ctx, req), r.logger)
}

func (r *nlbGatewayClassReconciler) reconcileHelper(ctx context.Context, req reconcile.Request) error {

	gw := &gwv1.Gateway{}
	if err := r.k8sClient.Get(ctx, req.NamespacedName, gw); err != nil {
		return client.IgnoreNotFound(err)
	}

	gwClass := &gwv1.GatewayClass{}

	// Gateway Class is a cluster scoped resource, but the k8s client only accepts namespaced names.
	gwClassNamespacedName := types.NamespacedName{
		Namespace: "",
		Name:      string(gw.Spec.GatewayClassName),
	}

	if err := r.k8sClient.Get(ctx, gwClassNamespacedName, gwClass); err != nil {
		r.logger.Info("Failed to get GatewayClass", "error", err, "gw class name", gwClassNamespacedName.Name)
		return client.IgnoreNotFound(err)
	}

	if string(gwClass.Spec.ControllerName) != r.config.ControllerName {
		// ignore this gateway event as the gateway belongs to a different controller.
		return nil
	}

	udpRoutes, err := nlb.ListUDPRoutes(ctx, r.k8sClient)
	if err != nil {
		return err
	}

	tcpRoutes, err := nlb.ListTCPRoutes(ctx, r.k8sClient)

	if err != nil {
		return err
	}

	gatewayConfig, err := r.configurationGetter.GenerateNLBGatewayConfiguration(ctx, gw, gwClass)

	if err != nil {
		return err
	}

	relevantUDPRoutes, relevantTCPRoutes := r.filterToRelevantRoutes(gw, udpRoutes, tcpRoutes)

	stack, lb, backendSGRequired, err := r.buildModel(ctx, gw, gwClass, relevantUDPRoutes, relevantTCPRoutes, gatewayConfig)

	if err != nil {
		return err
	}

	if lb == nil {
		err = r.reconcileDelete(ctx, gw, relevantUDPRoutes, relevantTCPRoutes)
		if err != nil {
			r.logger.Error(err, "Failed to process gateway delete")
		}
		return err
	}

	// Fetch GatewayClass Config Object + Gateway Config Object
	// Merge configuration objects
	// Build NLB definition
	// Deploy NLB

	return r.reconcileUpdate(ctx, gw, stack, lb, backendSGRequired)
}

func (r *nlbGatewayClassReconciler) reconcileDelete(ctx context.Context, gw *gwv1.Gateway, udpRoutes []gwalpha2.UDPRoute, tcpRoutes []gwalpha2.TCPRoute) error {

	if len(udpRoutes) != 0 || len(tcpRoutes) != 0 {
		// TODO - Better error messaging (e.g. tell user the routes that are still attached)
		return errors.New("Gateway still has routes attached")
	}

	return r.finalizerManager.RemoveFinalizers(ctx, gw, gatewayFinalizer)
}

func (r *nlbGatewayClassReconciler) reconcileUpdate(ctx context.Context, gw *gwv1.Gateway, stack core.Stack,
	lb *elbv2model.LoadBalancer, backendSGRequired bool) error {

	if err := r.finalizerManager.AddFinalizers(ctx, gw, gatewayFinalizer); err != nil {
		r.eventRecorder.Event(gw, corev1.EventTypeWarning, k8s.ServiceEventReasonFailedAddFinalizer, fmt.Sprintf("Failed add finalizer due to %v", err))
		return err
	}
	err := r.deployModel(ctx, gw, stack)
	if err != nil {
		return err
	}
	lbDNS, err := lb.DNSName().Resolve(ctx)
	if err != nil {
		return err
	}

	if !backendSGRequired {
		if err := r.backendSGProvider.Release(ctx, networking.ResourceTypeService, []types.NamespacedName{k8s.NamespacedName(gw)}); err != nil {
			return err
		}
	}

	if err = r.updateGatewayStatus(ctx, lbDNS, gw); err != nil {
		r.eventRecorder.Event(gw, corev1.EventTypeWarning, k8s.ServiceEventReasonFailedUpdateStatus, fmt.Sprintf("Failed update status due to %v", err))
		return err
	}
	r.eventRecorder.Event(gw, corev1.EventTypeNormal, k8s.ServiceEventReasonSuccessfullyReconciled, "Successfully reconciled")
	return nil
}

func (r *nlbGatewayClassReconciler) deployModel(ctx context.Context, gw *gwv1.Gateway, stack core.Stack) error {
	if err := r.stackDeployer.Deploy(ctx, stack); err != nil {
		var requeueNeededAfter *runtime.RequeueNeededAfter
		if errors.As(err, &requeueNeededAfter) {
			return err
		}
		r.eventRecorder.Event(gw, corev1.EventTypeWarning, k8s.ServiceEventReasonFailedDeployModel, fmt.Sprintf("Failed deploy model due to %v", err))
		return err
	}
	r.logger.Info("successfully deployed model", "gateway", k8s.NamespacedName(gw))
	return nil
}

func (r *nlbGatewayClassReconciler) buildModel(ctx context.Context, gw *gwv1.Gateway, gwClass *gwv1.GatewayClass, udpRoutes []gwalpha2.UDPRoute, tcpRoutes []gwalpha2.TCPRoute, gatewayConfig *nlbgwv1beta1.NLBGatewayConfigurationSpec) (core.Stack, *elbv2model.LoadBalancer, bool, error) {
	stack, lb, backendSGRequired, err := r.modelBuilder.Build(ctx, gw, gwClass, udpRoutes, tcpRoutes, gatewayConfig)
	if err != nil {
		r.eventRecorder.Event(gw, corev1.EventTypeWarning, k8s.ServiceEventReasonFailedBuildModel, fmt.Sprintf("Failed build model due to %v", err))
		return nil, nil, false, err
	}
	stackJSON, err := r.stackMarshaller.Marshal(stack)
	if err != nil {
		r.eventRecorder.Event(gw, corev1.EventTypeWarning, k8s.ServiceEventReasonFailedBuildModel, fmt.Sprintf("Failed build model due to %v", err))
		return nil, nil, false, err
	}
	r.logger.Info("successfully built model", "model", stackJSON)
	return stack, lb, backendSGRequired, nil
}

// filterToRelevantRoutes filters the route lists to only routes that are currently attached to the Gateway.
func (r *nlbGatewayClassReconciler) filterToRelevantRoutes(gw *gwv1.Gateway, udpRoutes *gwalpha2.UDPRouteList, tcpRoutes *gwalpha2.TCPRouteList) ([]gwalpha2.UDPRoute, []gwalpha2.TCPRoute) {

	relevantTCPRoutes := make([]gwalpha2.TCPRoute, 0)
	for _, route := range tcpRoutes.Items {
		for _, parentRef := range route.Spec.ParentRefs {

			var namespaceToCompare string

			if parentRef.Namespace != nil {
				namespaceToCompare = string(*parentRef.Namespace)
			} else {
				namespaceToCompare = gw.Namespace
			}

			if string(parentRef.Name) == gw.Name && gw.Namespace == namespaceToCompare {
				relevantTCPRoutes = append(relevantTCPRoutes, route)
			}
		}
	}

	relevantUDPRoutes := make([]gwalpha2.UDPRoute, 0)
	for _, route := range udpRoutes.Items {
		for _, parentRef := range route.Spec.ParentRefs {

			var namespaceToCompare string

			if parentRef.Namespace != nil {
				namespaceToCompare = string(*parentRef.Namespace)
			} else {
				namespaceToCompare = gw.Namespace
			}

			if string(parentRef.Name) == gw.Name && gw.Namespace == namespaceToCompare {
				relevantUDPRoutes = append(relevantUDPRoutes, route)
			}
		}
	}

	return relevantUDPRoutes, relevantTCPRoutes
}

func (r *nlbGatewayClassReconciler) updateGatewayStatus(ctx context.Context, lbDNS string, gw *gwv1.Gateway) error {
	// TODO Consider LB ARN.

	// Gateway Address Status
	if len(gw.Status.Addresses) != 1 ||
		gw.Status.Addresses[0].Value != "" ||
		gw.Status.Addresses[0].Value != lbDNS {
		gwOld := gw.DeepCopy()
		ipAddressType := gwv1.HostnameAddressType
		gw.Status.Addresses = []gwv1.GatewayStatusAddress{
			{
				Type:  &ipAddressType,
				Value: lbDNS,
			},
		}
		if err := r.k8sClient.Status().Patch(ctx, gw, client.MergeFrom(gwOld)); err != nil {
			return errors.Wrapf(err, "failed to update gw status: %v", k8s.NamespacedName(gw))
		}
	}

	// TODO: Listener status ListenerStatus
	// https://github.com/aws/aws-application-networking-k8s/blob/main/pkg/controllers/gateway_controller.go#L350

	return nil
}

func (r *nlbGatewayClassReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	gatewayClassHandler := eventhandlers.NewEnqueueRequestsForGatewayClassEvent(r.logger, r.k8sClient, r.config)
	return ctrl.NewControllerManagedBy(mgr).
		Named("nlbgateway").
		// Anything that influences a gateway object must be added here.
		// E.g. a CRD defining a gateway would go here too.
		For(&gwv1.Gateway{}).
		Watches(&gwv1.GatewayClass{}, gatewayClassHandler).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.config.MaxConcurrentReconciles,
		}).
		Complete(r)
}
