package routeutils

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	elbv2gw "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ BackendTargetGroupConfigurator = &GatewayBackendConfig{}

// GatewayBackendConfig describes how to send traffic to a gateway listener port (AKA ALB Target of NLB)
type GatewayBackendConfig struct {
	gateway          *gwv1.Gateway
	arn              string
	port             int
	targetGroupProps *elbv2gw.TargetGroupProps
}

func (g *GatewayBackendConfig) GetTargetType(_ elbv2model.TargetType) elbv2model.TargetType {
	return elbv2model.TargetTypeALB
}

func (g *GatewayBackendConfig) GetTargetGroupProps() *elbv2gw.TargetGroupProps {
	return g.targetGroupProps
}

func (g *GatewayBackendConfig) GetBackendObjectNamespacedName() types.NamespacedName {
	return k8s.NamespacedName(g.gateway)
}

func (g *GatewayBackendConfig) GetDataPort(_ elbv2model.TargetType) int32 {
	return int32(g.port)
}

func (g *GatewayBackendConfig) GetIPAddressType() elbv2model.TargetGroupIPAddressType {
	// ALB target groups always use IPv4.
	return elbv2model.TargetGroupIPAddressTypeIPv4
}

func (g *GatewayBackendConfig) GetHealthCheckPort(_ elbv2model.TargetType, _ bool) (intstr.IntOrString, error) {
	if g.targetGroupProps != nil && g.targetGroupProps.HealthCheckConfig != nil && g.targetGroupProps.HealthCheckConfig.HealthCheckPort != nil {
		return intstr.FromString(*g.targetGroupProps.HealthCheckConfig.HealthCheckPort), nil
	}

	return intstr.FromInt(g.port), nil
}

// GetExternalTrafficPolicy doesn't apply to Gateway backends.
func (g *GatewayBackendConfig) GetExternalTrafficPolicy() string {
	return ""
}
