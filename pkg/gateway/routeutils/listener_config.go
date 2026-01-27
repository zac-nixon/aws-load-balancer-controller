package routeutils

import (
	"sort"

	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ListenerSource identifies where a listener configuration originated from.
// This allows the system to track the source of listener configurations and
// supports future extension to additional listener sources beyond Gateway.
type ListenerSource string

const (
	// ListenerSourceGateway indicates the listener came from a Gateway object
	ListenerSourceGateway ListenerSource = "Gateway"
)

// ListenerEntry represents a single listener's configuration
type ListenerEntry struct {
	// Port is the port number for this listener
	Port int32

	// Protocol is the protocol for this listener (HTTP, HTTPS, TCP, UDP, TLS)
	Protocol gwv1.ProtocolType

	// SectionName is the name of the listener section from the Gateway
	SectionName gwv1.SectionName

	// Hostname is the optional hostname for this listener
	Hostname *gwv1.Hostname

	// Source identifies where this listener configuration came from
	Source ListenerSource
}

// ListenerConfig holds all extracted listener configurations
type ListenerConfig struct {
	// Entries contains all listener entries indexed by port
	// Multiple entries can exist for the same port (e.g., different section names)
	Entries map[int32][]ListenerEntry
}

// NewListenerConfig creates a new empty ListenerConfig
func NewListenerConfig() *ListenerConfig {
	return &ListenerConfig{
		Entries: make(map[int32][]ListenerEntry),
	}
}

// AddEntry adds a listener entry to the config
func (lc *ListenerConfig) AddEntry(entry ListenerEntry) {
	lc.Entries[entry.Port] = append(lc.Entries[entry.Port], entry)
}

// GetEntriesForPort returns all listener entries for a given port
func (lc *ListenerConfig) GetEntriesForPort(port int32) []ListenerEntry {
	return lc.Entries[port]
}

// GetAllPorts returns all ports that have listener entries in sorted order
func (lc *ListenerConfig) GetAllPorts() []int32 {
	ports := make([]int32, 0, len(lc.Entries))
	for port := range lc.Entries {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool {
		return ports[i] < ports[j]
	})
	return ports
}
