package crddetect

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
)

const (
	// GatewayV1GroupVersion is the stable Gateway API group version.
	GatewayV1GroupVersion = "gateway.networking.k8s.io/v1"
	// GatewayV1Alpha2GroupVersion is the experimental Gateway API group version.
	GatewayV1Alpha2GroupVersion = "gateway.networking.k8s.io/v1alpha2"
)

var (
	// StandardCRDKinds lists the CRD kinds required for ALB Gateway API support.
	StandardCRDKinds = []string{"Gateway", "GatewayClass", "HTTPRoute", "GRPCRoute"}
	// ExperimentalCRDKinds lists the CRD kinds required for NLB Gateway API support.
	ExperimentalCRDKinds = []string{"TCPRoute", "UDPRoute", "TLSRoute"}
	// ListenerSetCRDKinds lists the CRD kinds for ListenerSet support.
	// Kept separate from StandardCRDKinds because missing ListenerSet should NOT disable ALB/NLB Gateway API.
	ListenerSetCRDKinds = []string{"ListenerSet"}
)

// ApplyGatewayCRDDetection checks for the presence of Gateway API CRDs and
// disables the corresponding feature flags when required CRDs are missing.
// It is called from main() after the k8s client is ready and before any
// controller reads the feature flags.
func ApplyGatewayCRDDetection(client k8s.DiscoveryClient, featureGates config.FeatureGates, logger logr.Logger) {
	standardResult := k8s.DetectCRDs(client, GatewayV1GroupVersion, StandardCRDKinds)
	experimentalResult := k8s.DetectCRDs(client, GatewayV1Alpha2GroupVersion, ExperimentalCRDKinds)

	ApplyGatewayFeatureFlags(standardResult, experimentalResult, featureGates, logger)
}

// ApplyGatewayFeatureFlags applies the Gateway CRD detection results to the
// feature gates. Extracted for testability — accepts pre-computed results.
func ApplyGatewayFeatureFlags(standardResult, experimentalResult k8s.CRDGroupResult, featureGates config.FeatureGates, logger logr.Logger) {
	if !standardResult.AllPresent {
		logger.Info("Disabling ALBGatewayAPI: missing standard Gateway API CRDs",
			"groupVersion", standardResult.GroupVersion,
			"missing", standardResult.MissingKinds)
		featureGates.Disable(config.ALBGatewayAPI)
	} else {
		logger.Info("All standard Gateway API CRDs detected, ALBGatewayAPI remains enabled",
			"groupVersion", standardResult.GroupVersion)
	}

	if !standardResult.AllPresent || !experimentalResult.AllPresent {
		logger.Info("Disabling NLBGatewayAPI: missing required Gateway API CRDs",
			"missingStandard", standardResult.MissingKinds,
			"missingExperimental", experimentalResult.MissingKinds)
		featureGates.Disable(config.NLBGatewayAPI)
	} else {
		logger.Info("All required Gateway API CRDs detected, NLBGatewayAPI remains enabled",
			"standardGroupVersion", standardResult.GroupVersion,
			"experimentalGroupVersion", experimentalResult.GroupVersion)
	}
}

// DetectListenerSetCRD checks whether the ListenerSet CRD is installed in the
// cluster. It returns true when the CRD is present and false otherwise.
// Missing ListenerSet CRD does NOT disable any feature flags — the controller
// continues to operate normally, just without ListenerSet support.
func DetectListenerSetCRD(client k8s.DiscoveryClient, logger logr.Logger) bool {
	result := k8s.DetectCRDs(client, GatewayV1GroupVersion, ListenerSetCRDKinds)
	if !result.AllPresent {
		logger.Info("ListenerSet CRD not found, ListenerSet support will be disabled",
			"groupVersion", result.GroupVersion,
			"missing", result.MissingKinds)
		return false
	}
	logger.Info("ListenerSet CRD detected, ListenerSet support enabled",
		"groupVersion", result.GroupVersion)
	return true
}
