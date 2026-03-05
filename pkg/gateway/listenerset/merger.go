package listenerset

import (
	"fmt"
	"sort"

	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ ListenerMerger = &listenerMergerImpl{}

// listenerMergerImpl implements ListenerMerger.
type listenerMergerImpl struct{}

// NewListenerMerger creates a new ListenerMerger.
func NewListenerMerger() ListenerMerger {
	return &listenerMergerImpl{}
}

// MergeListeners applies GEP-1713 precedence rules to combine Gateway listeners
// with ListenerSet listeners. Gateway listeners always have highest precedence,
// followed by older ListenerSets, with alphabetical name as tiebreaker.
func (m *listenerMergerImpl) MergeListeners(
	gw gwv1.Gateway, listenerSets []ListenerSetData,
) *MergeResult {
	result := &MergeResult{
		ConflictMap: make(map[ListenerKey]ConflictInfo),
	}

	// Step 1: Build ordered list of (listener, origin) tuples.
	// Gateway listeners first (highest precedence).
	var allListeners []listenerWithOrigin
	for i, l := range gw.Spec.Listeners {
		allListeners = append(allListeners, listenerWithOrigin{
			listener: l,
			origin: listenerOrigin{
				SourceKind:        "Gateway",
				SourceName:        gw.Name,
				SourceNamespace:   gw.Namespace,
				CreationTimestamp: gw.CreationTimestamp,
				ListenerIndex:     i,
			},
		})
	}

	// Step 2: Sort ListenerSets by creationTimestamp, then by name.
	sort.Slice(listenerSets, func(i, j int) bool {
		ti := listenerSets[i].ListenerSet.CreationTimestamp
		tj := listenerSets[j].ListenerSet.CreationTimestamp
		if !ti.Equal(&tj) {
			return ti.Before(&tj)
		}
		return listenerSets[i].ListenerSet.Name < listenerSets[j].ListenerSet.Name
	})

	// Step 3: Append ListenerSet listeners in precedence order.
	for _, lsData := range listenerSets {
		for i, entry := range lsData.Listeners {
			allListeners = append(allListeners, listenerWithOrigin{
				listener: convertEntryToListener(entry),
				origin: listenerOrigin{
					SourceKind:        "ListenerSet",
					SourceName:        lsData.ListenerSet.Name,
					SourceNamespace:   lsData.ListenerSet.Namespace,
					CreationTimestamp: lsData.ListenerSet.CreationTimestamp,
					ListenerIndex:     i,
				},
			})
		}
	}

	// Step 4: Detect conflicts using port+protocol+hostname grouping.
	// portMap tracks which listener index "owns" each precedenceKey.
	portMap := make(map[precedenceKey]int) // value = index into allListeners
	conflictedIndices := make(map[int]bool)

	for idx, lwo := range allListeners {
		key := precedenceKey{
			Port:     lwo.listener.Port,
			Protocol: lwo.listener.Protocol,
			Hostname: hostnameStr(lwo.listener.Hostname),
		}

		lKey := ListenerKey{
			Port:     int32(lwo.listener.Port),
			Hostname: hostnameStr(lwo.listener.Hostname),
		}

		if existingIdx, exists := portMap[key]; exists {
			// Exact match on port+protocol+hostname: hostname conflict.
			// The existing listener has higher precedence.
			// Gateway listeners are never marked as conflicted.
			if lwo.origin.SourceKind != "Gateway" {
				conflictedIndices[idx] = true
				result.ConflictMap[lKey] = ConflictInfo{
					Reason:       gwv1.ListenerReasonHostnameConflict,
					Message:      fmt.Sprintf("Conflicts with listener from %s/%s", allListeners[existingIdx].origin.SourceKind, allListeners[existingIdx].origin.SourceName),
					WinnerSource: allListeners[existingIdx].origin.toListenerSource(),
				}
			}
		} else {
			// Check for protocol conflict on same port with different protocol.
			conflicted := false
			for existingKey, existingIdx := range portMap {
				if existingKey.Port == key.Port && existingKey.Protocol != key.Protocol {
					if lwo.origin.SourceKind != "Gateway" {
						conflictedIndices[idx] = true
						result.ConflictMap[lKey] = ConflictInfo{
							Reason:       gwv1.ListenerReasonProtocolConflict,
							Message:      fmt.Sprintf("Protocol conflict on port %d with %s/%s", key.Port, allListeners[existingIdx].origin.SourceKind, allListeners[existingIdx].origin.SourceName),
							WinnerSource: allListeners[existingIdx].origin.toListenerSource(),
						}
						conflicted = true
					}
					break
				}
			}
			if !conflicted {
				portMap[key] = idx
			}
		}
	}

	// Step 5: Build merged listener list from non-conflicted listeners only.
	for idx, lwo := range allListeners {
		if !conflictedIndices[idx] {
			result.MergedListeners = append(result.MergedListeners, MergedListener{
				Listener: lwo.listener,
				Source:   lwo.origin.toListenerSource(),
			})
		}
	}

	return result
}

// convertEntryToListener converts a ListenerEntry to a Listener.
func convertEntryToListener(entry gwv1.ListenerEntry) gwv1.Listener {
	return gwv1.Listener{
		Name:          entry.Name,
		Hostname:      entry.Hostname,
		Port:          entry.Port,
		Protocol:      entry.Protocol,
		TLS:           entry.TLS,
		AllowedRoutes: entry.AllowedRoutes,
	}
}

// hostnameStr returns the string value of a Hostname pointer, or empty string if nil.
func hostnameStr(h *gwv1.Hostname) string {
	if h == nil {
		return ""
	}
	return string(*h)
}
