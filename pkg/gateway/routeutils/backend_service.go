package routeutils

import (
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	elbv2gw "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/shared_constants"
)

var _ BackendTargetGroupConfigurator = &ServiceBackendConfig{}

// ServiceBackendConfig describes how to send traffic to an endpoint list maintained by a service.
type ServiceBackendConfig struct {
	Service          *corev1.Service
	TargetGroupProps *elbv2gw.TargetGroupProps
	ServicePort      *corev1.ServicePort
}

func (s *ServiceBackendConfig) GetTargetType(defaultType elbv2model.TargetType) elbv2model.TargetType {
	if s.TargetGroupProps == nil || s.TargetGroupProps.TargetType == nil {
		return defaultType
	}

	return elbv2model.TargetType(*s.TargetGroupProps.TargetType)
}

func (s *ServiceBackendConfig) GetTargetGroupProps() *elbv2gw.TargetGroupProps {
	return s.TargetGroupProps
}

func (s *ServiceBackendConfig) GetBackendObjectNamespacedName() types.NamespacedName {
	return k8s.NamespacedName(s.Service)
}

func (s *ServiceBackendConfig) GetDataPort(targetType elbv2model.TargetType) int32 {
	if targetType == elbv2model.TargetTypeInstance {
		return s.ServicePort.NodePort
	}
	if s.ServicePort.TargetPort.Type == intstr.Int {
		return int32(s.ServicePort.TargetPort.IntValue())
	}
	// If all else fails, just return 1 as alluded to above, this setting doesn't really matter.
	return 1
}

func (s *ServiceBackendConfig) GetIPAddressType() elbv2model.TargetGroupIPAddressType {
	var ipv6Configured bool
	for _, ipFamily := range s.Service.Spec.IPFamilies {
		if ipFamily == corev1.IPv6Protocol {
			ipv6Configured = true
			break
		}
	}
	if ipv6Configured {
		return elbv2model.TargetGroupIPAddressTypeIPv6
	}
	return elbv2model.TargetGroupIPAddressTypeIPv4
}

func (s *ServiceBackendConfig) GetHealthCheckPort(targetType elbv2model.TargetType, isServiceExternalTrafficPolicyTypeLocal bool) (intstr.IntOrString, error) {
	portConfigNotExist := s.TargetGroupProps == nil || s.TargetGroupProps.HealthCheckConfig == nil || s.TargetGroupProps.HealthCheckConfig.HealthCheckPort == nil

	if portConfigNotExist && isServiceExternalTrafficPolicyTypeLocal {
		return intstr.FromInt32(s.Service.Spec.HealthCheckNodePort), nil
	}

	if portConfigNotExist || *s.TargetGroupProps.HealthCheckConfig.HealthCheckPort == shared_constants.HealthCheckPortTrafficPort {
		return intstr.FromString(shared_constants.HealthCheckPortTrafficPort), nil
	}

	healthCheckPort := intstr.Parse(*s.TargetGroupProps.HealthCheckConfig.HealthCheckPort)
	if healthCheckPort.Type == intstr.Int {
		return healthCheckPort, nil
	}
	hcSvcPort, err := k8s.LookupServicePort(s.Service, healthCheckPort)
	if err != nil {
		return intstr.FromString(""), err
	}

	if targetType == elbv2model.TargetTypeInstance {
		return intstr.FromInt(int(hcSvcPort.NodePort)), nil
	}

	if hcSvcPort.TargetPort.Type == intstr.Int {
		return hcSvcPort.TargetPort, nil
	}
	return intstr.IntOrString{}, errors.New("cannot use named healthCheckPort for IP TargetType when service's targetPort is a named port")
}

func (s *ServiceBackendConfig) GetExternalTrafficPolicy() string {
	return string(s.Service.Spec.ExternalTrafficPolicy)
}
