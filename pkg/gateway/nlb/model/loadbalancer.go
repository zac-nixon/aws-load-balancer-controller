package nlbgatewaymodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"regexp"
	nlbgwv1beta1 "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/algorithm"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/annotations"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	elbv2deploy "sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	ec2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/ec2"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"sort"
	"strconv"
	"strings"
)

const (
	lbAttrsAccessLogsS3Enabled                 = "access_logs.s3.enabled"
	lbAttrsAccessLogsS3Bucket                  = "access_logs.s3.bucket"
	lbAttrsAccessLogsS3Prefix                  = "access_logs.s3.prefix"
	lbAttrsLoadBalancingCrossZoneEnabled       = "load_balancing.cross_zone.enabled"
	lbAttrsLoadBalancingDnsClientRoutingPolicy = "dns_record.client_routing_policy"
	availabilityZoneAffinity                   = "availability_zone_affinity"
	partialAvailabilityZoneAffinity            = "partial_availability_zone_affinity"
	anyAvailabilityZone                        = "any_availability_zone"
	resourceIDLoadBalancer                     = "LoadBalancer"
	resourceIDManagedSecurityGroup             = "ManagedLBSecurityGroup"
)

func (t *defaultModelBuildTask) buildLoadBalancer(ctx context.Context, scheme elbv2model.LoadBalancerScheme) error {
	existingLB, err := t.fetchExistingLoadBalancer(ctx)
	if err != nil {
		return err
	}
	spec, err := t.buildLoadBalancerSpec(ctx, scheme, existingLB)
	if err != nil {
		return err
	}
	t.loadBalancer = elbv2model.NewLoadBalancer(t.stack, resourceIDLoadBalancer, spec)
	return nil
}

func (t *defaultModelBuildTask) buildLoadBalancerSpec(ctx context.Context, scheme elbv2model.LoadBalancerScheme,
	existingLB *elbv2deploy.LoadBalancerWithTags) (elbv2model.LoadBalancerSpec, error) {
	ipAddressType, err := t.buildLoadBalancerIPAddressType(ctx)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	enablePrefixForIpv6SourceNat, err := t.buildLoadBalancerEnablePrefixForIpv6SourceNat(ctx, ipAddressType, t.ec2Subnets)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	lbAttributes, err := t.buildLoadBalancerAttributes(ctx)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	lbMinimumCapacity, err := t.buildLoadBalancerMinimumCapacity(ctx)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	securityGroups, err := t.buildLoadBalancerSecurityGroups(ctx, existingLB, ipAddressType)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	tags, err := t.buildLoadBalancerTags(ctx)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	subnetMappings, err := t.buildLoadBalancerSubnetMappings(ctx, ipAddressType, scheme, t.ec2Subnets, enablePrefixForIpv6SourceNat)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	name, err := t.buildLoadBalancerName(ctx, scheme)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	securityGroupsInboundRulesOnPrivateLink, err := t.buildSecurityGroupsInboundRulesOnPrivateLink(ctx)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}

	spec := elbv2model.LoadBalancerSpec{
		Name:                         name,
		Type:                         elbv2model.LoadBalancerTypeNetwork,
		Scheme:                       scheme,
		IPAddressType:                ipAddressType,
		EnablePrefixForIpv6SourceNat: enablePrefixForIpv6SourceNat,
		SecurityGroups:               securityGroups,
		SubnetMappings:               subnetMappings,
		LoadBalancerAttributes:       lbAttributes,
		MinimumLoadBalancerCapacity:  lbMinimumCapacity,
		Tags:                         tags,
	}

	if securityGroupsInboundRulesOnPrivateLink != nil {
		spec.SecurityGroupsInboundRulesOnPrivateLink = securityGroupsInboundRulesOnPrivateLink
	}

	return spec, nil
}

func (t *defaultModelBuildTask) buildLoadBalancerIPAddressType(_ context.Context) (elbv2model.IPAddressType, error) {
	if t.combinedConfiguration.LoadBalancerType == nil {
		return t.defaultIPAddressType, nil
	}

	switch *t.combinedConfiguration.LoadBalancerType {
	case nlbgwv1beta1.LBIPType(elbv2model.IPAddressTypeIPV4):
		return elbv2model.IPAddressTypeIPV4, nil
	case nlbgwv1beta1.LBIPType(elbv2model.IPAddressTypeDualStack):
		return elbv2model.IPAddressTypeDualStack, nil
	default:
		return "", errors.Errorf("unknown IPAddressType: %v", *t.combinedConfiguration.LoadBalancerType)
	}
}

func (t *defaultModelBuildTask) buildLoadBalancerEnablePrefixForIpv6SourceNat(_ context.Context, ipAddressType elbv2model.IPAddressType, ec2Subnets []ec2types.Subnet) (elbv2model.EnablePrefixForIpv6SourceNat, error) {
	// TODO -- implement this
	return elbv2model.EnablePrefixForIpv6SourceNatOff, nil
}

func (t *defaultModelBuildTask) buildLoadBalancerAttributes(_ context.Context) ([]elbv2model.LoadBalancerAttribute, error) {
	loadBalancerAttributes, err := t.getLoadBalancerAttributes()
	if err != nil {
		return []elbv2model.LoadBalancerAttribute{}, err
	}
	specificAttributes, err := t.getSpecialAttributes()
	if err != nil {
		return []elbv2model.LoadBalancerAttribute{}, err
	}
	mergedAttributes := algorithm.MergeStringMap(specificAttributes, loadBalancerAttributes)
	return makeAttributesSliceFromMap(mergedAttributes), nil
}

func (t *defaultModelBuildTask) getLoadBalancerAttributes() (map[string]string, error) {
	attributes := t.combinedConfiguration.LoadBalancerAttributes
	dnsRecordClientRoutingPolicy, exists := attributes[lbAttrsLoadBalancingDnsClientRoutingPolicy]
	if exists {
		switch dnsRecordClientRoutingPolicy {
		case availabilityZoneAffinity:
		case partialAvailabilityZoneAffinity:
		case anyAvailabilityZone:
		default:
			return nil, errors.Errorf("invalid dns_record.client_routing_policy set in annotation %s: got '%s' expected one of ['%s', '%s', '%s']",
				annotations.SvcLBSuffixLoadBalancerAttributes, dnsRecordClientRoutingPolicy,
				anyAvailabilityZone, partialAvailabilityZoneAffinity, availabilityZoneAffinity)
		}
	}
	return attributes, nil
}

func (t *defaultModelBuildTask) getSpecialAttributes() (map[string]string, error) {
	annotationSpecificAttrs := make(map[string]string)

	if t.combinedConfiguration.AccessLogConfiguration != nil {
		accessLogConfig := t.combinedConfiguration.AccessLogConfiguration
		annotationSpecificAttrs[lbAttrsAccessLogsS3Enabled] = strconv.FormatBool(accessLogConfig.AccessLogsEnabled)

		if accessLogConfig.BucketName != nil {
			annotationSpecificAttrs[lbAttrsAccessLogsS3Bucket] = *accessLogConfig.BucketName
		}

		if accessLogConfig.Prefix != nil {
			annotationSpecificAttrs[lbAttrsAccessLogsS3Prefix] = *accessLogConfig.Prefix
		}
	}

	if t.combinedConfiguration.EnableCrossZoneLoadBalancing != nil {
		annotationSpecificAttrs[lbAttrsLoadBalancingCrossZoneEnabled] = strconv.FormatBool(*t.combinedConfiguration.EnableCrossZoneLoadBalancing)
	}
	return annotationSpecificAttrs, nil
}

func (t *defaultModelBuildTask) buildLoadBalancerMinimumCapacity(_ context.Context) (*elbv2model.MinimumLoadBalancerCapacity, error) {
	// TODO -- implement this.
	return nil, nil
}

func (t *defaultModelBuildTask) buildLoadBalancerSecurityGroups(ctx context.Context, existingLB *elbv2deploy.LoadBalancerWithTags,
	ipAddressType elbv2model.IPAddressType) ([]core.StringToken, error) {
	if existingLB != nil && len(existingLB.LoadBalancer.SecurityGroups) == 0 {
		return nil, nil
	}
	if !t.featureGates.Enabled(config.NLBSecurityGroup) {
		if existingLB != nil && len(existingLB.LoadBalancer.SecurityGroups) != 0 {
			return nil, errors.New("conflicting security groups configuration")
		}
		return nil, nil
	}

	// TODO -- refactor this!
	var lbSGTokens []core.StringToken
	sgNameOrIDs := t.combinedConfiguration.LoadBalancerSecurityGroups
	if sgNameOrIDs == nil || len(*sgNameOrIDs) == 0 {
		managedSG, err := t.buildManagedSecurityGroup(ctx, ipAddressType)
		if err != nil {
			return nil, err
		}
		lbSGTokens = append(lbSGTokens, managedSG.GroupID())
		if !t.enableBackendSG {
			t.backendSGIDToken = managedSG.GroupID()
		} else {
			backendSGID, err := t.backendSGProvider.Get(ctx, networking.ResourceTypeNLBGateway, []types.NamespacedName{k8s.NamespacedName(t.gw)})
			if err != nil {
				return nil, err
			}
			t.backendSGIDToken = core.LiteralStringToken(backendSGID)
			t.backendSGAllocated = true
			lbSGTokens = append(lbSGTokens, t.backendSGIDToken)
		}
	} else {
		manageBackendSGRules, err := t.buildManageSecurityGroupRulesFlag(ctx)
		if err != nil {
			return nil, err
		}
		frontendSGIDs, err := t.sgResolver.ResolveViaNameOrID(ctx, sgNameOrIDs)
		if err != nil {
			return nil, err
		}
		for _, sgID := range frontendSGIDs {
			lbSGTokens = append(lbSGTokens, core.LiteralStringToken(sgID))
		}
		if manageBackendSGRules {
			if !t.enableBackendSG {
				return nil, errors.New("backendSG feature is required to manage worker node SG rules when frontendSG is manually specified")
			}
			backendSGID, err := t.backendSGProvider.Get(ctx, networking.ResourceTypeNLBGateway, []types.NamespacedName{k8s.NamespacedName(t.gw)})
			if err != nil {
				return nil, err
			}
			t.backendSGIDToken = core.LiteralStringToken(backendSGID)
			t.backendSGAllocated = true
			lbSGTokens = append(lbSGTokens, t.backendSGIDToken)
		}
	}
	return lbSGTokens, nil
}

func (t *defaultModelBuildTask) buildManagedSecurityGroup(ctx context.Context, ipAddressType elbv2model.IPAddressType) (*ec2model.SecurityGroup, error) {
	sgSpec, err := t.buildManagedSecurityGroupSpec(ctx, ipAddressType)
	if err != nil {
		return nil, err
	}
	sg := ec2model.NewSecurityGroup(t.stack, resourceIDManagedSecurityGroup, sgSpec)
	return sg, nil
}

func (t *defaultModelBuildTask) buildManagedSecurityGroupSpec(ctx context.Context, ipAddressType elbv2model.IPAddressType) (ec2model.SecurityGroupSpec, error) {
	name := t.buildManagedSecurityGroupName(ctx)
	tags, err := t.buildManagedSecurityGroupTags(ctx)
	if err != nil {
		return ec2model.SecurityGroupSpec{}, err
	}
	ingressPermissions, err := t.buildManagedSecurityGroupIngressPermissions(ctx, ipAddressType)
	if err != nil {
		return ec2model.SecurityGroupSpec{}, err
	}
	return ec2model.SecurityGroupSpec{
		GroupName:   name,
		Description: "[k8s] Managed SecurityGroup for LoadBalancer",
		Tags:        tags,
		Ingress:     ingressPermissions,
	}, nil
}

func (t *defaultModelBuildTask) buildManagedSecurityGroupIngressPermissions(ctx context.Context, ipAddressType elbv2model.IPAddressType) ([]ec2model.IPPermission, error) {
	var permissions []ec2model.IPPermission
	prefixListsConfigured := t.combinedConfiguration.LoadBalancerSecurityGroupPrefixes != nil
	cidrs, err := t.buildCIDRsFromSourceRanges(ctx, ipAddressType, prefixListsConfigured)
	if err != nil {
		return nil, err
	}

	t.tcpRoutes[0].Spec.ParentRefs

	for _, port := range t.service.Spec.Ports {
		listenPort := int32(port.Port)
		for _, cidr := range cidrs {
			if !strings.Contains(cidr, ":") {
				permissions = append(permissions, ec2model.IPPermission{
					IPProtocol: strings.ToLower(string(port.Protocol)),
					FromPort:   awssdk.Int32(listenPort),
					ToPort:     awssdk.Int32(listenPort),
					IPRanges: []ec2model.IPRange{
						{
							CIDRIP: cidr,
						},
					},
				})
			} else {
				permissions = append(permissions, ec2model.IPPermission{
					IPProtocol: strings.ToLower(string(port.Protocol)),
					FromPort:   awssdk.Int32(listenPort),
					ToPort:     awssdk.Int32(listenPort),
					IPv6Range: []ec2model.IPv6Range{
						{
							CIDRIPv6: cidr,
						},
					},
				})
			}
		}
		if prefixListsConfigured {
			for _, prefixID := range prefixListIDs {
				permissions = append(permissions, ec2model.IPPermission{
					IPProtocol: strings.ToLower(string(port.Protocol)),
					FromPort:   awssdk.Int32(listenPort),
					ToPort:     awssdk.Int32(listenPort),
					PrefixLists: []ec2model.PrefixList{
						{
							ListID: prefixID,
						},
					},
				})
			}
		}
	}
	return permissions, nil
}

func (t *defaultModelBuildTask) buildCIDRsFromSourceRanges(_ context.Context, ipAddressType elbv2model.IPAddressType, prefixListsConfigured bool) ([]string, error) {
	var cidrs []string
	// https://github.com/kubernetes-sigs/gateway-api/issues/3074
	// Need to use our own annotation!
	if t.combinedConfiguration.LoadBalancerSourceRanges != nil {
		for _, cidr := range *t.combinedConfiguration.LoadBalancerSourceRanges {
			cidrs = append(cidrs, cidr)
		}
	}

	for _, cidr := range cidrs {
		if strings.Contains(cidr, ":") && ipAddressType != elbv2model.IPAddressTypeDualStack {
			return nil, errors.Errorf("unsupported v6 cidr %v when lb is not dualstack", cidr)
		}
	}
	if len(cidrs) == 0 {
		if prefixListsConfigured {
			return cidrs, nil
		}
		cidrs = append(cidrs, "0.0.0.0/0")
		if ipAddressType == elbv2model.IPAddressTypeDualStack {
			cidrs = append(cidrs, "::/0")
		}
	}
	return cidrs, nil
}

var invalidSecurityGroupNamePtn, _ = regexp.Compile("[[:^alnum:]]")

func (t *defaultModelBuildTask) buildManagedSecurityGroupName(_ context.Context) string {
	uuidHash := sha256.New()
	_, _ = uuidHash.Write([]byte(t.clusterName))
	_, _ = uuidHash.Write([]byte(t.gw.Name))
	_, _ = uuidHash.Write([]byte(t.gw.Namespace))
	_, _ = uuidHash.Write([]byte(t.gw.UID))

	uuid := hex.EncodeToString(uuidHash.Sum(nil))
	sanitizedName := invalidSecurityGroupNamePtn.ReplaceAllString(t.gw.Name, "")
	sanitizedNamespace := invalidSecurityGroupNamePtn.ReplaceAllString(t.gw.Namespace, "")
	return fmt.Sprintf("k8s-%.8s-%.8s-%.10s", sanitizedNamespace, sanitizedName, uuid)
}

func (t *defaultModelBuildTask) buildManagedSecurityGroupTags(ctx context.Context) (map[string]string, error) {
	sgTags, err := t.buildAdditionalResourceTags(ctx)
	if err != nil {
		return nil, err
	}
	return algorithm.MergeStringMap(t.defaultTags, sgTags), nil
}

func (t *defaultModelBuildTask) buildAdditionalResourceTags(_ context.Context) (map[string]string, error) {
	annotationTags := t.combinedConfiguration.ExtraResourceTags
	for tagKey := range annotationTags {
		if t.externalManagedTags.Has(tagKey) {
			return nil, errors.Errorf("external managed tag key %v cannot be specified on Gateway", tagKey)
		}
	}

	mergedTags := algorithm.MergeStringMap(t.defaultTags, annotationTags)
	return mergedTags, nil
}

// TODO -- this is copy pasta
// This is incorrect according to the documentation. (The other function  returns false)
// https://kubernetes-sigs.github.io/aws-load-balancer-controller/v2.10/guide/service/annotations/#manage-backend-sg-rules
func (t *defaultModelBuildTask) buildManageSecurityGroupRulesFlag() (bool, error) {
	if t.combinedConfiguration.EnableBackendSecurityGroupRules == nil {
		return false, nil
	}
	return *t.combinedConfiguration.EnableBackendSecurityGroupRules, nil
}

// TODO -- this is copy pasta.
func makeAttributesSliceFromMap(loadBalancerAttributesMap map[string]string) []elbv2model.LoadBalancerAttribute {
	attributes := make([]elbv2model.LoadBalancerAttribute, 0, len(loadBalancerAttributesMap))
	for attrKey, attrValue := range loadBalancerAttributesMap {
		attributes = append(attributes, elbv2model.LoadBalancerAttribute{
			Key:   attrKey,
			Value: attrValue,
		})
	}
	sort.Slice(attributes, func(i, j int) bool {
		return attributes[i].Key < attributes[j].Key
	})
	return attributes
}
