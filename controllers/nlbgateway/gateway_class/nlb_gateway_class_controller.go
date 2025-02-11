package gateway_class

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	"time"
)

const (
	// the groupVersion used by Gateway & GatewayClass resources.
	gatewayResourceGroupVersion = "gateway.networking.k8s.io/v1"

	// the groupVersion used by TCPRoute and UDPRoute
	routeResourceGroupVersion = "gateway.networking.k8s.io/v1alpha2"
)

// NewNLBGatewayClassReconciler constructs a reconciler that responds to gateway class object changes
func NewNLBGatewayClassReconciler(k8sClient client.Client, eventRecorder record.EventRecorder, controllerConfig config.ControllerConfig, logger logr.Logger) *nlbGatewayClassReconciler {

	return &nlbGatewayClassReconciler{
		k8sClient:     k8sClient,
		eventRecorder: eventRecorder,
		logger:        logger,

		config: controllerConfig.NLBGatewayConfig,
	}
}

// nlbGatewayClassReconciler reconciles an NLB Gateway Classes .
type nlbGatewayClassReconciler struct {
	k8sClient     client.Client
	eventRecorder record.EventRecorder
	logger        logr.Logger
	config        config.NLBGatewayConfig
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/finalizers,verbs=update

func (r *nlbGatewayClassReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	err := r.reconcile(ctx, req)
	if err != nil {
		r.logger.Error(err, "Got this error!")
	}
	return runtime.HandleReconcileError(err, r.logger)
}

func (r *nlbGatewayClassReconciler) reconcile(ctx context.Context, req reconcile.Request) error {

	// TODO verify the referenced nlb gateway config from params reference

	gwClass := &gwv1.GatewayClass{}
	if err := r.k8sClient.Get(ctx, req.NamespacedName, gwClass); err != nil {
		return client.IgnoreNotFound(err)
	}

	r.logger.Info("Got this gateway class", "class", gwClass)
	if string(gwClass.Spec.ControllerName) != r.config.ControllerName {
		r.logger.Info("Ignoring gateway class not intended for our controller")
		return nil
	}

	gwClassOld := gwClass.DeepCopy()

	for _, v := range gwClass.Status.Conditions {
		if v.Type == "Accepted" && v.Status != "True" {
			cond := &v
			cond.LastTransitionTime = metav1.NewTime(time.Now())
			cond.ObservedGeneration = gwClass.Generation
			cond.Status = "True"
			cond.Message = string(gwv1.GatewayClassReasonAccepted)
			cond.Reason = string(gwv1.GatewayClassReasonAccepted)
			if err := r.k8sClient.Status().Patch(ctx, gwClass, client.MergeFrom(gwClassOld)); err != nil {
				r.logger.Error(err, "Failed to update GatewayClass status")
				return errors.Wrapf(err, "failed to update gatewayclass status")
			}
			return nil
		}
	}

	r.logger.Info("Done but not updating anything.")
	return nil
}

func (r *nlbGatewayClassReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("nlbgatewayclass").
		// Need to add a CRD for gateway class configurations
		For(&gwv1.GatewayClass{}).
		WithOptions(controller.Options{
			// This is probably not used often -- just setting 1?
			//MaxConcurrentReconciles: r.config.MaxConcurrentReconciles,
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}
