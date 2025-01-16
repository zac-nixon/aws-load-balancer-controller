package config

// NLBGatewayConfig contains the configurations for the Ingress controller
type NLBGatewayConfig struct {
	ControllerName string

	// Max concurrent reconcile loops for Ingress objects
	MaxConcurrentReconciles int
}
