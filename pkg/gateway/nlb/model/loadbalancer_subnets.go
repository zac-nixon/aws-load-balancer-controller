package nlbgatewaymodel

import (
	"context"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
)

func (t *defaultModelBuildTask) buildLoadBalancerSubnets(ctx context.Context, scheme elbv2model.LoadBalancerScheme) ([]ec2types.Subnet, error) {
	if t.combinedConfiguration.LoadBalancerSubnets != nil {
		return t.subnetsResolver.ResolveViaNameOrIDSlice(ctx, *t.combinedConfiguration.LoadBalancerSubnets,
			networking.WithSubnetsResolveLBType(elbv2model.LoadBalancerTypeNetwork),
			networking.WithSubnetsResolveLBScheme(scheme),
		)
	}

	existingLB, err := t.fetchExistingLoadBalancer(ctx)
	if err != nil {
		return nil, err
	}
	if existingLB != nil && string(scheme) == string(existingLB.LoadBalancer.Scheme) {
		availabilityZones := existingLB.LoadBalancer.AvailabilityZones
		subnetIDs := make([]string, 0, len(availabilityZones))
		for _, availabilityZone := range availabilityZones {
			subnetID := awssdk.ToString(availabilityZone.SubnetId)
			subnetIDs = append(subnetIDs, subnetID)
		}
		return t.subnetsResolver.ResolveViaNameOrIDSlice(ctx, subnetIDs,
			networking.WithSubnetsResolveLBType(elbv2model.LoadBalancerTypeNetwork),
			networking.WithSubnetsResolveLBScheme(scheme),
		)
	}

	// for internet-facing Load Balancers, the subnets mush have at least 8 available IP addresses;
	// for internal Load Balancers, this is only required if private ip address is not assigned
	/*
		var privateIpv4Addresses []string
		ipv4Configured := t.annotationParser.ParseStringSliceAnnotation(annotations.SvcLBSuffixPrivateIpv4Addresses, &privateIpv4Addresses, t.service.Annotations)
		if (scheme == elbv2model.LoadBalancerSchemeInternetFacing) ||
			((scheme == elbv2model.LoadBalancerSchemeInternal) && !ipv4Configured) {
			return t.subnetsResolver.ResolveViaDiscovery(ctx,
				networking.WithSubnetsResolveLBType(elbv2model.LoadBalancerTypeNetwork),
				networking.WithSubnetsResolveLBScheme(scheme),
				networking.WithSubnetsResolveAvailableIPAddressCount(8),
				networking.WithSubnetsClusterTagCheck(t.featureGates.Enabled(config.SubnetsClusterTagCheck)),
			)
		}
		return t.subnetsResolver.ResolveViaDiscovery(ctx,
			networking.WithSubnetsResolveLBType(elbv2model.LoadBalancerTypeNetwork),
			networking.WithSubnetsResolveLBScheme(scheme),
			networking.WithSubnetsClusterTagCheck(t.featureGates.Enabled(config.SubnetsClusterTagCheck)),
		)
	*/

	// TODO - missing implementation for aws-load-balancer-private-ipv4-addresses

	return t.subnetsResolver.ResolveViaDiscovery(ctx,
		networking.WithSubnetsResolveLBType(elbv2model.LoadBalancerTypeNetwork),
		networking.WithSubnetsResolveLBScheme(scheme),
		networking.WithSubnetsResolveAvailableIPAddressCount(8), // TODO
		networking.WithSubnetsClusterTagCheck(t.featureGates.Enabled(config.SubnetsClusterTagCheck)),
	)
}
