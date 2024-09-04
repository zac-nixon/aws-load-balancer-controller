package targetgroupbinding

import (
	"fmt"
	awssdk "github.com/aws/aws-sdk-go/aws"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/cache"
	"net/netip"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"sync"
	"time"
)

const (
	cidrCacheTTL   = 30 * time.Minute
	clusterCIDRKey = "clusterCIDR"
)

type MultiClusterManager interface {
	FilterTargets(targetInfo []TargetInfo) ([]TargetInfo, error)
}

type multiClusterManagerImpl struct {
	clusterName         string
	multiClusterEnabled bool
	subnetIds           []*string

	ec2 services.EC2
	eks services.EKS

	logger logr.Logger

	cidrCache      *cache.Expiring
	cidrCacheMutex sync.RWMutex
	cidrCacheTTL   time.Duration
}

func NewMultiClusterManager(clusterName string, multiClusterEnabled bool, subnetIds []string, ec2Client services.EC2, eks services.EKS, logger logr.Logger) MultiClusterManager {
	translatedSubnetIds := make([]*string, 0, len(subnetIds))

	if subnetIds != nil {
		for _, subnetId := range subnetIds {
			logger.Info(fmt.Sprintf("subnet id!: %s", subnetIds))
			translatedSubnetIds = append(translatedSubnetIds, &subnetId)
		}
	}

	return &multiClusterManagerImpl{
		clusterName:         clusterName,
		multiClusterEnabled: multiClusterEnabled,
		subnetIds:           translatedSubnetIds,

		ec2: ec2Client,
		eks: eks,

		logger: logger,

		cidrCache:    cache.NewExpiring(),
		cidrCacheTTL: cidrCacheTTL,
	}
}

func (m *multiClusterManagerImpl) FilterTargets(allTargets []TargetInfo) ([]TargetInfo, error) {
	if !m.multiClusterEnabled {
		return allTargets, nil
	}

	cidrs, err := m.getCIDRS()
	if err != nil {
		return nil, err
	}

	filteredTargets := make([]TargetInfo, 0, len(allTargets))

	for _, targetInfo := range allTargets {
		// We only care about IP targets, for any other kind of target we can just choose to not filter out the target.
		ip, err := netip.ParseAddr(*targetInfo.Target.Id)
		if err == nil {
			if networking.IsIPWithinCIDRs(ip, cidrs) {
				filteredTargets = append(filteredTargets, targetInfo)
			}
		} else {
			filteredTargets = append(filteredTargets, targetInfo)
		}
	}

	return filteredTargets, nil
}

func (m *multiClusterManagerImpl) getCIDRS() ([]netip.Prefix, error) {
	m.cidrCacheMutex.Lock()
	defer m.cidrCacheMutex.Unlock()

	if cachedCIDRs, ok := m.cidrCache.Get(clusterCIDRKey); ok {
		m.logger.Info(fmt.Sprintf("Found multi-cluster manager CIDR cache: %s", cachedCIDRs))
		return cachedCIDRs.([]netip.Prefix), nil
	}

	cidrs, err := m.fetchSubnetCIDRFromEC2()
	if err != nil {
		m.logger.Error(err, "failed to fetch subnet cidr")
		return nil, err
	}

	m.logger.Info(fmt.Sprintf("Found multi-cluster manager CIDR from source: %s", cidrs))
	m.cidrCache.Set(clusterCIDRKey, cidrs, m.cidrCacheTTL)
	return cidrs, nil
}

func (m *multiClusterManagerImpl) fetchSubnetCIDRFromEC2() ([]netip.Prefix, error) {

	resolvedSubnetIds, err := m.resolveSubnetIds()

	if err != nil {
		return nil, err
	}

	for _, subnetId := range resolvedSubnetIds {
		m.logger.Info(fmt.Sprintf("Fetching subnet cidr for subnet: %s", *subnetId))
	}

	input := &ec2sdk.DescribeSubnetsInput{
		SubnetIds: resolvedSubnetIds,
	}

	output, err := m.ec2.DescribeSubnets(input)
	if err != nil {
		return nil, err
	}
	var CIDRStrings []string
	for _, subnet := range output.Subnets {
		CIDRStrings = append(CIDRStrings, *subnet.CidrBlock)
	}
	CIDRs, err := networking.ParseCIDRs(CIDRStrings)
	if err != nil {
		return nil, err
	}
	m.logger.Info(fmt.Sprintf("Found %d subnets in %d CIDRs", len(CIDRs), len(CIDRStrings)))
	return CIDRs, nil
}

func (m *multiClusterManagerImpl) resolveSubnetIds() ([]*string, error) {
	if len(m.subnetIds) > 0 {
		return m.subnetIds, nil
	}
	input := &eks.DescribeClusterInput{
		Name: awssdk.String(m.clusterName),
	}
	result, err := m.eks.DescribeCluster(input)
	if err != nil {
		return nil, err
	}
	subnetIDs := result.Cluster.ResourcesVpcConfig.SubnetIds
	return subnetIDs, nil
}
