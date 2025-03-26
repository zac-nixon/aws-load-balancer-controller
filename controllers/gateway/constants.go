package gateway

/*
Common constants
*/

const (
	// the groupVersion used by Gateway & GatewayClass resources.
	gatewayResourceGroupVersion = "gateway.networking.k8s.io/v1"
)

/*
NLB constants
*/

const (
	// the tag applied to all resources created by the NLB Gateway controller.
	nlbGatewayTagPrefix = "gateway.k8s.aws.nlb"

	// the groupVersion used by TCPRoute and UDPRoute
	nlbRouteResourceGroupVersion = "gateway.networking.k8s.io/v1alpha2"

	// the finalizer we attach the NLB Gateway object
	nlbGatewayFinalizer = "gateway.k8s.aws/nlb-finalizer"
)

/*
ALB Constants
*/

const (
	// the tag applied to all resources created by the ALB Gateway controller.
	albGatewayTagPrefix = "gateway.k8s.aws.nlb"

	// the groupVersion used by HTTPRoute and GRPCRoute
	albRouteResourceGroupVersion = "gateway.networking.k8s.io/v1"

	// the finalizer we attach the ALB Gateway object
	albGatewayFinalizer = "gateway.k8s.aws/alb-finalizer"
)
