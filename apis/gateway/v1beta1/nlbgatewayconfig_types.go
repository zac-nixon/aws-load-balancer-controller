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

// +kubebuilder:validation:Enum=HTTP1Only;HTTP2Only;HTTP2Optional;HTTP2Preferred;None
// ALPNPolicy defines the ALPN policy for the LB
//
// HTTP1Only Negotiate only HTTP/1.*. The ALPN preference list is http/1.1, http/1.0.
// HTTP2Only Negotiate only HTTP/2. The ALPN preference list is h2.
// HTTP2Optional Prefer HTTP/1.* over HTTP/2 (which can be useful for HTTP/2 testing). The ALPN preference list is http/1.1, http/1.0, h2.
// HTTP2Preferred Prefer HTTP/2 over HTTP/1.*. The ALPN preference list is h2, http/1.1, http/1.0.
// None Do not negotiate ALPN. This is the default.

type ALPNPolicy elbv2.ALPNPolicy

// AccessLogConfiguration defines the access log settings for a Load Balancer.
type AccessLogConfiguration struct {
	// accessLogsEnabled whether or not that access logs should be turned sent.
	// +optional
	AccessLogsEnabled bool `json:"accessLogsEnabled,omitempty"`

	// bucketName the bucket to send access logs to.
	// +optional
	BucketName *string `json:"bucketName,omitempty"`

	// prefix is the prefix added to the bucket name.
	// +optional
	Prefix *string `json:"prefix,omitempty"`
}

// SubnetConfiguration defines the subnet settings for a Load Balancer.
type SubnetConfiguration struct {
	// identifier name or id for the subnet
	Identifier string `json:"identifier"`

	// eipAllocation the EIP name for this subnet.
	// +optional
	EIPAllocation *string `json:"eipAllocation,omitempty"`

	// privateIPv4Allocation the private ipv4 address to assign to this subnet.
	// +optional
	PrivateIPv4Allocation *string `json:"privateIPv4Allocation,omitempty"`

	// privateIPv6Allocation the private ipv6 address to assign to this subnet.
	// +optional
	PrivateIPv6Allocation *string `json:"privateIPv6Allocation,omitempty"`
}

// SSLConfiguration defines the SSL configuration for a listener on the Load Balancer.
type SSLConfiguration struct {
	// defaultCertificate the cert arn to be used by default.
	DefaultCertificate string `json:"defaultCertificate"`

	// certificates the list of other certificates to add to the listener.
	// +optional
	Certificates []string `json:"certificates,omitempty"`

	// negotiationPolicy the policy to use...
	// +optional
	NegotiationPolicy *string `json:"negotiationPolicy,omitempty"`

	// backendProtocol the protocol to use...
	// +optional
	BackendProtocol string `json:"backendProtocol,omitempty"`

	// alpnPolicy an optional string that allows you to configure ALPN policies on your Load Balancer
	// +optional
	ALPNPolicy ALPNPolicy `json:"alpnPolicy,omitempty"`
}

// NLBGatewayConfigurationSpec defines the desired state of NLBGatewayConfiguration
type NLBGatewayConfigurationSpec struct {
	// DONE
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// loadBalancerName defines the name of the NLB to provision. If unspecified, it will be automatically generated.
	// +optional
	LoadBalancerName *string `json:"loadBalancerName,omitempty"`

	// DONE
	// loadBalancerScheme defines the type of NLB to provision. If unspecified, it will be automatically inferred.
	// +optional
	LoadBalancerScheme *LBScheme `json:"loadBalancerScheme,omitempty"`

	// DONE
	// loadBalancerIPType defines what kind of load balancer to provision (ipv4, dual stack)
	// +optional
	LoadBalancerIPType *LBIPType `json:"loadBalancerIPType,omitempty"`

	// DONE
	// loadBalancerSubnets an optional list of subnet ids or names to be used in the NLB
	// +optional
	LoadBalancerSubnets *[]SubnetConfiguration `json:"loadBalancerSubnets,omitempty"`

	// DONE
	// loadBalancerSecurityGroups an optional list of security group ids or names to apply to the NLB
	// +optional
	LoadBalancerSecurityGroups *[]string `json:"loadBalancerSecurityGroups,omitempty"`

	// DONE
	// loadBalancerSecurityGroupPrefixes an optional list of prefixes that are allowed to access the NLB.
	// +optional
	LoadBalancerSecurityGroupPrefixes *[]string `json:"loadBalancerSecurityGroupPrefixes,omitempty"`

	// DONE - But this is defined in the TG config as well.
	// loadBalancerSourceRanges an optional list of CIDRs that are allowed to access the NLB.
	// +optional
	LoadBalancerSourceRanges *[]string `json:"loadBalancerSourceRanges,omitempty"`

	// DONE
	// enableBackendSecurityGroupRules an optional boolean flag that controls whether the controller should automatically add the ingress rules to the instance/ENI security group.
	// +optional
	EnableBackendSecurityGroupRules *bool `json:"enableBackendSecurityGroupRules,omitempty"`

	// DONE
	// loadBalancerAttributes an optional map of attributes to apply to the load balancer.
	// +optional
	LoadBalancerAttributes map[string]string `json:"loadBalancerAttributes,omitempty"`

	// DONE
	// listenerAttributes an optional map of attributes that maps listeners protocol + port to their attributes
	// +optional
	ListenerAttributes map[string]map[string]string `json:"listenerAttributes,omitempty"`

	// DONE
	// extraResourceTags an optional map of additional tags to apply to AWS resources.
	// +optional
	ExtraResourceTags map[string]string `json:"extraResourceTags,omitempty"`

	// DONE
	// sslConfiguration an optional map that maps listener port to TLS settings.
	// +optional
	SSLConfiguration map[string]SSLConfiguration `json:"sslConfiguration,omitempty"`

	// DONE
	// loadBalancerAttributes optional access log configuration for the load balancer.
	// +optional
	AccessLogConfiguration *AccessLogConfiguration `json:"accessLogConfiguration,omitempty"`

	// DONE
	// crossZoneLoadBalancingEnabled optional boolean that toggles routing across availability zones.
	// +optional
	EnableCrossZoneLoadBalancing *bool `json:"enableCrossZoneLoadBalancing,omitempty"`

	// multiClusterEnabled optional boolean that toggles multi cluster capabilities
	// +optional
	MultiClusterEnabled *bool `json:"MultiClusterEnabled,omitempty"`
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
