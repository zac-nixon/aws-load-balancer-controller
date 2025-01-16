/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
)

// +kubebuilder:validation:Enum=internal;internet-facing
// LBScheme is the scheme of your NLB
//
// * with `internal` scheme, the NLB is only accessible within the VPC.
// * with `internet-facing` scheme, the NLB is accesible via the public internet.
type LBScheme elbv2.LoadBalancerScheme

// +kubebuilder:validation:Enum=ipv4;dualstack
// LBIPType is the IP type of your NLB
//
// * with `ipv4`, the NLB is accessible via IPv4
// * with `dualstack`, the NLB is accesible via IPv4 and IPv6.
type LBIPType string

// AccessLogConfiguration defines the access log settings for a Load Balancer.
type AccessLogConfiguration struct {
	// enabled rather or not that access logs should be turned sent.
	// +optional
	AccessLogsEnabled bool `json:"accessLogsEnabled,omitempty"`

	// bucketName the bucket to send access logs to.
	// +optional
	BucketName *string `json:"bucketName,omitempty"`

	// prefix is the prefix added to the bucket name.
	// +optional
	Prefix *string `json:"prefix,omitempty"`
}

// NLBGatewayConfigurationSpec defines the desired state of NLBGatewayConfiguration
type NLBGatewayConfigurationSpec struct {
	// loadBalancerScheme defines the type of NLB to provision. If unspecified, it will be automatically inferred.
	// +optional
	LoadBalancerScheme *LBScheme `json:"loadBalancerScheme,omitempty"`

	// loadBalancerIPType defines what kind of load balancer to provision (ipv4, dual stack)
	// +optional
	LoadBalancerType *LBIPType `json:"loadBalancerType,omitempty"`

	// loadBalancerSubnets an optional list of subnet ids or names to be used in the NLB
	// +optional
	LoadBalancerSubnets *[]string `json:"loadBalancerSubnets,omitempty"`

	// loadBalancerSecurityGroups an optional list of security group ids or names to apply to the NLB
	// +optional
	LoadBalancerSecurityGroups *[]string `json:"loadBalancerSecurityGroups,omitempty"`

	// loadBalancerSecurityGroupPrefixes an optional list of prefixes that are allowed to access the NLB.
	// +optional
	LoadBalancerSecurityGroupPrefixes *[]string `json:"loadBalancerSecurityGroupPrefixes,omitempty"`

	// loadBalancerSourceRanges an optional list of CIDRs that are allowed to access the NLB.
	// +optional
	LoadBalancerSourceRanges *[]string `json:"loadBalancerSourceRanges,omitempty"`

	// enableBackendSecurityGroupRules an optional boolean flag that controls whether the controller should automatically add the ingress rules to the instance/ENI security group.
	// +optional
	EnableBackendSecurityGroupRules *bool `json:"enableBackendSecurityGroupRules,omitempty"`

	// loadBalancerAttributes an optional map of attributes to apply to the load balancer.
	// +optional
	LoadBalancerAttributes map[string]string `json:"loadBalancerAttributes,omitempty"`

	// extraResourceTags an optional map of additional tags to apply to AWS resources.
	// +optional
	ExtraResourceTags map[string]string `json:"extraResourceTags,omitempty"`

	// loadBalancerAttributes optional access log configuration for the load balancer.
	// +optional
	AccessLogConfiguration *AccessLogConfiguration `json:"accessLogConfiguration,omitempty"`

	// crossZoneLoadBalancingEnabled optional boolean that toggles routing across availability zones.
	// +optional
	EnableCrossZoneLoadBalancing *bool `json:"enableCrossZoneLoadBalancing,omitempty"`
}

// TODO -- these can be used to set what generation the gateway is currently on to track progress on reconcile.

// NLBGatewayConfigurationStatus defines the observed state of TargetGroupBinding
type NLBGatewayConfigurationStatus struct {
	// The generation of the Gateway Configuration attached to the Gateway object.
	// +optional
	ObservedGatewayConfigurationGeneration *int64 `json:"observedGatewayConfigurationGeneration,omitempty"`
	// The generation of the Gateway Configuration attached to the GatewayClass object.
	// +optional
	ObservedGatewayClassConfigurationGeneration *int64 `json:"observedGatewayClassConfigurationGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// NLBGatewayConfiguration is the Schema for the NLBGatewayConfiguration API
type NLBGatewayConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NLBGatewayConfigurationSpec   `json:"spec,omitempty"`
	Status NLBGatewayConfigurationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NLBGatewayConfigurationList contains a list of NLBGatewayConfiguration
type NLBGatewayConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NLBGatewayConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NLBGatewayConfiguration{}, &NLBGatewayConfigurationList{})
}
