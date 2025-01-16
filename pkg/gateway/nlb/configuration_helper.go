package nlb

import (
	"context"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	nlbgwv1beta1 "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/algorithm"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// GatewayConfigurationGetter ...
type GatewayConfigurationGetter interface {
	GenerateNLBGatewayConfiguration(ctx context.Context, gw *gwv1.Gateway, gwClass *gwv1.GatewayClass) (*nlbgwv1beta1.NLBGatewayConfigurationSpec, error)
}

type gatewayConfigurationGetter struct {
	client client.Client
}

var _ GatewayConfigurationGetter = &gatewayConfigurationGetter{}

func (getter *gatewayConfigurationGetter) GenerateNLBGatewayConfiguration(ctx context.Context, gw *gwv1.Gateway, gwClass *gwv1.GatewayClass) (*nlbgwv1beta1.NLBGatewayConfigurationSpec, error) {
	gwConfig, err := getter.retrieveConfigurationObject(ctx, convertLocalParameterReference(gw))

	if err != nil {
		return nil, err
	}

	gwClassConfig, err := getter.retrieveConfigurationObject(ctx, gwClass.Spec.ParametersRef)

	// TODO -- We are setting the gw class configuration above the gw config.
	// Need to decide if that is the right call.
	return priorityMerge(gwClassConfig, gwConfig), nil
}

func (getter *gatewayConfigurationGetter) retrieveConfigurationObject(ctx context.Context, paramReference *gwv1.ParametersReference) (*nlbgwv1beta1.NLBGatewayConfigurationSpec, error) {
	if paramReference == nil {
		return &nlbgwv1beta1.NLBGatewayConfigurationSpec{}, nil
	}

	if paramReference.Kind != "NLBGatewayConfiguration" {
		return nil, errors.Errorf("expected NLBGatewayConfiguration resource but got %s", paramReference.Kind)
	}

	config := &nlbgwv1beta1.NLBGatewayConfiguration{}

	namespace := ""
	if paramReference.Namespace != nil {
		namespace = string(*paramReference.Namespace)
	}

	if err := getter.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: paramReference.Name}, config); err != nil {
		return nil, err
	}

	return &config.Spec, nil
}

func convertLocalParameterReference(gw *gwv1.Gateway) *gwv1.ParametersReference {
	if gw == nil || gw.Spec.Infrastructure.ParametersRef == nil {
		return nil
	}

	return &gwv1.ParametersReference{
		Name:      gw.Spec.Infrastructure.ParametersRef.Name,
		Group:     gw.Spec.Infrastructure.ParametersRef.Group,
		Kind:      gw.Spec.Infrastructure.ParametersRef.Kind,
		Namespace: (*gwv1.Namespace)(&gw.Namespace),
	}
}

func priorityMerge(highPriority *nlbgwv1beta1.NLBGatewayConfigurationSpec, lowPriority *nlbgwv1beta1.NLBGatewayConfigurationSpec) *nlbgwv1beta1.NLBGatewayConfigurationSpec {
	result := &nlbgwv1beta1.NLBGatewayConfigurationSpec{}

	// TODO -- How to do this merging logic cleanly?
	if highPriority.LoadBalancerScheme != nil {
		result.LoadBalancerScheme = highPriority.LoadBalancerScheme
	} else if lowPriority.LoadBalancerScheme != nil {
		result.LoadBalancerScheme = lowPriority.LoadBalancerScheme
	}

	if highPriority.LoadBalancerSubnets != nil {
		result.LoadBalancerSubnets = highPriority.LoadBalancerSubnets
	} else if lowPriority.LoadBalancerSubnets != nil {
		result.LoadBalancerSubnets = lowPriority.LoadBalancerSubnets
	}

	if highPriority.LoadBalancerSecurityGroups != nil {
		result.LoadBalancerSecurityGroups = highPriority.LoadBalancerSecurityGroups
	} else if lowPriority.LoadBalancerSecurityGroups != nil {
		result.LoadBalancerSecurityGroups = lowPriority.LoadBalancerSecurityGroups
	}

	if highPriority.LoadBalancerSecurityGroupPrefixes != nil {
		result.LoadBalancerSecurityGroupPrefixes = highPriority.LoadBalancerSecurityGroupPrefixes
	} else if lowPriority.LoadBalancerSecurityGroupPrefixes != nil {
		result.LoadBalancerSecurityGroupPrefixes = lowPriority.LoadBalancerSecurityGroupPrefixes
	}

	if highPriority.LoadBalancerSourceRanges != nil {
		result.LoadBalancerSourceRanges = highPriority.LoadBalancerSourceRanges
	} else if lowPriority.LoadBalancerSourceRanges != nil {
		result.LoadBalancerSourceRanges = lowPriority.LoadBalancerSourceRanges
	}

	if highPriority.EnableBackendSecurityGroupRules != nil {
		result.EnableBackendSecurityGroupRules = highPriority.EnableBackendSecurityGroupRules
	} else if lowPriority.EnableBackendSecurityGroupRules != nil {
		result.EnableBackendSecurityGroupRules = lowPriority.EnableBackendSecurityGroupRules
	}

	if highPriority.LoadBalancerType != nil {
		result.LoadBalancerType = highPriority.LoadBalancerType
	} else if lowPriority.LoadBalancerType != nil {
		result.LoadBalancerType = lowPriority.LoadBalancerType
	}

	result.LoadBalancerAttributes = algorithm.MergeStringMap(highPriority.LoadBalancerAttributes, lowPriority.LoadBalancerAttributes)

	result.ExtraResourceTags = algorithm.MergeStringMap(highPriority.ExtraResourceTags, lowPriority.ExtraResourceTags)

	if highPriority.AccessLogConfiguration != nil {
		result.AccessLogConfiguration = highPriority.AccessLogConfiguration
	} else if lowPriority.AccessLogConfiguration != nil {
		result.AccessLogConfiguration = lowPriority.AccessLogConfiguration
	}

	if highPriority.EnableCrossZoneLoadBalancing != nil {
		result.EnableCrossZoneLoadBalancing = highPriority.EnableCrossZoneLoadBalancing
	} else if lowPriority.EnableCrossZoneLoadBalancing != nil {
		result.EnableCrossZoneLoadBalancing = lowPriority.EnableCrossZoneLoadBalancing
	}

	return result
}
