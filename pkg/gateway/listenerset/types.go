package listenerset

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ListenerSetLoader loads and validates ListenerSets for a Gateway.
type ListenerSetLoader interface {
	// LoadListenerSetsForGateway returns all ListenerSets that are accepted
	// by the given Gateway's allowedListeners policy.
	LoadListenerSetsForGateway(ctx context.Context, gw gwv1.Gateway) (*ListenerSetLoadResult, error)
}

// ListenerMerger merges Gateway listeners with ListenerSet listeners.
type ListenerMerger interface {
	// MergeListeners combines parent Gateway listeners with ListenerSet listeners,
	// applying precedence rules and detecting conflicts.
	MergeListeners(gw gwv1.Gateway, listenerSets []ListenerSetData) *MergeResult
}

// ListenerSetStatusManager handles status updates for ListenerSets and
// the Gateway's attachedListenerSets field.
type ListenerSetStatusManager interface {
	// UpdateListenerSetStatuses updates the status of all ListenerSets
	// (both accepted and rejected) and the Gateway's attachedListenerSets count.
	UpdateListenerSetStatuses(
		ctx context.Context,
		gw *gwv1.Gateway,
		acceptedSets []ListenerSetData,
		rejectedSets []RejectedListenerSet,
		conflictMap map[ListenerKey]ConflictInfo,
		isProgrammed bool,
	) error
}

// ListenerSetLoadResult holds the results of loading ListenerSets for a Gateway.
type ListenerSetLoadResult struct {
	// AcceptedSets contains ListenerSets that passed the handshake validation.
	AcceptedSets []ListenerSetData
	// RejectedSets contains ListenerSets that failed validation, with reasons.
	RejectedSets []RejectedListenerSet
}

// ListenerSetData holds an accepted ListenerSet and its listeners.
type ListenerSetData struct {
	ListenerSet *gwv1.ListenerSet
	Listeners   []gwv1.ListenerEntry
}

// RejectedListenerSet holds a rejected ListenerSet with the rejection reason.
type RejectedListenerSet struct {
	ListenerSet *gwv1.ListenerSet
	Reason      string
	Message     string
}

// MergeResult holds the output of merging Gateway and ListenerSet listeners.
type MergeResult struct {
	// MergedListeners is the final ordered list of all non-conflicted listeners.
	MergedListeners []MergedListener
	// ConflictMap tracks which listeners are conflicted and why.
	ConflictMap map[ListenerKey]ConflictInfo
}

// MergedListener represents a single listener in the merged list with its source.
type MergedListener struct {
	Listener gwv1.Listener
	// Source indicates where this listener came from.
	Source ListenerSource
}

// ListenerSource identifies the origin of a listener (Gateway or ListenerSet).
type ListenerSource struct {
	// Kind is "Gateway" or "ListenerSet".
	Kind string
	// Name is the name of the source resource.
	Name string
	// Namespace is the namespace of the source resource.
	Namespace string
	// CreationTimestamp for precedence ordering.
	CreationTimestamp metav1.Time
}

// ListenerKey uniquely identifies a listener by port and hostname for conflict tracking.
type ListenerKey struct {
	Port     int32
	Hostname string
}

// ConflictInfo describes a listener conflict and which listener won.
type ConflictInfo struct {
	Reason       gwv1.ListenerConditionReason
	Message      string
	WinnerSource ListenerSource
}

// listenerOrigin tracks the source of each listener for precedence resolution.
type listenerOrigin struct {
	// SourceKind: "Gateway" or "ListenerSet"
	SourceKind string
	// SourceName: name of the Gateway or ListenerSet
	SourceName string
	// SourceNamespace: namespace of the source
	SourceNamespace string
	// CreationTimestamp: for ordering ListenerSets
	CreationTimestamp metav1.Time
	// ListenerIndex: position within the source's listener list
	ListenerIndex int
}

// toListenerSource converts a listenerOrigin to a ListenerSource.
func (o listenerOrigin) toListenerSource() ListenerSource {
	return ListenerSource{
		Kind:              o.SourceKind,
		Name:              o.SourceName,
		Namespace:         o.SourceNamespace,
		CreationTimestamp: o.CreationTimestamp,
	}
}

// precedenceKey groups listeners that may conflict.
type precedenceKey struct {
	Port     gwv1.PortNumber
	Protocol gwv1.ProtocolType
	Hostname string
}

// listenerWithOrigin pairs a listener with its origin metadata.
type listenerWithOrigin struct {
	listener gwv1.Listener
	origin   listenerOrigin
}
