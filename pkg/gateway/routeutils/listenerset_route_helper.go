package routeutils

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/listenerset"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/shared_constants"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// listenerSetRouteContext holds internal context for ListenerSet route processing.
// This is built inside the loader and never exposed to the reconciler.
type listenerSetRouteContext struct {
	// AcceptedListenerSets maps ListenerSet NamespacedName to its data.
	AcceptedListenerSets map[types.NamespacedName]listenerset.ListenerSetData
	// MergedListeners is the full merged listener list from the merger.
	MergedListeners []listenerset.MergedListener
	// ConflictMap tracks conflicted listeners.
	ConflictMap map[listenerset.ListenerKey]listenerset.ConflictInfo
}

// buildListenerSetRouteContext creates the internal context for ListenerSet route processing.
func buildListenerSetRouteContext(
	acceptedSets []listenerset.ListenerSetData,
	mergeResult *listenerset.MergeResult,
) *listenerSetRouteContext {
	acceptedMap := make(map[types.NamespacedName]listenerset.ListenerSetData, len(acceptedSets))
	for _, lsData := range acceptedSets {
		key := types.NamespacedName{
			Name:      lsData.ListenerSet.Name,
			Namespace: lsData.ListenerSet.Namespace,
		}
		acceptedMap[key] = lsData
	}
	return &listenerSetRouteContext{
		AcceptedListenerSets: acceptedMap,
		MergedListeners:      mergeResult.MergedListeners,
		ConflictMap:          mergeResult.ConflictMap,
	}
}

// loadRoutesForListenerSetParentRefs discovers routes with ListenerSet parentRefs,
// validates them against accepted ListenerSets, and maps them to merged listeners.
// This implements tasks 8.3, 8.4, 8.5, 8.6, and 8.7.
func (l *loaderImpl) loadRoutesForListenerSetParentRefs(
	ctx context.Context,
	acceptedSets []listenerset.ListenerSetData,
	mergeResult *listenerset.MergeResult,
	loadedRoutes []preLoadRouteDescriptor,
) (map[int32][]RouteDescriptor, []RouteData, map[types.NamespacedName][]RouteOrigin, error) {
	lsCtx := buildListenerSetRouteContext(acceptedSets, mergeResult)

	routesByPort := make(map[int32][]RouteDescriptor)
	routeOrigins := make(map[types.NamespacedName][]RouteOrigin)
	var statusUpdates []RouteData

	// Step 1: Filter routes that have at least one ListenerSet parentRef
	// We reuse the already-loaded routes from the main loader flow.
	for _, route := range loadedRoutes {
		parentRefs := route.GetParentRefs()
		routeNN := route.GetRouteNamespacedName()

		for _, parentRef := range parentRefs {
			if !isListenerSetParentRef(parentRef) {
				continue
			}

			lsKey := resolveListenerSetKey(parentRef, routeNN.Namespace)

			// Step 2: Validate ListenerSet is in AcceptedSets
			lsData, accepted := lsCtx.AcceptedListenerSets[lsKey]
			if !accepted {
				// Route references a non-accepted ListenerSet — reject
				statusUpdates = append(statusUpdates, generateListenerSetRouteData(
					false, true,
					string(gwv1.RouteReasonNotAllowedByListeners),
					"ListenerSet is not accepted by the parent Gateway",
					routeNN, route.GetRouteKind(), route.GetRouteGeneration(),
					lsKey, parentRef.Port, parentRef.SectionName,
				))
				continue
			}

			// Step 3: Resolve which listeners in this ListenerSet the route attaches to
			matchedListeners := resolveListenerSetRouteAttachment(parentRef, lsData, lsCtx)

			if len(matchedListeners) == 0 {
				// No matching listeners — NoMatchingParent
				statusUpdates = append(statusUpdates, generateListenerSetRouteData(
					false, true,
					string(gwv1.RouteReasonNoMatchingParent),
					"No matching listener found in ListenerSet",
					routeNN, route.GetRouteKind(), route.GetRouteGeneration(),
					lsKey, parentRef.Port, parentRef.SectionName,
				))
				continue
			}

			// Step 4: For each matched listener, validate and attach
			var attachedPorts []int32
			for _, ml := range matchedListeners {
				// 4a: Check if listener is conflicted (task 8.4)
				lKey := listenerset.ListenerKey{
					Port:     int32(ml.Listener.Port),
					Hostname: hostnameToString(ml.Listener.Hostname),
				}
				if _, conflicted := lsCtx.ConflictMap[lKey]; conflicted {
					// Listener is conflicted — do not attach
					continue
				}

				// 4b: Enforce allowedRoutes relative to ListenerSet namespace (task 8.5)
				if !isRouteAllowedByListenerSetListener(ctx, l.nsSelector, route, ml.Listener, lsData.ListenerSet.Namespace) {
					continue
				}

				// 4c: Check ReferenceGrant independence (task 8.6)
				if routeNN.Namespace != lsData.ListenerSet.Namespace {
					// TODO: Implement full ReferenceGrant checking for cross-namespace routes.
					// For now, cross-namespace routes to ListenerSets require a ReferenceGrant
					// in the ListenerSet's namespace. This requires listing ReferenceGrant resources
					// which will be implemented in a follow-up task.
					// Reject cross-namespace routes without ReferenceGrant validation.
					statusUpdates = append(statusUpdates, generateListenerSetRouteData(
						false, true,
						string(gwv1.RouteReasonRefNotPermitted),
						"No ReferenceGrant allows this cross-namespace attachment to ListenerSet",
						routeNN, route.GetRouteKind(), route.GetRouteGeneration(),
						lsKey, parentRef.Port, parentRef.SectionName,
					))
					continue
				}

				// 4d: Attach route to this merged listener's port
				port := int32(ml.Listener.Port)
				attachedPorts = append(attachedPorts, port)
			}

			// Step 5: If any ports matched, load child resources and record origin (task 8.7)
			if len(attachedPorts) > 0 {
				// Load child resources for this route
				generatedRoute, loadErrors := route.loadAttachedRules(ctx, l.k8sClient)
				if len(loadErrors) > 0 {
					for _, le := range loadErrors {
						if le.Fatal {
							return nil, nil, nil, le.Err
						}
					}
				}

				for _, port := range attachedPorts {
					routesByPort[port] = append(routesByPort[port], generatedRoute)
				}

				// Record route origin (task 8.7)
				routeOrigins[routeNN] = append(routeOrigins[routeNN], RouteOrigin{
					ParentRefKind:        shared_constants.ListenerSetKind,
					ParentRefName:        lsKey.Name,
					ParentRefNamespace:   lsKey.Namespace,
					SectionName:          parentRef.SectionName,
					MatchedListenerPorts: attachedPorts,
				})

				// Generate accepted status with ListenerSet parentRef (task 8.7)
				statusUpdates = append(statusUpdates, generateListenerSetRouteData(
					true, true,
					string(gwv1.RouteConditionAccepted),
					RouteStatusInfoAcceptedMessage,
					routeNN, route.GetRouteKind(), route.GetRouteGeneration(),
					lsKey, parentRef.Port, parentRef.SectionName,
				))
			}
		}
	}

	return routesByPort, statusUpdates, routeOrigins, nil
}

// isListenerSetParentRef checks if a parentRef targets a ListenerSet.
func isListenerSetParentRef(parentRef gwv1.ParentReference) bool {
	if parentRef.Kind == nil {
		return false // default kind is Gateway
	}
	return string(*parentRef.Kind) == shared_constants.ListenerSetKind
}

// resolveListenerSetKey extracts the NamespacedName of the ListenerSet from a parentRef.
func resolveListenerSetKey(parentRef gwv1.ParentReference, routeNamespace string) types.NamespacedName {
	namespace := routeNamespace
	if parentRef.Namespace != nil {
		namespace = string(*parentRef.Namespace)
	}
	return types.NamespacedName{
		Name:      string(parentRef.Name),
		Namespace: namespace,
	}
}

// resolveListenerSetRouteAttachment determines which listeners within a ListenerSet
// a route should attach to, based on the parentRef's sectionName.
// Returns ONLY listeners from the specified ListenerSet (never from Gateway or other ListenerSets).
func resolveListenerSetRouteAttachment(
	parentRef gwv1.ParentReference,
	lsData listenerset.ListenerSetData,
	lsCtx *listenerSetRouteContext,
) []listenerset.MergedListener {
	var matched []listenerset.MergedListener

	if parentRef.SectionName != nil {
		// Case 1: sectionName is set — match ONLY the named listener within this ListenerSet
		sectionName := string(*parentRef.SectionName)
		for _, entry := range lsData.Listeners {
			if string(entry.Name) == sectionName {
				ml := findMergedListenerForEntry(lsCtx.MergedListeners, entry, lsData.ListenerSet)
				if ml != nil {
					matched = append(matched, *ml)
				}
				return matched // sectionName is unique within a ListenerSet
			}
		}
		// sectionName not found — return nil (NoMatchingParent)
		return nil
	}

	// Case 2: sectionName is NOT set — match ALL listeners in this ListenerSet
	for _, entry := range lsData.Listeners {
		ml := findMergedListenerForEntry(lsCtx.MergedListeners, entry, lsData.ListenerSet)
		if ml != nil {
			matched = append(matched, *ml)
		}
	}

	return matched
}

// findMergedListenerForEntry looks up a ListenerSet listener entry in the merged listener list,
// matching by port+hostname and verifying the source is the expected ListenerSet.
func findMergedListenerForEntry(
	mergedListeners []listenerset.MergedListener,
	entry gwv1.ListenerEntry,
	ls *gwv1.ListenerSet,
) *listenerset.MergedListener {
	for i, ml := range mergedListeners {
		if ml.Listener.Port == entry.Port &&
			hostnameToString(ml.Listener.Hostname) == hostnameToString(entry.Hostname) &&
			ml.Source.Kind == shared_constants.ListenerSetKind &&
			ml.Source.Name == ls.Name &&
			ml.Source.Namespace == ls.Namespace {
			return &mergedListeners[i]
		}
	}
	return nil // Listener was likely conflicted and excluded from merged list
}

// isRouteAllowedByListenerSetListener checks the listener's allowedRoutes field
// with namespace filtering relative to the ListenerSet's namespace (not the Gateway's).
// This implements task 8.5.
func isRouteAllowedByListenerSetListener(
	ctx context.Context,
	nsSelector namespaceSelector,
	route preLoadRouteDescriptor,
	listener gwv1.Listener,
	listenerSetNamespace string,
) bool {
	routeNamespace := route.GetRouteNamespacedName().Namespace

	allowedRoutes := listener.AllowedRoutes
	if allowedRoutes == nil {
		// Default: Same namespace as the ListenerSet
		return routeNamespace == listenerSetNamespace
	}

	// Check route kind
	if len(allowedRoutes.Kinds) > 0 {
		kindAllowed := false
		for _, k := range allowedRoutes.Kinds {
			if RouteKind(k.Kind) == route.GetRouteKind() {
				kindAllowed = true
				break
			}
		}
		if !kindAllowed {
			return false
		}
	}

	// Check namespace — relative to the ListenerSet's namespace
	if allowedRoutes.Namespaces == nil || allowedRoutes.Namespaces.From == nil {
		// Default: Same namespace as the ListenerSet
		return routeNamespace == listenerSetNamespace
	}

	switch *allowedRoutes.Namespaces.From {
	case gwv1.NamespacesFromSame:
		return routeNamespace == listenerSetNamespace
	case gwv1.NamespacesFromAll:
		return true
	case gwv1.NamespacesFromSelector:
		if allowedRoutes.Namespaces.Selector == nil {
			return false
		}
		namespaces, err := nsSelector.getNamespacesFromSelector(ctx, allowedRoutes.Namespaces.Selector)
		if err != nil {
			return false
		}
		return namespaces.Has(routeNamespace)
	default:
		return false
	}
}

// generateListenerSetRouteData creates a RouteData with the parentRef pointing to
// the ListenerSet (not the Gateway). This implements task 8.7.
func generateListenerSetRouteData(
	accepted bool, resolvedRefs bool,
	reason string, message string,
	routeNN types.NamespacedName, routeKind RouteKind, routeGeneration int64,
	lsKey types.NamespacedName,
	port *gwv1.PortNumber, sectionName *gwv1.SectionName,
) RouteData {
	namespace := gwv1.Namespace(lsKey.Namespace)
	group := gwv1.Group(shared_constants.GatewayAPIResourcesGroup)
	kind := gwv1.Kind(shared_constants.ListenerSetKind)
	return RouteData{
		RouteStatusInfo: RouteStatusInfo{
			Accepted:     accepted,
			ResolvedRefs: resolvedRefs,
			Reason:       reason,
			Message:      message,
		},
		RouteMetadata: RouteMetadata{
			RouteName:       routeNN.Name,
			RouteNamespace:  routeNN.Namespace,
			RouteKind:       string(routeKind),
			RouteGeneration: routeGeneration,
		},
		ParentRef: gwv1.ParentReference{
			Group:       &group,
			Kind:        &kind,
			Name:        gwv1.ObjectName(lsKey.Name),
			Namespace:   &namespace,
			Port:        port,
			SectionName: sectionName,
		},
	}
}

// hostnameToString converts a *gwv1.Hostname to a string.
func hostnameToString(h *gwv1.Hostname) string {
	if h == nil {
		return ""
	}
	return string(*h)
}
