package targetgroupbinding

import (
	"fmt"
	awssdk "github.com/aws/aws-sdk-go/aws"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	elbv2sdk "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/cache"
	"net/netip"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"sync"
	"time"
)

const (
	clusterCIDRKey   = "clusterCIDR"
	sharedTgCacheTTL = 12 * time.Hour
)

// MultiClusterManager implements logic to support multiple LBCs managing the same Target Group.
type MultiClusterManager interface {
	// FilterTargets Given a purposed list of targets from a source (probably ELB API), filter the list down to only targets
	// the cluster should operate on.
	FilterTargets(targetInfo []TargetInfo) ([]TargetInfo, error)

	// CheckedSharedTargetGroup Ensures that the controller is running multicluster mode, when the managed Target Group
	// is marked as shared.
	CheckedSharedTargetGroup(tgARN string) error

	// MarkSharedTargetGroup Utilizes the Target Group tags to denote that the controller needs to be running in multicluster mode
	// in order to operate on this Target Group
	MarkSharedTargetGroup(tgARN string) error
}

type multiClusterManagerImpl struct {
	clusterName         string
	multiClusterEnabled bool
	subnetIds           []*string
	sharedTGTagName     string

	ec2   services.EC2
	eks   services.EKS
	elbv2 services.ELBV2

	logger logr.Logger

	cidrCache      *cache.Expiring
	cidrCacheMutex sync.RWMutex
	cidrCacheTTL   time.Duration

	sharedTgCache      *cache.Expiring
	sharedTgCacheMutex sync.RWMutex
	sharedTgCacheTTL   time.Duration
}

// NewMultiClusterManager constructs a multicluster manager that is immediately ready to use.
func NewMultiClusterManager(clusterName string, multiClusterEnabled bool, subnetIds []string, ec2Client services.EC2, eks services.EKS, elbv2 services.ELBV2, cidrCacheTTL int, sharedTGTagName string, logger logr.Logger) MultiClusterManager {
	translatedSubnetIds := make([]*string, 0, len(subnetIds))

	if subnetIds != nil && len(subnetIds) > 0 {
		for _, subnetId := range subnetIds {
			translatedSubnetIds = append(translatedSubnetIds, &subnetId)
		}
	}

	return &multiClusterManagerImpl{
		clusterName:         clusterName,
		multiClusterEnabled: multiClusterEnabled,
		subnetIds:           translatedSubnetIds,
		sharedTGTagName:     sharedTGTagName,

		ec2:   ec2Client,
		eks:   eks,
		elbv2: elbv2,

		logger: logger,

		cidrCache:    cache.NewExpiring(),
		cidrCacheTTL: time.Duration(cidrCacheTTL) * time.Minute,

		sharedTgCache:    cache.NewExpiring(),
		sharedTgCacheTTL: sharedTgCacheTTL,
	}
}

func (m *multiClusterManagerImpl) CheckedSharedTargetGroup(tgARN string) error {
	sharedTg, err := m.lookupSharedTGValue(tgARN)
	if err != nil {
		return err
	}

	if sharedTg && !m.multiClusterEnabled {
		return errors.New("operating on shared target group, but not using multicluster mode.")
	}

	return nil
}

func (m *multiClusterManagerImpl) lookupSharedTGValue(tgARN string) (bool, error) {
	m.sharedTgCacheMutex.Lock()
	defer m.sharedTgCacheMutex.Unlock()

	var sharedTg bool
	var err error
	if v, ok := m.sharedTgCache.Get(tgARN); ok {
		sharedTg = v.(bool)
	} else {
		sharedTg, err = m.getTargetGroupStatus(tgARN)
		if err != nil {
			return false, err
		}
		m.cacheSharedTargetGroupStatus(tgARN, sharedTg)
	}
	return sharedTg, nil
}

func (m *multiClusterManagerImpl) getTargetGroupStatus(tgARN string) (bool, error) {
	req := &elbv2sdk.DescribeTagsInput{
		ResourceArns: []*string{awssdk.String(tgARN)},
	}
	resp, err := m.elbv2.DescribeTags(req)

	if err != nil {
		return false, err
	}

	for _, tagDescription := range resp.TagDescriptions {
		for _, tag := range tagDescription.Tags {
			if tag.Key != nil && *tag.Key == m.sharedTGTagName {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *multiClusterManagerImpl) MarkSharedTargetGroup(tgARN string) error {
	sharedTg, err := m.lookupSharedTGValue(tgARN)
	if err != nil {
		return err
	}

	if !sharedTg {
		req := &elbv2sdk.AddTagsInput{
			ResourceArns: []*string{awssdk.String(tgARN)},
			Tags: []*elbv2sdk.Tag{{
				Key: awssdk.String(m.sharedTGTagName),
			}},
		}
		if _, err := m.elbv2.AddTags(req); err != nil {
			return err
		}
		m.sharedTgCacheMutex.Lock()
		defer m.sharedTgCacheMutex.Unlock()
		m.cacheSharedTargetGroupStatus(tgARN, true)
	}
	return nil
}

func (m *multiClusterManagerImpl) cacheSharedTargetGroupStatus(tgARN string, shared bool) {
	m.sharedTgCache.Set(tgARN, shared, m.sharedTgCacheTTL)
}

func (m *multiClusterManagerImpl) FilterTargets(allTargets []TargetInfo) ([]TargetInfo, error) {
	if !m.multiClusterEnabled || len(allTargets) == 0 {
		return allTargets, nil
	}

	cidrs, err := m.getCIDRS()
	if err != nil {
		m.logger.Error(err, "Failed to get cidrs")
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
		return cachedCIDRs.([]netip.Prefix), nil
	}

	cidrs, err := m.fetchSubnetCIDRFromEC2()
	if err != nil {
		m.logger.Error(err, "failed to fetch subnet cidr")
		return nil, err
	}

	m.cidrCache.Set(clusterCIDRKey, cidrs, m.cidrCacheTTL)
	return cidrs, nil
}

func (m *multiClusterManagerImpl) fetchSubnetCIDRFromEC2() ([]netip.Prefix, error) {

	resolvedSubnetIds, err := m.resolveSubnetIds()

	if err != nil {
		return nil, err
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
		for _, ipv6Assocation := range subnet.Ipv6CidrBlockAssociationSet {
			CIDRStrings = append(CIDRStrings, *ipv6Assocation.Ipv6CidrBlock)
		}
	}
	CIDRs, err := networking.ParseCIDRs(CIDRStrings)
	if err != nil {
		return nil, err
	}
	m.logger.Info(fmt.Sprintf("Retrieved these CIDRs: %s", CIDRStrings))
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
