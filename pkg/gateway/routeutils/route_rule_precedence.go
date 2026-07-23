package routeutils

import (
	"math"
	"sort"
	"strings"
	"time"

	v1 "sigs.k8s.io/gateway-api/apis/v1"
)

type RulePrecedence struct {
	CommonRulePrecedence CommonRulePrecedence
	HTTPMatch            *v1.HTTPRouteMatch
	GRPCMatch            *v1.GRPCRouteMatch

	// PrecedenceFactor holds the specificity signals used to order this match.
	// A single struct is used for both HTTPRoute and GRPCRoute rules so that one
	// comparator can order every rule with a single uniform key — which is a
	// strict weak ordering, as sort.Slice requires.
	PrecedenceFactor *RulePrecedenceFactor
}

// RulePrecedenceFactor captures the per-match specificity signals for either
// route kind. The two length fields are populated differently per kind so that
// one uniform comparison key preserves each kind's Gateway API precedence:
//
//	         | PathLength (primary) | SecondaryLength | HasMethod          | QueryParamCount
//	HTTPRoute| path length          | 0               | method match set   | query param count
//	GRPCRoute| service length       | method length   | false              | 0
type RulePrecedenceFactor struct {
	PathType        int  // HTTP: 3=exact,2=prefix,1=regex,0=none. GRPC: 3=exact,1=regex,0=none.
	PathLength      int  // primary match length: HTTP path length / GRPC service length
	SecondaryLength int  // secondary match length: GRPC method length (0 for HTTP)
	HasMethod       bool // HTTP method match present (always false for GRPC)
	HeaderCount     int  // header match count
	QueryParamCount int  // HTTP query param count (always 0 for GRPC)
}

type CommonRulePrecedence struct {
	RouteDescriptor RouteDescriptor
	Rule            RouteRule

	// common rule precedence factors
	Hostnames            []string // raw hostnames from route, unsorted
	RouteNamespacedName  string
	RuleIndexInRoute     int // index of the rule in the route
	MatchIndexInRule     int // index of the match in the rule
	RouteCreateTimestamp time.Time
}

func SortAllRulesByPrecedence(routes []RouteDescriptor, port int32) []RulePrecedence {
	var allRoutes []RulePrecedence
	var httpRoutes []RulePrecedence
	var grpcRoutes []RulePrecedence

	for _, route := range routes {
		routeInfo := getCommonRouteInfo(route, port)
		for ruleIndex, rule := range route.GetAttachedRules() {
			rawRule := rule.GetRawRouteRule()
			switch r := rawRule.(type) {
			case *v1.HTTPRouteRule:
				for matchIndex, httpMatch := range r.Matches {
					common := routeInfo
					common.Rule = rule
					common.RuleIndexInRoute = ruleIndex
					common.MatchIndexInRule = matchIndex
					match := RulePrecedence{
						HTTPMatch:            &httpMatch,
						PrecedenceFactor:     &RulePrecedenceFactor{},
						CommonRulePrecedence: common,
					}
					// populate PrecedenceFactor from the HTTP match
					getHttpMatchPrecedenceInfo(&httpMatch, &match)
					httpRoutes = append(httpRoutes, match)
				}
				if len(r.Matches) == 0 {
					common := routeInfo
					common.Rule = rule
					common.RuleIndexInRoute = ruleIndex
					common.MatchIndexInRule = math.MaxInt
					match := RulePrecedence{
						HTTPMatch:            &v1.HTTPRouteMatch{},
						PrecedenceFactor:     &RulePrecedenceFactor{},
						CommonRulePrecedence: common,
					}
					httpRoutes = append(httpRoutes, match)
				}
			case *v1.GRPCRouteRule:
				for matchIndex, grpcMatch := range r.Matches {
					common := routeInfo
					common.Rule = rule
					common.RuleIndexInRoute = ruleIndex
					common.MatchIndexInRule = matchIndex
					match := RulePrecedence{
						GRPCMatch:            &grpcMatch,
						PrecedenceFactor:     &RulePrecedenceFactor{},
						CommonRulePrecedence: common,
					}
					// populate PrecedenceFactor from the GRPC match
					getGrpcMatchPrecedenceInfo(&grpcMatch, &match)
					grpcRoutes = append(grpcRoutes, match)
				}

				if len(r.Matches) == 0 {
					common := routeInfo
					common.Rule = rule
					common.RuleIndexInRoute = ruleIndex
					common.MatchIndexInRule = math.MaxInt
					match := RulePrecedence{
						GRPCMatch:            &v1.GRPCRouteMatch{},
						PrecedenceFactor:     &RulePrecedenceFactor{},
						CommonRulePrecedence: common,
					}
					grpcRoutes = append(grpcRoutes, match)
				}
			}
		}
	}

	allRoutes = append(allRoutes, httpRoutes...)
	allRoutes = append(allRoutes, grpcRoutes...)

	// Sort every rule — HTTP or GRPC — with a single unified comparator. Using
	// one uniform key for all pairs (rather than separate same-kind and
	// cross-kind comparators) guarantees a strict weak ordering, which
	// sort.Slice requires.
	sort.Slice(allRoutes, func(i, j int) bool {
		return compareRulePrecedenceUnified(allRoutes[i], allRoutes[j])
	})

	return allRoutes
}

// getHostnamePrecedenceOrder Hostname precedence ordering rule:
// 1. non-wildcard has higher precedence than wildcard
// 2. hostname with longer characters have higher precedence than those with shorter ones
// -1 means hostnameOne has higher precedence, 1 means hostnameTwo has higher precedence, 0 means equal
func getHostnamePrecedenceOrder(hostnameOne, hostnameTwo string) int {
	isHostnameOneWildcard := strings.HasPrefix(hostnameOne, "*.")
	isHostnameTwoWildcard := strings.HasPrefix(hostnameTwo, "*.")

	if !isHostnameOneWildcard && isHostnameTwoWildcard {
		return -1
	} else if isHostnameOneWildcard && !isHostnameTwoWildcard {
		return 1
	} else {
		dotsInHostnameOne := strings.Count(hostnameOne, ".")
		dotsInHostnameTwo := strings.Count(hostnameTwo, ".")
		if dotsInHostnameOne > dotsInHostnameTwo {
			return -1
		} else if dotsInHostnameOne < dotsInHostnameTwo {
			return 1
		}
		if len(hostnameOne) > len(hostnameTwo) {
			return -1
		} else if len(hostnameOne) < len(hostnameTwo) {
			return 1
		} else {
			return 0
		}
	}
}

// mostSpecificHostname returns the highest-precedence (most specific) hostname
// from a non-empty list without mutating the input slice. Callers must not pass
// an empty list.
func mostSpecificHostname(hostnames []string) string {
	best := hostnames[0]
	for _, h := range hostnames[1:] {
		if getHostnamePrecedenceOrder(h, best) < 0 {
			best = h
		}
	}
	return best
}

// getHostnameListPrecedenceOrder tiebreaks two routes based on hostname precedence.
// -1 means hostnameListOne has higher precedence, 1 means hostnameListTwo has higher
// precedence, 0 means equal (tiebreak continues on path/header/etc.).
//
// An empty hostname list is a catch-all: it matches every hostname and is therefore
// the least specific, so it always has lower precedence than any non-empty list.
//
// For two non-empty lists, precedence is decided by the single most-specific hostname
// each list contributes — a route is only as specific as its most specific hostname,
// and the number of hostnames in a list does not affect how specifically any given
// request matches. Reducing each list to one representative keeps this comparator a
// strict weak ordering (transitive), which sort.Slice requires. The previous
// implementation compared lists positionally and then broke ties by list length; that
// is both non-transitive (lists of differing length that tie on their shared prefix are
// each "equal" to a shorter list but not to each other) and semantically wrong (it lets
// hostname-list length dominate path specificity, so a catch-all path on a route with
// more hostnames could outrank a specific path on a route with fewer hostnames).
// Returning 0 on a genuine tie lets precedence fall through to path/header criteria,
// matching the Gateway API precedence ordering (matching hostname specificity, then
// path, then headers, ...).
func getHostnameListPrecedenceOrder(hostnameListOne, hostnameListTwo []string) int {
	oneEmpty := len(hostnameListOne) == 0
	twoEmpty := len(hostnameListTwo) == 0
	if oneEmpty || twoEmpty {
		switch {
		case oneEmpty && twoEmpty:
			return 0
		case oneEmpty:
			return 1 // two (non-empty) is more specific
		default:
			return -1 // one (non-empty) is more specific
		}
	}
	return getHostnamePrecedenceOrder(
		mostSpecificHostname(hostnameListOne),
		mostSpecificHostname(hostnameListTwo),
	)
}

// compareRulePrecedenceUnified is the single comparator used to order every
// rule — HTTP or GRPC — by one uniform specificity key. Returns true when
// ruleOne has higher precedence than ruleTwo (i.e. should sort earlier and be
// assigned a lower/first ALB priority).
//
// Using one key for all pairs (rather than separate same-kind and cross-kind
// comparators) guarantees a strict weak ordering, which sort.Slice requires.
// The earlier design compared same-kind GRPC rules by (serviceLength,
// methodLength) as separate keys while comparing cross-kind rules by their sum,
// which was non-transitive and produced an undefined, input-order-dependent ALB
// rule priority.
//
// Key, most significant first:
//  1. Hostname specificity (most-specific matching hostname)
//  2. Path match type (Exact=3 > Prefix=2 > Regex=1 > none=0)
//  3. Primary match length   (HTTP: path length;    GRPC: service length)
//  4. Secondary match length  (HTTP: 0;             GRPC: method length)
//  5. HTTP method constraint  (HTTP: hasMethod;     GRPC: always false)
//  6. Header match count
//  7. Query param count       (HTTP: query params;  GRPC: always 0)
//  8. Common tiebreakers (creation timestamp, namespaced name, rule/match index)
//
// Because each kind's "foreign" fields are constant within that kind
// (SecondaryLength is always 0 for HTTP, HasMethod always false and
// QueryParamCount always 0 for GRPC), this reproduces the previous intra-kind
// HTTP and GRPC orderings exactly while making cross-kind ordering consistent.
// In particular GRPC service and method remain separate keys (steps 3 and 4),
// preserving the Gateway API GRPC precedence (service characters, then method
// characters).
func compareRulePrecedenceUnified(ruleOne, ruleTwo RulePrecedence) bool {
	precedence := getHostnameListPrecedenceOrder(ruleOne.CommonRulePrecedence.Hostnames, ruleTwo.CommonRulePrecedence.Hostnames)
	if precedence != 0 {
		return precedence < 0 // -1 means first hostname has higher precedence
	}

	one := ruleOne.PrecedenceFactor
	two := ruleTwo.PrecedenceFactor

	// path match type (exact > prefix > regex > none)
	if one.PathType != two.PathType {
		return one.PathType > two.PathType
	}
	// primary match length (HTTP path length / GRPC service length)
	if one.PathLength != two.PathLength {
		return one.PathLength > two.PathLength
	}
	// secondary match length (GRPC method length; 0 for HTTP)
	if one.SecondaryLength != two.SecondaryLength {
		return one.SecondaryLength > two.SecondaryLength
	}
	// HTTP method constraint (always false for GRPC)
	if one.HasMethod != two.HasMethod {
		return one.HasMethod
	}
	// header match count
	if one.HeaderCount != two.HeaderCount {
		return one.HeaderCount > two.HeaderCount
	}
	// query param count (0 for GRPC)
	if one.QueryParamCount != two.QueryParamCount {
		return one.QueryParamCount > two.QueryParamCount
	}
	return compareCommonTieBreakers(ruleOne, ruleTwo)
}

func compareCommonTieBreakers(ruleOne RulePrecedence, ruleTwo RulePrecedence) bool {
	// compare creation timestamp
	if !ruleOne.CommonRulePrecedence.RouteCreateTimestamp.Equal(ruleTwo.CommonRulePrecedence.RouteCreateTimestamp) {
		return ruleOne.CommonRulePrecedence.RouteCreateTimestamp.Before(ruleTwo.CommonRulePrecedence.RouteCreateTimestamp)
	}
	// compare namespaced name (namespace/name) in alphabetic order
	if ruleOne.CommonRulePrecedence.RouteNamespacedName != ruleTwo.CommonRulePrecedence.RouteNamespacedName {
		return ruleOne.CommonRulePrecedence.RouteNamespacedName < ruleTwo.CommonRulePrecedence.RouteNamespacedName
	}
	// compare rule index in route
	if ruleOne.CommonRulePrecedence.RuleIndexInRoute != ruleTwo.CommonRulePrecedence.RuleIndexInRoute {
		return ruleOne.CommonRulePrecedence.RuleIndexInRoute < ruleTwo.CommonRulePrecedence.RuleIndexInRoute
	}
	// compare match index within rule
	return ruleOne.CommonRulePrecedence.MatchIndexInRule < ruleTwo.CommonRulePrecedence.MatchIndexInRule
}

func getCommonRouteInfo(route RouteDescriptor, port int32) CommonRulePrecedence {
	routeNamespacedName := route.GetRouteNamespacedName().String()
	routeCreateTimestamp := route.GetRouteCreateTimestamp()
	// Use compatible hostnames computed during route attachment
	compatibleHostnamesByPort := route.GetCompatibleHostnamesByPort()[port]
	hostnames := make([]string, 0)
	for _, h := range compatibleHostnamesByPort {
		hostnames = append(hostnames, string(h))
	}
	// If no compatible hostnames, use route hostnames
	if len(hostnames) == 0 {
		for _, h := range route.GetHostnames() {
			hostnames = append(hostnames, string(h))
		}
	}
	return CommonRulePrecedence{
		RouteDescriptor:      route,
		Hostnames:            hostnames,
		RouteCreateTimestamp: routeCreateTimestamp,
		RouteNamespacedName:  routeNamespacedName,
	}
}

func getHttpMatchPrecedenceInfo(httpMatch *v1.HTTPRouteMatch, matchPrecedence *RulePrecedence) {
	matchPrecedence.PrecedenceFactor.PathType = getHttpRoutePathType(httpMatch.Path)
	// httpMatch.Path.Value won't be nil, default is /
	matchPrecedence.PrecedenceFactor.PathLength = len(*httpMatch.Path.Value)
	matchPrecedence.PrecedenceFactor.HasMethod = httpMatch.Method != nil
	matchPrecedence.PrecedenceFactor.HeaderCount = len(httpMatch.Headers)
	matchPrecedence.PrecedenceFactor.QueryParamCount = len(httpMatch.QueryParams)
	// SecondaryLength stays 0 for HTTP.
}

// getHttpRoutePathType returns path type
// the higher priority path type has higher value
// Exact = 3, Prefix = 2, RegularExpression = 1
func getHttpRoutePathType(path *v1.HTTPPathMatch) int {
	if path == nil {
		return 0
	}
	switch *path.Type {
	case v1.PathMatchExact:
		return 3
	case v1.PathMatchPathPrefix:
		return 2
	case v1.PathMatchRegularExpression:
		return 1
	default:
		return 0
	}
}

// getGrpcMatchPrecedenceInfo populates the unified PrecedenceFactor from a GRPC
// match: service length is the primary length, method length the secondary,
// preserving the Gateway API GRPC precedence (service characters, then method
// characters). HasMethod stays false and QueryParamCount stays 0 for GRPC.
func getGrpcMatchPrecedenceInfo(grpcMatch *v1.GRPCRouteMatch, matchPrecedence *RulePrecedence) {
	matchPrecedence.PrecedenceFactor.PathType = getGrpcRoutePathType(grpcMatch.Method)
	matchPrecedence.PrecedenceFactor.HeaderCount = len(grpcMatch.Headers)
	if grpcMatch.Method != nil {
		if grpcMatch.Method.Service != nil {
			matchPrecedence.PrecedenceFactor.PathLength = len(*grpcMatch.Method.Service)
		}
		if grpcMatch.Method.Method != nil {
			matchPrecedence.PrecedenceFactor.SecondaryLength = len(*grpcMatch.Method.Method)
		}
	}
}

// getGrpcRoutePathType returns path type for grpc
func getGrpcRoutePathType(method *v1.GRPCMethodMatch) int {
	if method == nil {
		return 0
	}
	switch *method.Type {
	case v1.GRPCMethodMatchExact:
		return 3
	case v1.GRPCMethodMatchRegularExpression:
		return 1
	default:
		return 0
	}
}
