package listenerutils

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
	gateway_constants "sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/constants"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type listenerParentKind string

const (
	listenerParentKindGateway     = "Gateway"
	listenerParentKindListenerSet = "ListenerSet"
)

type listenerSource struct {
	parent             interface{}
	parentCreationTime metav1.Time
	parentKind         listenerParentKind
	listener           gwv1.Listener
}

type validatedListenerSource struct {
	ListenerSource listenerSource
	Validation     ListenerValidationResult
}

type ListenerValidationResult struct {
	ListenerName   gwv1.SectionName
	IsValid        bool
	Reason         gwv1.ListenerConditionReason
	Message        string
	SupportedKinds []gwv1.RouteGroupKind
}

type ListenerValidationResults struct {
	Results   map[gwv1.SectionName]ListenerValidationResult
	HasErrors bool
}

// validateListeners validates all listeners configurations in a Gateway against controller-specific requirements.
// it is different from listener <-> route validation
// It checks for supported route kinds, valid port ranges (1-65535), controller-compatible protocols
// (ALB: HTTP/HTTPS/GRPC, NLB: TCP/UDP/TLS), protocol conflicts on same ports (except TCP+UDP),
// hostname conflicts - same port trying to use same hostname
func validateListeners(listeners map[listenerParentKind][]listenerSource, controllerName string) ListenerValidationResults {
	results := ListenerValidationResults{
		Results: make(map[gwv1.SectionName]ListenerValidationResult),
	}

	if len(listeners) == 0 {
		return results
	}

	portHostnameMap := make(map[string]bool)
	portProtocolMap := make(map[gwv1.PortNumber]gwv1.ProtocolType)

	// Gateway Listeners have priority over ListenerSet listeners, so we process them first.
	gatewayListeners, ok := listeners[listenerParentKindGateway]
	if ok {
		validateListenerList(gatewayListeners, portHostnameMap, portProtocolMap, controllerName)
	}

	listenerSetListeners, ok := listeners[listenerParentKindListenerSet]
	if ok {
		/*
			Listeners should be merged using the following precedence:

			    1. "parent" Gateway
			    2. ListenerSet ordered by creation time (oldest first)
			    3. ListenerSet ordered alphabetically by “{namespace}/{name}”.

		*/
		// .....
		sort.SliceStable(listenerSetListeners, func(i, j int) bool {
			return listenerSetListeners[i].parentCreationTime.Unix() < listenerSetListeners[j].parentCreationTime.Unix()
		})
		validateListenerList(listenerSetListeners, portHostnameMap, portProtocolMap, controllerName)
	}

	return results
}

func validateListenerList(listeners []listenerSource, portHostnameMap map[string]bool, portProtocolMap map[gwv1.PortNumber]gwv1.ProtocolType, controllerName string) {
	validationResults := ListenerValidationResults{}
	for _, ls := range listeners {
		// check supported kinds
		supportedKinds, isKindSupported := getSupportedKinds(controllerName, ls.listener)
		result := ListenerValidationResult{
			ListenerName:   ls.listener.Name,
			IsValid:        true,
			Reason:         gwv1.ListenerReasonAccepted,
			Message:        gateway_constants.ListenerAcceptedMessage,
			SupportedKinds: supportedKinds,
		}

		if !isKindSupported {
			result.IsValid = false
			result.Reason = gwv1.ListenerReasonInvalidRouteKinds
			result.Message = fmt.Sprintf("Invalid route kind for listener %s", ls.listener.Name)
			results.HasErrors = true
		} else if ls.listener.Port < 1 || ls.listener.Port > 65535 {
			result.IsValid = false
			result.Reason = gwv1.ListenerReasonPortUnavailable
			result.Message = fmt.Sprintf("Port %d is not available (listener name %s)", ls.listener.Port, ls.listener.Name)
			results.HasErrors = true
		} else if controllerName == gateway_constants.ALBGatewayController &&
			(ls.listener.Protocol == gwv1.TCPProtocolType || ls.listener.Protocol == gwv1.UDPProtocolType || ls.listener.Protocol == gwv1.TLSProtocolType) {
			result.IsValid = false
			result.Reason = gwv1.ListenerReasonUnsupportedProtocol
			result.Message = fmt.Sprintf("Unsupported protocol %s for listener %s", ls.listener.Protocol, ls.listener.Name)
			results.HasErrors = true
		} else if controllerName == gateway_constants.NLBGatewayController &&
			(ls.listener.Protocol == gwv1.HTTPProtocolType || ls.listener.Protocol == gwv1.HTTPSProtocolType) {
			result.IsValid = false
			result.Reason = gwv1.ListenerReasonUnsupportedProtocol
			result.Message = fmt.Sprintf("Unsupported protocol %s for listener %s", ls.listener.Protocol, ls.listener.Name)
			results.HasErrors = true
		} else {
			// Check protocol conflicts - same port with different protocols (except TCP+UDP)
			if existingProtocol, exists := portProtocolMap[ls.listener.Port]; exists {
				if existingProtocol != ls.listener.Protocol {
					if !((existingProtocol == gwv1.TCPProtocolType && ls.listener.Protocol == gwv1.UDPProtocolType) ||
						(existingProtocol == gwv1.UDPProtocolType && ls.listener.Protocol == gwv1.TCPProtocolType)) {
						result.IsValid = false
						result.Reason = gwv1.ListenerReasonProtocolConflict
						result.Message = fmt.Sprintf("Protocol conflict for port %d", ls.listener.Port)
						results.HasErrors = true
					}
				}
			} else {
				portProtocolMap[ls.listener.Port] = ls.listener.Protocol
			}

			// Check hostname conflicts - only when hostname is specified
			if ls.listener.Hostname != nil {
				hostname := *ls.listener.Hostname
				key := fmt.Sprintf("%d-%s", ls.listener.Port, hostname)

				if portHostnameMap[key] {
					result.IsValid = false
					result.Reason = gwv1.ListenerReasonHostnameConflict
					result.Message = fmt.Sprintf("Hostname conflict for port %d with hostname %s", ls.listener.Port, hostname)
					results.HasErrors = true
				} else {
					portHostnameMap[key] = true
				}
			}
		}

		results.Results[ls.listener.Name] = result
	}
}

func getSupportedKinds(controllerName string, listener gwv1.Listener) ([]gwv1.RouteGroupKind, bool) {
	supportedKinds := []gwv1.RouteGroupKind{}
	groupName := gateway_constants.GatewayResourceGroupName
	isKindSupported := true
	// we are allowing empty AllowedRoutes.Kinds
	if listener.AllowedRoutes == nil || listener.AllowedRoutes.Kinds == nil || len(listener.AllowedRoutes.Kinds) == 0 {
		allowedRoutes := sets.New[constants.RouteKind](DefaultProtocolToRouteKindMap[listener.Protocol]...)
		for _, routeKind := range allowedRoutes.UnsortedList() {
			supportedKinds = append(supportedKinds, gwv1.RouteGroupKind{
				Group: (*gwv1.Group)(&groupName),
				Kind:  gwv1.Kind(routeKind),
			})
		}
	}
	for _, routeGroup := range listener.AllowedRoutes.Kinds {
		if controllerName == gateway_constants.ALBGatewayController {
			if string(routeGroup.Kind) == string(constants.HTTPRouteKind) || string(routeGroup.Kind) == string(constants.GRPCRouteKind) {
				supportedKinds = append(supportedKinds, gwv1.RouteGroupKind{
					Group: (*gwv1.Group)(&groupName),
					Kind:  routeGroup.Kind,
				})
			} else {
				isKindSupported = false
			}
		}
		if controllerName == gateway_constants.NLBGatewayController {
			if string(routeGroup.Kind) == string(constants.TCPRouteKind) || string(routeGroup.Kind) == string(constants.TLSRouteKind) || string(routeGroup.Kind) == string(constants.UDPRouteKind) {
				supportedKinds = append(supportedKinds, gwv1.RouteGroupKind{
					Group: (*gwv1.Group)(&groupName),
					Kind:  routeGroup.Kind,
				})
			} else {
				isKindSupported = false
			}
		}
	}

	return supportedKinds, isKindSupported
}

func retrieveGatewayListeners(ctx context.Context, gw gwv1.Gateway, lsLoader listenerSetLoader) ([]listenerSource, []*gwv1.ListenerSet, error) {
	listenerSources := make([]listenerSource, 0)
	for i := range gw.Spec.Listeners {
		listenerSources = append(listenerSources, listenerSource{
			parent:             gw,
			parentKind:         listenerParentKindGateway,
			parentCreationTime: gw.CreationTimestamp,
			listener:           gw.Spec.Listeners[i],
		})
	}

	listenerSetListenerSources, rejectedListenerSets, err := lsLoader.retrieveListenersFromListenerSets(ctx, gw)
	if err != nil {
		return nil, err
	}

	return listenerSources
}
