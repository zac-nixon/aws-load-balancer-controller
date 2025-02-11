package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
)

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

// Reference defines how to look up the Target Group configuration for a service.
type Reference struct {
	// Group is the group of the referent. For example, "gateway.networking.k8s.io".
	// When unspecified or empty string, core API group is inferred.
	//
	// +optional
	// +kubebuilder:default=""
	Group *string `json:"group,omitempty"`

	// Kind is the Kubernetes resource kind of the referent. For example
	// "Service".
	//
	// Defaults to "Service" when not specified.
	//
	// ExternalName services can refer to CNAME DNS records that may live
	// outside of the cluster and as such are difficult to reason about in
	// terms of conformance. They also may not be safe to forward to (see
	// CVE-2021-25740 for more information). Implementations SHOULD NOT
	// support ExternalName Services.
	//
	// Support: Core (Services with a type other than ExternalName)
	//
	// Support: Implementation-specific (Services with type ExternalName)
	//
	// +optional
	// +kubebuilder:default=Service
	Kind *string `json:"kind,omitempty"`

	// Name is the name of the referent.
	Name string `json:"name"`
}

// HealthCheckConfiguration defines the Health Check configuration for a Target Group.
type HealthCheckConfiguration struct {
	// healthCheckProtocol ....
	// +optional
	HealthCheckProtocol *elbv2model.Protocol `json:"healthCheckProtocol,omitempty"`

	// healthCheckPath ....
	// +optional
	HealthCheckPath *string `json:"healthCheckPath,omitempty"`

	// healthCheckCodes ....
	// +optional
	HealthCheckCodes []string `json:"healthCheckCodes,omitempty"`

	// healthCheckPort ....
	// +optional
	HealthCheckPort *string `json:"healthCheckPort,omitempty"`

	// healthCheckInterval ....
	// +optional
	HealthCheckInterval *int32 `json:"healthCheckInterval,omitempty"`

	// healthCheckTimeout ....
	// +optional
	HealthCheckTimeout *int32 `json:"healthCheckTimeout,omitempty"`

	// healthCheckHealthyThreshold ....
	// +optional
	HealthCheckHealthyThreshold *int32 `json:"healthCheckHealthyThreshold,omitempty"`

	// healthCheckUnHealthyThreshold ....
	// +optional
	HealthCheckUnHealthyThreshold *int32 `json:"healthCheckUnHealthyThreshold,omitempty"`
}

// TargetGroupConfigurationSpec defines the TargetGroup properties for a route.
type TargetGroupConfigurationSpec struct {

	// targetReference the kubernetes object to attach the Target Group settings to.
	TargetReference Reference `json:"targetReference"`

	// healthCheckConfig The Health Check configuration for this backend.
	// +optional
	HealthCheckConfig *HealthCheckConfiguration `json:"healthCheckConfig,omitempty"`

	// nodeSelector The node selector used for instance type target groups.
	// +optional
	NodeSelector *map[string]string `json:"instanceNodeSelector,omitempty"`

	// targetGroupAttributes ....
	// +optional
	TargetGroupAttributes *map[string]string `json:"targetGroupAttributes,omitempty"`

	// enableProxyProtocol ....
	EnableProxyProtocol *bool `json:"enableProxyProtocol,omitempty"`

	// manageBackendSecurityGroupRules flag to enable / disable the addition of rules to the cluster SG. The default is true.
	// +optional
	ManageBackendSecurityGroupRules *bool `json:"manageBackendSecurityGroupRules,omitempty"`

	// LoadBalancerSourceRanges restricts traffic through the load-balancer to only the specified client IPs.
	// +optional
	LoadBalancerSourceRanges *[]string `json:"loadBalancerSourceRanges,omitempty"`

	// TODO -- ADD ENUMS!
	// targetType defaults to instance.
	// +optional
	TargetType *string `json:"targetType,omitempty"`
}

// TODO -- these can be used to set what generation the gateway is currently on to track progress on reconcile.

// TargetGroupConfigurationStatus defines the observed state of TargetGroupConfiguration
type TargetGroupConfigurationStatus struct {
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

// TargetGroupConfiguration is the Schema for defining TargetGroups with an AWS ELB Gateway
type TargetGroupConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TargetGroupConfigurationSpec   `json:"spec,omitempty"`
	Status TargetGroupConfigurationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TargetGroupConfigurationList contains a list of TargetGroupConfiguration
type TargetGroupConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TargetGroupConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TargetGroupConfiguration{}, &TargetGroupConfigurationList{})
}
