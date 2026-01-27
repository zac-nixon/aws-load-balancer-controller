package routeutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestNewListenerConfig(t *testing.T) {
	t.Run("creates empty config with initialized map", func(t *testing.T) {
		config := NewListenerConfig()

		assert.NotNil(t, config)
		assert.NotNil(t, config.Entries)
		assert.Empty(t, config.Entries)
	})
}

func TestListenerConfig_AddEntry(t *testing.T) {
	hostname := gwv1.Hostname("example.com")

	testCases := []struct {
		name           string
		entries        []ListenerEntry
		expectedPorts  []int32
		expectedCounts map[int32]int
	}{
		{
			name: "add single entry",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Hostname:    nil,
					Source:      ListenerSourceGateway,
				},
			},
			expectedPorts:  []int32{80},
			expectedCounts: map[int32]int{80: 1},
		},
		{
			name: "add multiple entries on different ports",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Hostname:    nil,
					Source:      ListenerSourceGateway,
				},
				{
					Port:        443,
					Protocol:    gwv1.HTTPSProtocolType,
					SectionName: "https-listener",
					Hostname:    &hostname,
					Source:      ListenerSourceGateway,
				},
			},
			expectedPorts:  []int32{80, 443},
			expectedCounts: map[int32]int{80: 1, 443: 1},
		},
		{
			name: "add multiple entries on same port",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-1",
					Hostname:    nil,
					Source:      ListenerSourceGateway,
				},
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-2",
					Hostname:    &hostname,
					Source:      ListenerSourceGateway,
				},
			},
			expectedPorts:  []int32{80},
			expectedCounts: map[int32]int{80: 2},
		},
		{
			name: "add entries with mixed ports",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Source:      ListenerSourceGateway,
				},
				{
					Port:        443,
					Protocol:    gwv1.HTTPSProtocolType,
					SectionName: "https-listener",
					Source:      ListenerSourceGateway,
				},
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-2",
					Source:      ListenerSourceGateway,
				},
			},
			expectedPorts:  []int32{80, 443},
			expectedCounts: map[int32]int{80: 2, 443: 1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := NewListenerConfig()

			for _, entry := range tc.entries {
				config.AddEntry(entry)
			}

			// Verify the correct number of ports
			assert.Equal(t, len(tc.expectedPorts), len(config.Entries))

			// Verify entry counts per port
			for port, expectedCount := range tc.expectedCounts {
				entries := config.Entries[port]
				assert.Equal(t, expectedCount, len(entries), "port %d should have %d entries", port, expectedCount)
			}
		})
	}
}

func TestListenerConfig_GetEntriesForPort(t *testing.T) {
	hostname1 := gwv1.Hostname("example.com")
	hostname2 := gwv1.Hostname("test.com")

	testCases := []struct {
		name            string
		entries         []ListenerEntry
		queryPort       int32
		expectedEntries []ListenerEntry
	}{
		{
			name:            "get entries for non-existent port returns nil",
			entries:         []ListenerEntry{},
			queryPort:       80,
			expectedEntries: nil,
		},
		{
			name: "get entries for existing port with single entry",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Hostname:    nil,
					Source:      ListenerSourceGateway,
				},
			},
			queryPort: 80,
			expectedEntries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Hostname:    nil,
					Source:      ListenerSourceGateway,
				},
			},
		},
		{
			name: "get entries for existing port with multiple entries",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-1",
					Hostname:    &hostname1,
					Source:      ListenerSourceGateway,
				},
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-2",
					Hostname:    &hostname2,
					Source:      ListenerSourceGateway,
				},
			},
			queryPort: 80,
			expectedEntries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-1",
					Hostname:    &hostname1,
					Source:      ListenerSourceGateway,
				},
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-2",
					Hostname:    &hostname2,
					Source:      ListenerSourceGateway,
				},
			},
		},
		{
			name: "get entries for specific port when multiple ports exist",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Source:      ListenerSourceGateway,
				},
				{
					Port:        443,
					Protocol:    gwv1.HTTPSProtocolType,
					SectionName: "https-listener",
					Source:      ListenerSourceGateway,
				},
			},
			queryPort: 443,
			expectedEntries: []ListenerEntry{
				{
					Port:        443,
					Protocol:    gwv1.HTTPSProtocolType,
					SectionName: "https-listener",
					Source:      ListenerSourceGateway,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := NewListenerConfig()

			for _, entry := range tc.entries {
				config.AddEntry(entry)
			}

			result := config.GetEntriesForPort(tc.queryPort)

			assert.Equal(t, tc.expectedEntries, result)
		})
	}
}

func TestListenerConfig_GetAllPorts(t *testing.T) {
	testCases := []struct {
		name          string
		entries       []ListenerEntry
		expectedPorts []int32
	}{
		{
			name:          "empty config returns empty slice",
			entries:       []ListenerEntry{},
			expectedPorts: []int32{},
		},
		{
			name: "single port",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Source:      ListenerSourceGateway,
				},
			},
			expectedPorts: []int32{80},
		},
		{
			name: "multiple unique ports are sorted",
			entries: []ListenerEntry{
				{
					Port:        443,
					Protocol:    gwv1.HTTPSProtocolType,
					SectionName: "https-listener",
					Source:      ListenerSourceGateway,
				},
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener",
					Source:      ListenerSourceGateway,
				},
				{
					Port:        8080,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "alt-http-listener",
					Source:      ListenerSourceGateway,
				},
			},
			expectedPorts: []int32{80, 443, 8080},
		},
		{
			name: "multiple entries on same port returns unique ports",
			entries: []ListenerEntry{
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-1",
					Source:      ListenerSourceGateway,
				},
				{
					Port:        80,
					Protocol:    gwv1.HTTPProtocolType,
					SectionName: "http-listener-2",
					Source:      ListenerSourceGateway,
				},
				{
					Port:        443,
					Protocol:    gwv1.HTTPSProtocolType,
					SectionName: "https-listener",
					Source:      ListenerSourceGateway,
				},
			},
			expectedPorts: []int32{80, 443},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := NewListenerConfig()

			for _, entry := range tc.entries {
				config.AddEntry(entry)
			}

			result := config.GetAllPorts()

			assert.Equal(t, tc.expectedPorts, result)
		})
	}
}

func TestListenerEntry_Fields(t *testing.T) {
	hostname := gwv1.Hostname("example.com")

	t.Run("entry preserves all fields correctly", func(t *testing.T) {
		entry := ListenerEntry{
			Port:        8443,
			Protocol:    gwv1.HTTPSProtocolType,
			SectionName: "secure-listener",
			Hostname:    &hostname,
			Source:      ListenerSourceGateway,
		}

		assert.Equal(t, int32(8443), entry.Port)
		assert.Equal(t, gwv1.HTTPSProtocolType, entry.Protocol)
		assert.Equal(t, gwv1.SectionName("secure-listener"), entry.SectionName)
		assert.NotNil(t, entry.Hostname)
		assert.Equal(t, gwv1.Hostname("example.com"), *entry.Hostname)
		assert.Equal(t, ListenerSourceGateway, entry.Source)
	})

	t.Run("entry with nil hostname", func(t *testing.T) {
		entry := ListenerEntry{
			Port:        80,
			Protocol:    gwv1.HTTPProtocolType,
			SectionName: "http-listener",
			Hostname:    nil,
			Source:      ListenerSourceGateway,
		}

		assert.Nil(t, entry.Hostname)
	})
}

func TestListenerSource_Constants(t *testing.T) {
	t.Run("ListenerSourceGateway has correct value", func(t *testing.T) {
		assert.Equal(t, ListenerSource("Gateway"), ListenerSourceGateway)
	})
}
