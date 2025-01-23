package common

type NetworkingProtocol string

const (
	// NetworkingProtocolTCP is the TCP protocol.
	NetworkingProtocolTCP NetworkingProtocol = "TCP"

	// NetworkingProtocolUDP is the UDP protocol.
	NetworkingProtocolUDP NetworkingProtocol = "UDP"
)

type ControllerRoute interface {
	GetServiceRefs() []*ServiceRef
	GetProtocol() NetworkingProtocol
}

type RouteDescriptor struct {
	Route       interface{}
	Protocol    NetworkingProtocol
	ServiceRefs []ServiceRef
}
