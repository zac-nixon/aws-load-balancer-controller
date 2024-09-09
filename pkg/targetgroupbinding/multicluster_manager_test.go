package targetgroupbinding

import (
	awssdk "github.com/aws/aws-sdk-go/aws"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	elbv2sdk "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/cache"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
	"testing"
	"time"
)

const (
	ClusterName = "my-cluster"
)

func Test_MCM_Constructor(t *testing.T) {
	tests := []struct {
		name              string
		mcEnabled         bool
		subnetIds         []string
		expectedSubnetIds []*string
	}{
		{
			name:              "default case, mc not enabled. no subnets specified.",
			mcEnabled:         false,
			subnetIds:         nil,
			expectedSubnetIds: make([]*string, 0),
		},
		{
			name:              "mc enabled. no subnets specified.",
			mcEnabled:         true,
			subnetIds:         nil,
			expectedSubnetIds: make([]*string, 0),
		},
		{
			name:              "mc enabled. with subnets specified.",
			mcEnabled:         true,
			subnetIds:         []string{"subnet-1", "subnet-2", "subnet-3"},
			expectedSubnetIds: []*string{stringToPointer("subnet-1"), stringToPointer("subnet-2"), stringToPointer("subnet-3")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			eksClient := services.NewMockEKS(ctrl)
			ec2Client := services.NewMockEC2(ctrl)
			elbv2Client := services.NewMockELBV2(ctrl)
			mcManager := NewMultiClusterManager(ClusterName, tt.mcEnabled, tt.subnetIds, ec2Client, eksClient, elbv2Client, 30, "tagName", logr.New(&log.NullLogSink{}))
			assert.NotNil(t, mcManager)
			mcManagerImpl := mcManager.(*multiClusterManagerImpl)
			assert.Equal(t, ClusterName, mcManagerImpl.clusterName)
			assert.Equal(t, tt.mcEnabled, mcManagerImpl.multiClusterEnabled)
			assert.Equal(t, tt.expectedSubnetIds, mcManagerImpl.subnetIds)
			assert.Equal(t, eksClient, mcManagerImpl.eks)
			assert.Equal(t, ec2Client, mcManagerImpl.ec2)
			assert.Equal(t, 30*time.Minute, mcManagerImpl.cidrCacheTTL)
			assert.NotNil(t, mcManagerImpl.logger)
			assert.NotNil(t, mcManagerImpl.cidrCache)
		})
	}
}

func Test_FilterTargets(t *testing.T) {
	type describeClusterCall struct {
		req  *eks.DescribeClusterInput
		resp *eks.DescribeClusterOutput
		err  error
	}
	type describeSubnetsCall struct {
		req  *ec2sdk.DescribeSubnetsInput
		resp *ec2sdk.DescribeSubnetsOutput
		err  error
	}

	type fields struct {
		describeClusterCalls []describeClusterCall
		describeSubnetsCalls []describeSubnetsCall
	}

	tests := []struct {
		name            string
		mcEnabled       bool
		fields          fields
		subnetIds       []*string
		inputTargets    []TargetInfo
		expectedTargets []TargetInfo
		expectedError   error
	}{
		{
			name:      "mc not enabled. no filter",
			mcEnabled: false,
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
		},
		{
			name:      "mc enabled. instance targets no filter",
			mcEnabled: true,
			fields: fields{
				describeClusterCalls: []describeClusterCall{
					{
						req: &eks.DescribeClusterInput{
							Name: awssdk.String(ClusterName),
						},
						resp: &eks.DescribeClusterOutput{
							Cluster: &eks.Cluster{
								ResourcesVpcConfig: &eks.VpcConfigResponse{
									SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
								},
							},
						},
					},
				},
				describeSubnetsCalls: []describeSubnetsCall{
					{
						req: &ec2sdk.DescribeSubnetsInput{
							SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
						},
						resp: &ec2sdk.DescribeSubnetsOutput{
							Subnets: []*ec2sdk.Subnet{
								{
									CidrBlock: awssdk.String("10.0.0.0/24"),
								},
								{
									CidrBlock: awssdk.String("10.0.1.0/24"),
								},
							},
						},
					},
				},
			},
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-foo"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-foo2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-foo3"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-foo"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-foo2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-foo3"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
		},
		{
			name:      "mc enabled. ip targets from other subnets get filtered",
			mcEnabled: true,
			fields: fields{
				describeClusterCalls: []describeClusterCall{
					{
						req: &eks.DescribeClusterInput{
							Name: awssdk.String(ClusterName),
						},
						resp: &eks.DescribeClusterOutput{
							Cluster: &eks.Cluster{
								ResourcesVpcConfig: &eks.VpcConfigResponse{
									SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
								},
							},
						},
					},
				},
				describeSubnetsCalls: []describeSubnetsCall{
					{
						req: &ec2sdk.DescribeSubnetsInput{
							SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
						},
						resp: &ec2sdk.DescribeSubnetsOutput{
							Subnets: []*ec2sdk.Subnet{
								{
									CidrBlock: awssdk.String("10.0.0.0/24"),
								},
								{
									CidrBlock: awssdk.String("10.0.1.0/24"),
								},
							},
						},
					},
				},
			},
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("168.0.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("255.255.255.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedTargets: []TargetInfo{},
		},
		{
			name:      "mc enabled. ip targets from other subnets get filtered. get subnets from args",
			mcEnabled: true,
			subnetIds: []*string{stringToPointer("subnet-1"), stringToPointer("subnet-2")},
			fields: fields{
				describeSubnetsCalls: []describeSubnetsCall{
					{
						req: &ec2sdk.DescribeSubnetsInput{
							SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
						},
						resp: &ec2sdk.DescribeSubnetsOutput{
							Subnets: []*ec2sdk.Subnet{
								{
									CidrBlock: awssdk.String("10.0.0.0/24"),
								},
								{
									CidrBlock: awssdk.String("10.0.1.0/24"),
								},
							},
						},
					},
				},
			},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("168.0.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("255.255.255.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedTargets: []TargetInfo{},
		},
		{
			name:      "mc enabled. ip targets from other subnets get filtered. some targets belong in this clusters' cidr",
			mcEnabled: true,
			fields: fields{
				describeClusterCalls: []describeClusterCall{
					{
						req: &eks.DescribeClusterInput{
							Name: awssdk.String(ClusterName),
						},
						resp: &eks.DescribeClusterOutput{
							Cluster: &eks.Cluster{
								ResourcesVpcConfig: &eks.VpcConfigResponse{
									SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
								},
							},
						},
					},
				},
				describeSubnetsCalls: []describeSubnetsCall{
					{
						req: &ec2sdk.DescribeSubnetsInput{
							SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
						},
						resp: &ec2sdk.DescribeSubnetsOutput{
							Subnets: []*ec2sdk.Subnet{
								{
									CidrBlock: awssdk.String("10.0.0.0/24"),
								},
								{
									CidrBlock: awssdk.String("10.0.1.0/24"),
								},
							},
						},
					},
				},
			},
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("168.0.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("255.255.255.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.200"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.200"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
		},
		{
			name:      "mc enabled. ipv6 targets from other subnets get filtered. some targets belong in this clusters' cidr",
			mcEnabled: true,
			fields: fields{
				describeClusterCalls: []describeClusterCall{
					{
						req: &eks.DescribeClusterInput{
							Name: awssdk.String(ClusterName),
						},
						resp: &eks.DescribeClusterOutput{
							Cluster: &eks.Cluster{
								ResourcesVpcConfig: &eks.VpcConfigResponse{
									SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
								},
							},
						},
					},
				},
				describeSubnetsCalls: []describeSubnetsCall{
					{
						req: &ec2sdk.DescribeSubnetsInput{
							SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
						},
						resp: &ec2sdk.DescribeSubnetsOutput{
							Subnets: []*ec2sdk.Subnet{
								{
									CidrBlock: awssdk.String("10.0.0.0/24"),
									Ipv6CidrBlockAssociationSet: []*ec2sdk.SubnetIpv6CidrBlockAssociation{
										{
											Ipv6CidrBlock: awssdk.String("2600:1f18:48f:6a00::/56"),
										},
									},
								},
							},
						},
					},
				},
			},
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("2600:1f18:048f:6a00:0000:0000:0000:0001"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("2600:1f18:048f:6a00:0000:0000:0000:0002"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("2600:1f18:3d56:b100:0000:0000:0000:0000"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("2600:1f18:3d56:b100:0000:0000:0000:0001"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("2600:1f18:3d56:b100:0000:0000:0000:0002"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("2600:1f18:048f:6a00:0000:0000:0000:0001"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("2600:1f18:048f:6a00:0000:0000:0000:0002"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
		},
		{
			name:      "error case: describe cluster error is propagated correctly",
			mcEnabled: true,
			fields: fields{
				describeClusterCalls: []describeClusterCall{
					{
						req: &eks.DescribeClusterInput{
							Name: awssdk.String(ClusterName),
						},
						err: errors.New("some error"),
					},
				},
			},
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("168.0.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("255.255.255.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.200"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedError: errors.New("some error"),
		},
		{
			name:      "error case: describe subnet returns an invalid cidr, propagate error",
			mcEnabled: true,
			fields: fields{
				describeClusterCalls: []describeClusterCall{
					{
						req: &eks.DescribeClusterInput{
							Name: awssdk.String(ClusterName),
						},
						resp: &eks.DescribeClusterOutput{
							Cluster: &eks.Cluster{
								ResourcesVpcConfig: &eks.VpcConfigResponse{
									SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
								},
							},
						},
					},
				},
				describeSubnetsCalls: []describeSubnetsCall{
					{
						req: &ec2sdk.DescribeSubnetsInput{
							SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
						},
						resp: &ec2sdk.DescribeSubnetsOutput{
							Subnets: []*ec2sdk.Subnet{
								{
									CidrBlock: awssdk.String("foo"),
								},
							},
						},
					},
				},
			},
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("168.0.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("255.255.255.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.200"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedError: errors.New("netip.ParsePrefix(\"foo\"): no '/'"),
		},
		{
			name:      "error case: describe subnet error is propagated correctly",
			mcEnabled: true,
			fields: fields{
				describeClusterCalls: []describeClusterCall{
					{
						req: &eks.DescribeClusterInput{
							Name: awssdk.String(ClusterName),
						},
						resp: &eks.DescribeClusterOutput{
							Cluster: &eks.Cluster{
								ResourcesVpcConfig: &eks.VpcConfigResponse{
									SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
								},
							},
						},
					},
				},
				describeSubnetsCalls: []describeSubnetsCall{
					{
						req: &ec2sdk.DescribeSubnetsInput{
							SubnetIds: awssdk.StringSlice([]string{"subnet-1", "subnet-2"}),
						},
						err: errors.New("some error"),
					},
				},
			},
			subnetIds: []*string{},
			inputTargets: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("168.0.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("255.255.255.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("10.0.0.200"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.0.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			expectedError: errors.New("some error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			eksClient := services.NewMockEKS(ctrl)
			for _, call := range tt.fields.describeClusterCalls {
				eksClient.EXPECT().DescribeCluster(call.req).Return(call.resp, call.err)
			}
			ec2Client := services.NewMockEC2(ctrl)
			for _, call := range tt.fields.describeSubnetsCalls {
				ec2Client.EXPECT().DescribeSubnets(call.req).Return(call.resp, call.err)
			}
			mcManager := multiClusterManagerImpl{
				clusterName:         ClusterName,
				multiClusterEnabled: tt.mcEnabled,
				subnetIds:           tt.subnetIds,
				ec2:                 ec2Client,
				eks:                 eksClient,
				logger:              logr.New(&log.NullLogSink{}),
				cidrCache:           cache.NewExpiring(),
				cidrCacheMutex:      sync.RWMutex{},
				cidrCacheTTL:        10 * time.Minute,
			}

			output, err := mcManager.FilterTargets(tt.inputTargets)

			if tt.expectedError != nil {
				assert.EqualError(t, err, tt.expectedError.Error())
			} else {
				assert.Equal(t, tt.expectedTargets, output)
			}
		})
	}

}

func stringToPointer(s string) *string {
	return &s
}
