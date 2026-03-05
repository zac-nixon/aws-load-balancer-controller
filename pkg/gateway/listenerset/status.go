package listenerset

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ ListenerSetStatusManager = &listenerSetStatusManagerImpl{}

// listenerSetStatusManagerImpl implements ListenerSetStatusManager.
type listenerSetStatusManagerImpl struct {
	k8sClient client.Client
	logger    logr.Logger
}

// NewListenerSetStatusManager creates a new ListenerSetStatusManager.
func NewListenerSetStatusManager(k8sClient client.Client, logger logr.Logger) ListenerSetStatusManager {
	return &listenerSetStatusManagerImpl{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

// UpdateListenerSetStatuses updates the status of all ListenerSets
// (both accepted and rejected) and the Gateway's attachedListenerSets count.
func (s *listenerSetStatusManagerImpl) UpdateListenerSetStatuses(
	ctx context.Context,
	gw *gwv1.Gateway,
	acceptedSets []ListenerSetData,
	rejectedSets []RejectedListenerSet,
	conflictMap map[ListenerKey]ConflictInfo,
	isProgrammed bool,
) error {
	// Update status for each accepted ListenerSet.
	for _, lsData := range acceptedSets {
		if err := s.updateAcceptedListenerSetStatus(ctx, lsData, conflictMap, isProgrammed); err != nil {
			s.logger.Error(err, "Failed to update accepted ListenerSet status",
				"listenerSet", fmt.Sprintf("%s/%s", lsData.ListenerSet.Namespace, lsData.ListenerSet.Name))
			return err
		}
	}

	// Update status for each rejected ListenerSet.
	for _, rejected := range rejectedSets {
		if err := s.updateRejectedListenerSetStatus(ctx, rejected); err != nil {
			s.logger.Error(err, "Failed to update rejected ListenerSet status",
				"listenerSet", fmt.Sprintf("%s/%s", rejected.ListenerSet.Namespace, rejected.ListenerSet.Name))
			return err
		}
	}

	// Update Gateway status with attachedListenerSets count.
	if err := s.updateGatewayAttachedListenerSets(ctx, gw, int32(len(acceptedSets))); err != nil {
		s.logger.Error(err, "Failed to update Gateway attachedListenerSets count",
			"gateway", fmt.Sprintf("%s/%s", gw.Namespace, gw.Name))
		return err
	}

	return nil
}

// updateAcceptedListenerSetStatus sets top-level Accepted=True and Programmed
// conditions, plus per-listener conditions on an accepted ListenerSet.
func (s *listenerSetStatusManagerImpl) updateAcceptedListenerSetStatus(
	ctx context.Context,
	lsData ListenerSetData,
	conflictMap map[ListenerKey]ConflictInfo,
	isProgrammed bool,
) error {
	ls := lsData.ListenerSet
	lsOld := ls.DeepCopy()
	now := metav1.NewTime(time.Now())
	generation := ls.Generation

	// Set top-level Accepted=True.
	setListenerSetCondition(&ls.Status.Conditions, metav1.Condition{
		Type:               string(gwv1.ListenerSetConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gwv1.ListenerSetReasonAccepted),
		Message:            "ListenerSet is accepted by the Gateway",
		LastTransitionTime: now,
		ObservedGeneration: generation,
	})

	// Set top-level Programmed condition based on isProgrammed flag.
	if isProgrammed {
		setListenerSetCondition(&ls.Status.Conditions, metav1.Condition{
			Type:               string(gwv1.ListenerSetConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(gwv1.ListenerSetReasonProgrammed),
			Message:            "ListenerSet is programmed",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})
	} else {
		setListenerSetCondition(&ls.Status.Conditions, metav1.Condition{
			Type:               string(gwv1.ListenerSetConditionProgrammed),
			Status:             metav1.ConditionFalse,
			Reason:             string(gwv1.ListenerSetReasonPending),
			Message:            "Waiting for the Gateway to be programmed",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})
	}

	// Set per-listener conditions.
	ls.Status.Listeners = buildListenerEntryStatuses(lsData.Listeners, conflictMap, isProgrammed, generation, now)

	return s.k8sClient.Status().Patch(ctx, ls, client.MergeFrom(lsOld))
}

// updateRejectedListenerSetStatus sets top-level Accepted=False on a rejected ListenerSet.
func (s *listenerSetStatusManagerImpl) updateRejectedListenerSetStatus(
	ctx context.Context,
	rejected RejectedListenerSet,
) error {
	ls := rejected.ListenerSet
	lsOld := ls.DeepCopy()
	now := metav1.NewTime(time.Now())
	generation := ls.Generation

	// Set top-level Accepted=False with the rejection reason.
	setListenerSetCondition(&ls.Status.Conditions, metav1.Condition{
		Type:               string(gwv1.ListenerSetConditionAccepted),
		Status:             metav1.ConditionFalse,
		Reason:             rejected.Reason,
		Message:            rejected.Message,
		LastTransitionTime: now,
		ObservedGeneration: generation,
	})

	// Set top-level Programmed=False since the ListenerSet is not accepted.
	setListenerSetCondition(&ls.Status.Conditions, metav1.Condition{
		Type:               string(gwv1.ListenerSetConditionProgrammed),
		Status:             metav1.ConditionFalse,
		Reason:             string(gwv1.ListenerSetReasonInvalid),
		Message:            "ListenerSet is not accepted",
		LastTransitionTime: now,
		ObservedGeneration: generation,
	})

	return s.k8sClient.Status().Patch(ctx, ls, client.MergeFrom(lsOld))
}

// updateGatewayAttachedListenerSets updates the Gateway status with the
// attachedListenerSets count.
func (s *listenerSetStatusManagerImpl) updateGatewayAttachedListenerSets(
	ctx context.Context,
	gw *gwv1.Gateway,
	count int32,
) error {
	// Skip patch if the count is already correct.
	if gw.Status.AttachedListenerSets != nil && *gw.Status.AttachedListenerSets == count {
		return nil
	}

	gwOld := gw.DeepCopy()
	gw.Status.AttachedListenerSets = &count
	return s.k8sClient.Status().Patch(ctx, gw, client.MergeFrom(gwOld))
}

// buildListenerEntryStatuses builds per-listener status entries for an accepted
// ListenerSet, setting Accepted, Programmed, Conflicted, and ResolvedRefs conditions.
func buildListenerEntryStatuses(
	listeners []gwv1.ListenerEntry,
	conflictMap map[ListenerKey]ConflictInfo,
	isProgrammed bool,
	generation int64,
	now metav1.Time,
) []gwv1.ListenerEntryStatus {
	statuses := make([]gwv1.ListenerEntryStatus, 0, len(listeners))
	for _, entry := range listeners {
		lKey := ListenerKey{
			Port:     int32(entry.Port),
			Hostname: hostnameStr(entry.Hostname),
		}
		conflictInfo, isConflicted := conflictMap[lKey]

		var conditions []metav1.Condition

		// Accepted condition.
		conditions = append(conditions, metav1.Condition{
			Type:               string(gwv1.ListenerEntryConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gwv1.ListenerEntryReasonAccepted),
			Message:            "Listener is accepted",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})

		// Conflicted condition.
		if isConflicted {
			conditions = append(conditions, metav1.Condition{
				Type:               string(gwv1.ListenerEntryConditionConflicted),
				Status:             metav1.ConditionTrue,
				Reason:             string(conflictInfo.Reason),
				Message:            conflictInfo.Message,
				LastTransitionTime: now,
				ObservedGeneration: generation,
			})
		} else {
			conditions = append(conditions, metav1.Condition{
				Type:               string(gwv1.ListenerEntryConditionConflicted),
				Status:             metav1.ConditionFalse,
				Reason:             "NoConflicts",
				Message:            "Listener has no conflicts",
				LastTransitionTime: now,
				ObservedGeneration: generation,
			})
		}

		// Programmed condition.
		if isConflicted {
			conditions = append(conditions, metav1.Condition{
				Type:               string(gwv1.ListenerEntryConditionProgrammed),
				Status:             metav1.ConditionFalse,
				Reason:             string(gwv1.ListenerEntryReasonInvalid),
				Message:            "Listener is conflicted and cannot be programmed",
				LastTransitionTime: now,
				ObservedGeneration: generation,
			})
		} else if isProgrammed {
			conditions = append(conditions, metav1.Condition{
				Type:               string(gwv1.ListenerEntryConditionProgrammed),
				Status:             metav1.ConditionTrue,
				Reason:             string(gwv1.ListenerEntryReasonProgrammed),
				Message:            "Listener is programmed",
				LastTransitionTime: now,
				ObservedGeneration: generation,
			})
		} else {
			conditions = append(conditions, metav1.Condition{
				Type:               string(gwv1.ListenerEntryConditionProgrammed),
				Status:             metav1.ConditionFalse,
				Reason:             string(gwv1.ListenerEntryReasonPending),
				Message:            "Waiting for the Gateway to be programmed",
				LastTransitionTime: now,
				ObservedGeneration: generation,
			})
		}

		// ResolvedRefs condition.
		conditions = append(conditions, metav1.Condition{
			Type:               string(gwv1.ListenerEntryConditionResolvedRefs),
			Status:             metav1.ConditionTrue,
			Reason:             string(gwv1.ListenerEntryReasonResolvedRefs),
			Message:            "Listener references are resolved",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})

		statuses = append(statuses, gwv1.ListenerEntryStatus{
			Name:       entry.Name,
			Conditions: conditions,
		})
	}
	return statuses
}

// setListenerSetCondition sets or updates a condition in the given conditions slice.
func setListenerSetCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	if conditions == nil {
		return
	}
	for i, existing := range *conditions {
		if existing.Type == condition.Type {
			(*conditions)[i] = condition
			return
		}
	}
	*conditions = append(*conditions, condition)
}
