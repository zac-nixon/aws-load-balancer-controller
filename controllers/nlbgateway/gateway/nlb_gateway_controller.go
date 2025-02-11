package gateway

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	elbgwv1beta1 "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/controllers/nlbgateway/gateway/eventhandlers"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy"
	elbv2deploy "sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/tracking"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/common"
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
	// the tag applied to all resources created by this controller.
	gatewayTagPrefix = "gateway.k8s.aws.nlb"

	// the groupVersion used by Gateway & GatewayClass resources.
	gatewayResourceGroupVersion = "gateway.networking.k8s.io/v1"

	// the groupVersion used by TCPRoute and UDPRoute
	routeResourceGroupVersion = "gateway.networking.k8s.io/v1alpha2"

	// the finalizer we attach the Gateway object
	gatewayFinalizer = "gateway.k8s.aws/nlb-finalizer"
)

// NewNLBGatewayReconciler constructs a reconciler that responds to gateway object changes
func NewNLBGatewayReconciler(cloud aws.Cloud, k8sClient client.Client, configurationGetter nlb.GatewayConfigurationGetter, eventRecorder record.EventRecorder, controllerConfig config.ControllerConfig, finalizerManager k8s.FinalizerManager, networkingSGReconciler networking.SecurityGroupReconciler, networkingSGManager networking.SecurityGroupManager, elbv2TaggingManager elbv2deploy.TaggingManager, subnetResolver networking.SubnetsResolver, vpcInfoProvider networking.VPCInfoProvider, backendSGProvider networking.BackendSGProvider, sgResolver networking.SecurityGroupResolver, logger logr.Logger) *nlbGatewayReconciler {

	trackingProvider := tracking.NewDefaultProvider(gatewayTagPrefix, controllerConfig.ClusterName)
	modelBuilder := nlbgatewaymodel.NewDefaultModelBuilder(subnetResolver, vpcInfoProvider, cloud.VpcID(), trackingProvider, elbv2TaggingManager, cloud.EC2(), controllerConfig.FeatureGates, controllerConfig.ClusterName, controllerConfig.DefaultTags, controllerConfig.ExternalManagedTags, controllerConfig.DefaultSSLPolicy, controllerConfig.DefaultTargetType, controllerConfig.DefaultLoadBalancerScheme, backendSGProvider, sgResolver, controllerConfig.EnableBackendSecurityGroup, controllerConfig.DisableRestrictedSGRules, logger)

	stackMarshaller := deploy.NewDefaultStackMarshaller()
	stackDeployer := deploy.NewDefaultStackDeployer(cloud, k8sClient, networkingSGManager, networkingSGReconciler, elbv2TaggingManager, controllerConfig, gatewayTagPrefix, logger)

	return &nlbGatewayReconciler{
		k8sClient:         k8sClient,
		modelBuilder:      modelBuilder,
		backendSGProvider: backendSGProvider,
		stackMarshaller:   stackMarshaller,
		stackDeployer:     stackDeployer,
		finalizerManager:  finalizerManager,
		eventRecorder:     eventRecorder,
		logger:            logger,

		config:              controllerConfig.NLBGatewayConfig,
		configurationGetter: configurationGetter,
	}
}

// nlbGatewayReconciler reconciles an NLB Gateway.
type nlbGatewayReconciler struct {
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

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=udproutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=udproutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=udproutes/finalizers,verbs=update

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes/finalizers,verbs=update

//+kubebuilder:rbac:groups=elbv2.k8s.aws,resources=targetgroupbindings,verbs=get;list;watch
//+kubebuilder:rbac:groups=elbv2.k8s.aws,resources=targetgroupbindings/status,verbs=get;update;patch

func (r *nlbGatewayReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	err := r.reconcileHelper(ctx, req)
	if err != nil {
		r.logger.Error(err, "Got this error!")
	}
	return runtime.HandleReconcileError(err, r.logger)
}

func (r *nlbGatewayReconciler) reconcileHelper(ctx context.Context, req reconcile.Request) error {

	gw := &gwv1.Gateway{}
	if err := r.k8sClient.Get(ctx, req.NamespacedName, gw); err != nil {
		return client.IgnoreNotFound(err)
	}

	r.logger.Info("Got request for reconcile", "gw", *gw)

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

	udpRoutes, err := common.ListUDPRoutes(ctx, r.k8sClient)
	if err != nil {
		return err
	}

	tcpRoutes, err := common.ListTCPRoutes(ctx, r.k8sClient)

	if err != nil {
		return err
	}

	gatewayConfig, err := r.configurationGetter.GenerateNLBGatewayConfiguration(ctx, gw, gwClass)

	if err != nil {
		return err
	}

	relevantUDPRoutes, relevantTCPRoutes := r.filterToRelevantRoutes(gw, udpRoutes, tcpRoutes)

	allRoutes, err := r.mapRoutesToGatewayListener(ctx, gw, relevantUDPRoutes, relevantTCPRoutes)

	stack, lb, backendSGRequired, err := r.buildModel(ctx, gw, gwClass, allRoutes, gatewayConfig)

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

func (r *nlbGatewayReconciler) reconcileDelete(ctx context.Context, gw *gwv1.Gateway, udpRoutes []gwalpha2.UDPRoute, tcpRoutes []gwalpha2.TCPRoute) error {

	if len(udpRoutes) != 0 || len(tcpRoutes) != 0 {
		// TODO - Better error messaging (e.g. tell user the routes that are still attached)
		return errors.New("Gateway still has routes attached")
	}

	return r.finalizerManager.RemoveFinalizers(ctx, gw, gatewayFinalizer)
}

func (r *nlbGatewayReconciler) reconcileUpdate(ctx context.Context, gw *gwv1.Gateway, stack core.Stack,
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

func (r *nlbGatewayReconciler) deployModel(ctx context.Context, gw *gwv1.Gateway, stack core.Stack) error {
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

func (r *nlbGatewayReconciler) buildModel(ctx context.Context, gw *gwv1.Gateway, gwClass *gwv1.GatewayClass, routes map[gwv1.Listener][]common.ControllerRoute, gatewayConfig *elbgwv1beta1.NLBGatewayConfigurationSpec) (core.Stack, *elbv2model.LoadBalancer, bool, error) {
	stack, lb, backendSGRequired, err := r.modelBuilder.Build(ctx, gw, gwClass, routes, gatewayConfig)
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

// gw (nlb) -> route (listener) -> service (target group)
// We already know via parent refs that these routes belong to the gateway.
func (r *nlbGatewayReconciler) mapRoutesToGatewayListener(ctx context.Context, gw *gwv1.Gateway, udpRoutes []gwalpha2.UDPRoute, tcpRoutes []gwalpha2.TCPRoute) (map[gwv1.Listener][]common.ControllerRoute, error) {

	routeMap := map[gwv1.Listener][]common.ControllerRoute{}

	for _, listener := range gw.Spec.Listeners {

		for _, route := range udpRoutes {
			if r.doesRouteBelongToListener(&listener, &listener.Port, &listener.Name) {
				allowed, err := r.doesListenerNamespaceAllowAttachment(&listener, gw.Namespace, route.Namespace)
				if err != nil {
					return routeMap, err
				}
				if allowed {
					convertedRoute, err := common.ConvertKubeUDPRoutes(ctx, r.k8sClient, route)
					if err != nil {
						return routeMap, err
					}
					routeMap[listener] = append(routeMap[listener], convertedRoute)
				} else {
					r.logger.Error(errors.Errorf("Route does not allow attachement"), "", "route", route)
				}
			}
		}

		for _, route := range tcpRoutes {
			if r.doesRouteBelongToListener(&listener, &listener.Port, &listener.Name) {
				allowed, err := r.doesListenerNamespaceAllowAttachment(&listener, gw.Namespace, route.Namespace)
				if err != nil {
					return routeMap, err
				}
				if allowed {
					convertedRoute, err := common.ConvertKubeTCPRoutes(ctx, r.k8sClient, route)
					if err != nil {
						return routeMap, err
					}
					routeMap[listener] = append(routeMap[listener], convertedRoute)
				} else {
					r.logger.Error(errors.Errorf("Route does not allow attachement"), "", "route", route)
				}
			}
		}
	}
	return routeMap, nil
}

func (r *nlbGatewayReconciler) doesRouteBelongToListener(listener *gwv1.Listener, port *gwv1.PortNumber, sectionName *gwv1.SectionName) bool {
	if port != nil && sectionName != nil {
		return listener.Port == *port && listener.Name == *sectionName
	}

	if port != nil {
		return listener.Port == *port
	}

	if sectionName != nil {
		return listener.Name == *sectionName
	}

	return false
}

func (r *nlbGatewayReconciler) doesListenerNamespaceAllowAttachment(listener *gwv1.Listener, gatewayNamespace string, routeNamespace string) (bool, error) {
	if listener.AllowedRoutes == nil {
		return false, nil
	}

	ar := *listener.AllowedRoutes
	// The default when allowed routes is missing is to allow connection in the same namespace.
	if ar.Namespaces == nil || *ar.Namespaces.From == gwv1.NamespacesFromSame {
		return gatewayNamespace == routeNamespace, nil
	}

	if *ar.Namespaces.From == gwv1.NamespacesFromAll {
		return true, nil
	}

	if *ar.Namespaces.From == gwv1.NamespacesFromSelector {
		if ar.Namespaces.Selector == nil {
			return false, errors.Errorf("Namespace selector has to be provided for Selector usage.")
		}

		// TODO -- Add support for this.
	}

	return false, errors.Errorf(string("Unknown value for " + *ar.Namespaces.From))
}

// filterToRelevantRoutes filters the route lists to only routes that are currently attached to the Gateway.
func (r *nlbGatewayReconciler) filterToRelevantRoutes(gw *gwv1.Gateway, udpRoutes *gwalpha2.UDPRouteList, tcpRoutes *gwalpha2.TCPRouteList) ([]gwalpha2.UDPRoute, []gwalpha2.TCPRoute) {

	relevantTCPRoutes := make([]gwalpha2.TCPRoute, 0)
	for _, route := range tcpRoutes.Items {
		for _, parentRef := range route.Spec.ParentRefs {

			// Default for kind is Gateway.
			if parentRef.Kind != nil && *parentRef.Kind != "Gateway" {
				continue
			}

			var namespaceToCompare string

			if parentRef.Namespace != nil {
				namespaceToCompare = string(*parentRef.Namespace)
			} else {
				namespaceToCompare = gw.Namespace
			}

			if string(parentRef.Name) == gw.Name && gw.Namespace == namespaceToCompare {
				relevantTCPRoutes = append(relevantTCPRoutes, route)
				// TODO need a break?
				break
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
				// TODO need a break?
				break
			}
		}
	}

	return relevantUDPRoutes, relevantTCPRoutes
}

func (r *nlbGatewayReconciler) updateGatewayStatus(ctx context.Context, lbDNS string, gw *gwv1.Gateway) error {
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

func (r *nlbGatewayReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
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
