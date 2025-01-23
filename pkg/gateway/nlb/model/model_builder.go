package nlbgatewaymodel

import (
	"context"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	nlbgwv1beta1 "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/common"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	elbv2deploy "sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/tracking"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
)

const (
	LoadBalancerTypeNLBIP          = "nlb-ip"
	LoadBalancerTypeExternal       = "external"
	LoadBalancerTargetTypeIP       = "ip"
	LoadBalancerTargetTypeInstance = "instance"
	lbAttrsDeletionProtection      = "deletion_protection.enabled"
)

const (
	healthCheckPortTrafficPort = "traffic-port"
)

// ModelBuilder builds the model stack for the service resource.
type ModelBuilder interface {
	// Build model stack for an NLB gateway
	Build(ctx context.Context, gw *gwv1.Gateway, gwClass *gwv1.GatewayClass, routes []common.ControllerRoute, combinedConfiguration *nlbgwv1beta1.NLBGatewayConfigurationSpec) (core.Stack, *elbv2model.LoadBalancer, bool, error)
}

// NewDefaultModelBuilder construct a new defaultModelBuilder
func NewDefaultModelBuilder(subnetsResolver networking.SubnetsResolver,
	vpcInfoProvider networking.VPCInfoProvider, vpcID string, trackingProvider tracking.Provider,
	elbv2TaggingManager elbv2deploy.TaggingManager, ec2Client services.EC2, featureGates config.FeatureGates, clusterName string, defaultTags map[string]string,
	externalManagedTags []string, defaultSSLPolicy string, defaultTargetType string, defaultLoadBalancerScheme string, enableIPTargetType bool,
	backendSGProvider networking.BackendSGProvider, sgResolver networking.SecurityGroupResolver, enableBackendSG bool,
	disableRestrictedSGRules bool, logger logr.Logger) *defaultModelBuilder {
	return &defaultModelBuilder{
		subnetsResolver:           subnetsResolver,
		vpcInfoProvider:           vpcInfoProvider,
		trackingProvider:          trackingProvider,
		elbv2TaggingManager:       elbv2TaggingManager,
		featureGates:              featureGates,
		clusterName:               clusterName,
		vpcID:                     vpcID,
		defaultTags:               defaultTags,
		externalManagedTags:       sets.New(externalManagedTags...),
		defaultSSLPolicy:          defaultSSLPolicy,
		defaultTargetType:         elbv2model.TargetType(defaultTargetType),
		defaultLoadBalancerScheme: elbv2model.LoadBalancerScheme(defaultLoadBalancerScheme),
		enableIPTargetType:        enableIPTargetType,
		backendSGProvider:         backendSGProvider,
		sgResolver:                sgResolver,
		ec2Client:                 ec2Client,
		enableBackendSG:           enableBackendSG,
		disableRestrictedSGRules:  disableRestrictedSGRules,
		logger:                    logger,
	}
}

var _ ModelBuilder = &defaultModelBuilder{}

type defaultModelBuilder struct {
	subnetsResolver          networking.SubnetsResolver
	vpcInfoProvider          networking.VPCInfoProvider
	backendSGProvider        networking.BackendSGProvider
	sgResolver               networking.SecurityGroupResolver
	trackingProvider         tracking.Provider
	elbv2TaggingManager      elbv2deploy.TaggingManager
	featureGates             config.FeatureGates
	ec2Client                services.EC2
	enableBackendSG          bool
	disableRestrictedSGRules bool

	clusterName               string
	vpcID                     string
	defaultTags               map[string]string
	externalManagedTags       sets.Set[string]
	defaultSSLPolicy          string
	defaultTargetType         elbv2model.TargetType
	defaultLoadBalancerScheme elbv2model.LoadBalancerScheme
	enableIPTargetType        bool
	logger                    logr.Logger
}

func (b *defaultModelBuilder) Build(ctx context.Context, gw *gwv1.Gateway, gwClass *gwv1.GatewayClass, routes []common.ControllerRoute, combinedConfiguration *nlbgwv1beta1.NLBGatewayConfigurationSpec) (core.Stack, *elbv2model.LoadBalancer, bool, error) {
	stack := core.NewDefaultStack(core.StackID(k8s.NamespacedName(gw)))
	task := &defaultModelBuildTask{
		clusterName:              b.clusterName,
		vpcID:                    b.vpcID,
		subnetsResolver:          b.subnetsResolver,
		backendSGProvider:        b.backendSGProvider,
		sgResolver:               b.sgResolver,
		vpcInfoProvider:          b.vpcInfoProvider,
		trackingProvider:         b.trackingProvider,
		elbv2TaggingManager:      b.elbv2TaggingManager,
		featureGates:             b.featureGates,
		enableIPTargetType:       b.enableIPTargetType,
		ec2Client:                b.ec2Client,
		enableBackendSG:          b.enableBackendSG,
		disableRestrictedSGRules: b.disableRestrictedSGRules,
		logger:                   b.logger,

		stack:     stack,
		tgByResID: make(map[string]*elbv2model.TargetGroup),

		gw:                    gw,
		gwClass:               gwClass,
		routes:                routes,
		combinedConfiguration: combinedConfiguration,

		defaultTags:                          b.defaultTags,
		externalManagedTags:                  b.externalManagedTags,
		defaultSSLPolicy:                     b.defaultSSLPolicy,
		defaultAccessLogS3Enabled:            false,
		defaultAccessLogsS3Bucket:            "",
		defaultAccessLogsS3Prefix:            "",
		defaultIPAddressType:                 elbv2model.IPAddressTypeIPV4,
		defaultLoadBalancingCrossZoneEnabled: false,
		defaultProxyProtocolV2Enabled:        false,
		defaultTargetType:                    b.defaultTargetType,
		defaultLoadBalancerScheme:            b.defaultLoadBalancerScheme,
		defaultHealthCheckProtocol:           elbv2model.ProtocolTCP,
		defaultHealthCheckPort:               healthCheckPortTrafficPort,
		defaultHealthCheckPath:               "/",
		defaultHealthCheckInterval:           10,
		defaultHealthCheckTimeout:            10,
		defaultHealthCheckHealthyThreshold:   3,
		defaultHealthCheckUnhealthyThreshold: 3,
		defaultHealthCheckMatcherHTTPCode:    "200-399",
		defaultIPv4SourceRanges:              []string{"0.0.0.0/0"},
		defaultIPv6SourceRanges:              []string{"::/0"},

		defaultHealthCheckProtocolForInstanceModeLocal:           elbv2model.ProtocolHTTP,
		defaultHealthCheckPathForInstanceModeLocal:               "/healthz",
		defaultHealthCheckIntervalForInstanceModeLocal:           10,
		defaultHealthCheckTimeoutForInstanceModeLocal:            6,
		defaultHealthCheckHealthyThresholdForInstanceModeLocal:   2,
		defaultHealthCheckUnhealthyThresholdForInstanceModeLocal: 2,
	}

	if err := task.run(ctx); err != nil {
		return nil, nil, false, err
	}
	return task.stack, task.loadBalancer, task.backendSGAllocated, nil
}

type defaultModelBuildTask struct {
	clusterName         string
	vpcID               string
	subnetsResolver     networking.SubnetsResolver
	vpcInfoProvider     networking.VPCInfoProvider
	backendSGProvider   networking.BackendSGProvider
	sgResolver          networking.SecurityGroupResolver
	trackingProvider    tracking.Provider
	elbv2TaggingManager elbv2deploy.TaggingManager
	featureGates        config.FeatureGates
	enableIPTargetType  bool
	ec2Client           services.EC2
	logger              logr.Logger

	stack                    core.Stack
	loadBalancer             *elbv2model.LoadBalancer
	tgByResID                map[string]*elbv2model.TargetGroup
	ec2Subnets               []ec2types.Subnet
	enableBackendSG          bool
	disableRestrictedSGRules bool
	backendSGIDToken         core.StringToken
	backendSGAllocated       bool
	preserveClientIP         bool

	gw                    *gwv1.Gateway
	gwClass               *gwv1.GatewayClass
	routes                []common.ControllerRoute
	combinedConfiguration *nlbgwv1beta1.NLBGatewayConfigurationSpec

	fetchExistingLoadBalancerOnce sync.Once
	existingLoadBalancer          *elbv2deploy.LoadBalancerWithTags

	defaultTags                          map[string]string
	externalManagedTags                  sets.Set[string]
	defaultSSLPolicy                     string
	defaultAccessLogS3Enabled            bool
	defaultAccessLogsS3Bucket            string
	defaultAccessLogsS3Prefix            string
	defaultIPAddressType                 elbv2model.IPAddressType
	defaultLoadBalancingCrossZoneEnabled bool
	defaultProxyProtocolV2Enabled        bool
	defaultTargetType                    elbv2model.TargetType
	defaultLoadBalancerScheme            elbv2model.LoadBalancerScheme
	defaultHealthCheckProtocol           elbv2model.Protocol
	defaultHealthCheckPort               string
	defaultHealthCheckPath               string
	defaultHealthCheckInterval           int32
	defaultHealthCheckTimeout            int32
	defaultHealthCheckHealthyThreshold   int32
	defaultHealthCheckUnhealthyThreshold int32
	defaultHealthCheckMatcherHTTPCode    string
	defaultDeletionProtectionEnabled     bool
	defaultIPv4SourceRanges              []string
	defaultIPv6SourceRanges              []string

	// Default health check settings for NLB instance mode with spec.ExternalTrafficPolicy set to Local
	defaultHealthCheckProtocolForInstanceModeLocal           elbv2model.Protocol
	defaultHealthCheckPathForInstanceModeLocal               string
	defaultHealthCheckIntervalForInstanceModeLocal           int32
	defaultHealthCheckTimeoutForInstanceModeLocal            int32
	defaultHealthCheckHealthyThresholdForInstanceModeLocal   int32
	defaultHealthCheckUnhealthyThresholdForInstanceModeLocal int32
}

func (t *defaultModelBuildTask) run(ctx context.Context) error {
	// TODO something about deletion protection.
	return t.buildModel(ctx)
}

func (t *defaultModelBuildTask) buildModel(ctx context.Context) error {
	scheme, err := t.buildLoadBalancerScheme()
	if err != nil {
		return err
	}
	t.ec2Subnets, err = t.buildLoadBalancerSubnets(ctx, scheme)
	if err != nil {
		return err
	}
	err = t.buildLoadBalancer(ctx, scheme)
	if err != nil {
		return err
	}
	err = t.buildListeners(ctx, scheme)
	if err != nil {
		return err
	}
	return nil
}

func (t *defaultModelBuildTask) fetchExistingLoadBalancer(ctx context.Context) (*elbv2deploy.LoadBalancerWithTags, error) {
	var fetchError error
	t.fetchExistingLoadBalancerOnce.Do(func() {
		stackTags := t.trackingProvider.StackTags(t.stack)
		sdkLBs, err := t.elbv2TaggingManager.ListLoadBalancers(ctx, tracking.TagsAsTagFilter(stackTags))
		if err != nil {
			fetchError = err
		}
		if len(sdkLBs) == 0 {
			t.existingLoadBalancer = nil
		} else {
			t.existingLoadBalancer = &sdkLBs[0]
		}
	})
	return t.existingLoadBalancer, fetchError
}
