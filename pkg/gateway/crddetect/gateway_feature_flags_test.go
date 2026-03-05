package crddetect

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
)

// mockDiscoveryClient implements k8s.DiscoveryClient for testing.
type mockDiscoveryClient struct {
	resources map[string]*metav1.APIResourceList
	err       error
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if m.err != nil {
		return nil, m.err
	}
	res, ok := m.resources[groupVersion]
	if !ok {
		return nil, fmt.Errorf("group version %q not found", groupVersion)
	}
	return res, nil
}

func TestApplyGatewayFeatureFlags_StandardMissing(t *testing.T) {
	fg := config.NewFeatureGates()
	assert.True(t, fg.Enabled(config.ALBGatewayAPI), "precondition: ALBGatewayAPI should default to true")
	assert.True(t, fg.Enabled(config.NLBGatewayAPI), "precondition: NLBGatewayAPI should default to true")

	standardResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1",
		AllPresent:   false,
		MissingKinds: []string{"HTTPRoute"},
	}
	experimentalResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1alpha2",
		AllPresent:   true,
	}

	ApplyGatewayFeatureFlags(standardResult, experimentalResult, fg, logr.Discard())

	assert.False(t, fg.Enabled(config.ALBGatewayAPI), "ALBGatewayAPI should be disabled when standard CRDs are missing")
	assert.False(t, fg.Enabled(config.NLBGatewayAPI), "NLBGatewayAPI should be disabled when standard CRDs are missing")
}

func TestApplyGatewayFeatureFlags_ExperimentalMissing_StandardPresent(t *testing.T) {
	fg := config.NewFeatureGates()

	standardResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1",
		AllPresent:   true,
	}
	experimentalResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1alpha2",
		AllPresent:   false,
		MissingKinds: []string{"TCPRoute"},
	}

	ApplyGatewayFeatureFlags(standardResult, experimentalResult, fg, logr.Discard())

	assert.True(t, fg.Enabled(config.ALBGatewayAPI), "ALBGatewayAPI should remain enabled when standard CRDs are present")
	assert.False(t, fg.Enabled(config.NLBGatewayAPI), "NLBGatewayAPI should be disabled when experimental CRDs are missing")
}

func TestApplyGatewayFeatureFlags_BothPresent(t *testing.T) {
	fg := config.NewFeatureGates()

	standardResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1",
		AllPresent:   true,
	}
	experimentalResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1alpha2",
		AllPresent:   true,
	}

	ApplyGatewayFeatureFlags(standardResult, experimentalResult, fg, logr.Discard())

	assert.True(t, fg.Enabled(config.ALBGatewayAPI), "ALBGatewayAPI should remain enabled")
	assert.True(t, fg.Enabled(config.NLBGatewayAPI), "NLBGatewayAPI should remain enabled")
}

func TestApplyGatewayFeatureFlags_BothMissing(t *testing.T) {
	fg := config.NewFeatureGates()

	standardResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1",
		AllPresent:   false,
		MissingKinds: []string{"Gateway", "GatewayClass"},
	}
	experimentalResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1alpha2",
		AllPresent:   false,
		MissingKinds: []string{"TCPRoute", "UDPRoute"},
	}

	ApplyGatewayFeatureFlags(standardResult, experimentalResult, fg, logr.Discard())

	assert.False(t, fg.Enabled(config.ALBGatewayAPI), "ALBGatewayAPI should be disabled")
	assert.False(t, fg.Enabled(config.NLBGatewayAPI), "NLBGatewayAPI should be disabled")
}

func TestApplyGatewayFeatureFlags_ExplicitlyEnabledStillDisabledWhenCRDsMissing(t *testing.T) {
	fg := config.NewFeatureGates()
	// Simulate explicit --feature-gates ALBGatewayAPI=true,NLBGatewayAPI=true
	fg.Enable(config.ALBGatewayAPI)
	fg.Enable(config.NLBGatewayAPI)

	standardResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1",
		AllPresent:   false,
		MissingKinds: []string{"GRPCRoute"},
	}
	experimentalResult := k8s.CRDGroupResult{
		GroupVersion: "gateway.networking.k8s.io/v1alpha2",
		AllPresent:   true,
	}

	ApplyGatewayFeatureFlags(standardResult, experimentalResult, fg, logr.Discard())

	assert.False(t, fg.Enabled(config.ALBGatewayAPI), "ALBGatewayAPI should be force-disabled even when explicitly enabled")
	assert.False(t, fg.Enabled(config.NLBGatewayAPI), "NLBGatewayAPI should be force-disabled even when explicitly enabled")
}

func TestDetectListenerSetCRD_Present(t *testing.T) {
	client := &mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"gateway.networking.k8s.io/v1": {
				APIResources: []metav1.APIResource{
					{Kind: "Gateway"},
					{Kind: "GatewayClass"},
					{Kind: "HTTPRoute"},
					{Kind: "ListenerSet"},
				},
			},
		},
	}

	result := DetectListenerSetCRD(client, logr.Discard())
	assert.True(t, result, "should return true when ListenerSet CRD is present")
}

func TestDetectListenerSetCRD_Absent(t *testing.T) {
	client := &mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"gateway.networking.k8s.io/v1": {
				APIResources: []metav1.APIResource{
					{Kind: "Gateway"},
					{Kind: "GatewayClass"},
					{Kind: "HTTPRoute"},
				},
			},
		},
	}

	result := DetectListenerSetCRD(client, logr.Discard())
	assert.False(t, result, "should return false when ListenerSet CRD is absent")
}

func TestDetectListenerSetCRD_APIError(t *testing.T) {
	client := &mockDiscoveryClient{
		err: fmt.Errorf("connection refused"),
	}

	result := DetectListenerSetCRD(client, logr.Discard())
	assert.False(t, result, "should return false on API error (fail-closed)")
}
