package routeutils

// This file adds fast, deterministic "ordering-contract" gates for the Gateway
// API rule-precedence comparator. They target the class of defect that reached
// production: a comparator that is NOT a strict weak ordering (SWO), which makes
// sort.Slice produce an undefined, input-order-dependent result, plus semantic
// ordering regressions (e.g. a catch-all outranking a more specific path).
//
// Three complementary gates:
//
//  1. SWO property tests (axiom-based):
//       - getHostnameListPrecedenceOrder (the exact function whose non-transitive
//         behavior caused the original production bug), as a three-way comparator.
//       - compareRulePrecedenceUnified over HTTP-only, GRPC-only, and MIXED inputs,
//         verifying the single comparator is a strict weak ordering.
//
//  2. Shuffle-determinism + stable/unstable agreement over the full end-to-end
//     SortAllRulesByPrecedence on realistic MIXED (HTTP+GRPC) inputs. This is the
//     black-box guard that reproduces the production symptom ("ALB order depends
//     on input order") and covers the cross-kind comparator without relying on
//     fragile synthetic domain collisions.
//
//  3. Golden ordering fixtures for SortAllRulesByPrecedence, including the exact
//     cross-route catch-all-vs-specific-path scenario from the gateway-api
//     conformance test httproute-matching-across-routes.

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func swoSign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}

var swoSeeds = []int64{1, 2, 3, 5, 8, 13, 21, 34}

var swoHostnamePool = []string{
	"example.com",
	"example.net",
	"a.example.com",
	"api.example.com",
	"foo.bar.example.com",
	"*.example.com",
	"*.b.example.com",
}

// randHostnameList returns a random hostname list of size 0..3 (size 0 == the
// catch-all case, which is exactly where the production bug lived).
func randHostnameList(rng *rand.Rand) []string {
	n := rng.Intn(4) // 0..3
	if n == 0 {
		return nil
	}
	perm := rng.Perm(len(swoHostnamePool))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, swoHostnamePool[perm[i]])
	}
	return out
}

type ruleKind int

const (
	kindHTTP ruleKind = iota
	kindGRPC
	kindMixed
)

var swoTimes = []time.Time{
	time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
}

var swoNames = []string{"ns-a/route-1", "ns-a/route-2", "ns-b/route-1"}

// genRandomRule builds a random, well-formed RulePrecedence. Exactly one of the
// kind-specific factors is set, matching how SortAllRulesByPrecedence constructs
// values. Domains are intentionally small so ties (and thus equivalence classes)
// actually occur, which is what makes the equivalence-transitivity axiom
// meaningful. matchIdx is threaded through as MatchIndexInRule to give each
// generated element the unique (route, rule, match) identity that production
// always has.
func genRandomRule(rng *rand.Rand, kind ruleKind, matchIdx int) RulePrecedence {
	common := CommonRulePrecedence{
		Hostnames:            randHostnameList(rng),
		RouteNamespacedName:  swoNames[rng.Intn(len(swoNames))],
		RouteCreateTimestamp: swoTimes[rng.Intn(len(swoTimes))],
		RuleIndexInRoute:     rng.Intn(2),
		MatchIndexInRule:     matchIdx,
	}

	isHTTP := kind == kindHTTP || (kind == kindMixed && rng.Intn(2) == 0)
	if isHTTP {
		return RulePrecedence{
			PrecedenceFactor: &RulePrecedenceFactor{
				PathType:        []int{0, 1, 2, 3}[rng.Intn(4)],
				PathLength:      []int{0, 1, 3, 5, 7}[rng.Intn(5)],
				HasMethod:       rng.Intn(2) == 0,
				HeaderCount:     rng.Intn(3),
				QueryParamCount: rng.Intn(2),
			},
			CommonRulePrecedence: common,
		}
	}
	// GRPC-shaped factor: service length is the primary length, method length the
	// secondary; HasMethod stays false and QueryParamCount stays 0.
	return RulePrecedence{
		PrecedenceFactor: &RulePrecedenceFactor{
			PathType:        []int{0, 1, 3}[rng.Intn(3)],
			PathLength:      []int{0, 3, 5}[rng.Intn(3)],
			SecondaryLength: []int{0, 3, 7}[rng.Intn(3)],
			HeaderCount:     rng.Intn(3),
		},
		CommonRulePrecedence: common,
	}
}

func genRandomRules(rng *rand.Rand, kind ruleKind, n int) []RulePrecedence {
	out := make([]RulePrecedence, n)
	for i := 0; i < n; i++ {
		out[i] = genRandomRule(rng, kind, i)
	}
	return out
}

// assertBoolComparatorIsSWO verifies the four axioms a `less` comparator must
// satisfy to be a strict weak ordering (what sort.Slice requires):
//   - irreflexivity:            !less(a,a)
//   - asymmetry:                less(a,b) => !less(b,a)
//   - transitivity of <:        less(a,b) && less(b,c) => less(a,c)
//   - transitivity of ~ (equal): eq(a,b) && eq(b,c) => eq(a,c), where
//     eq(a,b) := !less(a,b) && !less(b,a)
//
// A violation is reported with the concrete indices so failures are debuggable.
func assertBoolComparatorIsSWO(t *testing.T, items []RulePrecedence, less func(a, b RulePrecedence) bool, label func(i int) string) {
	t.Helper()
	n := len(items)

	// Precompute the pairwise "less" matrix once.
	lm := make([][]bool, n)
	for i := 0; i < n; i++ {
		lm[i] = make([]bool, n)
		for j := 0; j < n; j++ {
			lm[i][j] = less(items[i], items[j])
		}
	}
	eq := func(i, j int) bool { return !lm[i][j] && !lm[j][i] }

	for i := 0; i < n; i++ {
		if lm[i][i] {
			t.Fatalf("irreflexivity violated: less(x,x)=true for %s", label(i))
		}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if lm[i][j] && lm[j][i] {
				t.Fatalf("asymmetry violated: less(a,b) && less(b,a)\n a=%s\n b=%s", label(i), label(j))
			}
		}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if !lm[i][j] {
				continue
			}
			for k := 0; k < n; k++ {
				if lm[j][k] && !lm[i][k] {
					t.Fatalf("transitivity of < violated: less(a,b) && less(b,c) but !less(a,c)\n a=%s\n b=%s\n c=%s",
						label(i), label(j), label(k))
				}
			}
		}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if !eq(i, j) {
				continue
			}
			for k := 0; k < n; k++ {
				if eq(j, k) && !eq(i, k) {
					t.Fatalf("transitivity of equivalence violated: eq(a,b) && eq(b,c) but !eq(a,c)\n a=%s\n b=%s\n c=%s",
						label(i), label(j), label(k))
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 1. SWO property tests for the sub-comparators
// ---------------------------------------------------------------------------

// Test_getHostnameListPrecedenceOrder_SWO_Property randomly generates hostname
// lists (including empty catch-all lists) and verifies the three-way comparator
// is a strict weak ordering. This is the direct regression guard for the
// production bug, where an empty list compared "equal" to every list, making the
// induced equivalence relation non-transitive.
func Test_getHostnameListPrecedenceOrder_SWO_Property(t *testing.T) {
	cmp := getHostnameListPrecedenceOrder

	for _, seed := range swoSeeds {
		rng := rand.New(rand.NewSource(seed))
		const n = 80
		lists := make([][]string, n)
		for i := range lists {
			lists[i] = randHostnameList(rng)
		}
		label := func(i int) string { return fmt.Sprintf("%v", lists[i]) }

		// Reflexivity of equality and antisymmetry.
		for i := 0; i < n; i++ {
			if cmp(lists[i], lists[i]) != 0 {
				t.Fatalf("seed %d: cmp(x,x) != 0 for %s", seed, label(i))
			}
			for j := 0; j < n; j++ {
				if swoSign(cmp(lists[i], lists[j])) != -swoSign(cmp(lists[j], lists[i])) {
					t.Fatalf("seed %d: antisymmetry violated\n a=%s\n b=%s", seed, label(i), label(j))
				}
			}
		}
		// Transitivity of strict order and of equivalence.
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				sij := swoSign(cmp(lists[i], lists[j]))
				for k := 0; k < n; k++ {
					sjk := swoSign(cmp(lists[j], lists[k]))
					sik := swoSign(cmp(lists[i], lists[k]))
					if sij < 0 && sjk < 0 && !(sik < 0) {
						t.Fatalf("seed %d: strict transitivity violated\n a=%s\n b=%s\n c=%s", seed, label(i), label(j), label(k))
					}
					if sij == 0 && sjk == 0 && sik != 0 {
						t.Fatalf("seed %d: equivalence transitivity violated\n a=%s\n b=%s\n c=%s", seed, label(i), label(j), label(k))
					}
				}
			}
		}
	}
}

// Test_compareRulePrecedenceUnified_SWO_Property_HTTPOnly verifies the unified
// comparator is a strict weak ordering across randomized HTTP-shaped rules. The
// equivalence-transitivity axiom exercises the hostname key: a regression that
// makes hostname comparison non-transitive (the original production bug) breaks
// this test.
func Test_compareRulePrecedenceUnified_SWO_Property_HTTPOnly(t *testing.T) {
	for _, seed := range swoSeeds {
		rng := rand.New(rand.NewSource(seed))
		rules := genRandomRules(rng, kindHTTP, 60)
		assertBoolComparatorIsSWO(t, rules, compareRulePrecedenceUnified, func(i int) string {
			return fmt.Sprintf("seed %d idx %d %s", seed, i, describeRule(rules[i]))
		})
	}
}

// Test_compareRulePrecedenceUnified_SWO_Property_GRPCOnly verifies the unified
// comparator is a strict weak ordering across randomized GRPC-shaped rules.
func Test_compareRulePrecedenceUnified_SWO_Property_GRPCOnly(t *testing.T) {
	for _, seed := range swoSeeds {
		rng := rand.New(rand.NewSource(seed))
		rules := genRandomRules(rng, kindGRPC, 60)
		assertBoolComparatorIsSWO(t, rules, compareRulePrecedenceUnified, func(i int) string {
			return fmt.Sprintf("seed %d idx %d %s", seed, i, describeRule(rules[i]))
		})
	}
}

// Test_compareRulePrecedenceUnified_SWO_Property verifies that the single
// comparator handed to sort.Slice in SortAllRulesByPrecedence is a strict weak
// ordering across MIXED HTTP/GRPC inputs — the property sort.Slice requires.
//
// This is the regression gate for the cross-kind ordering bug. The earlier design
// used separate same-kind and cross-kind comparators: same-kind GRPC ranked by
// (serviceLength, methodLength) as separate keys, while the cross-kind path summed
// them into one effective length. Those disagreed, so a mixed triple could cycle.
// Minimal counterexample (same most-specific hostname and path type):
//
//	a = GRPC{service=5, method=0}
//	b = GRPC{service=0, method=7}
//	c = HTTP{pathLength=7}
//	old: less(a,b) (service 5>0), less(b,c) (sum 8>7), !less(a,c) (sum 6<7)  => a<b<c but c<a
//
// The unified comparator uses one key for every pair (service length as the primary
// length, method length as the secondary), which is a strict weak ordering by
// construction, so this test now passes.
func Test_compareRulePrecedenceUnified_SWO_Property(t *testing.T) {
	seeds := []int64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233}
	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		rules := genRandomRules(rng, kindMixed, 120)
		assertBoolComparatorIsSWO(t, rules, compareRulePrecedenceUnified, func(i int) string {
			return fmt.Sprintf("seed %d idx %d %s", seed, i, describeRule(rules[i]))
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Shuffle-determinism + stable/unstable agreement (end-to-end, mixed kinds)
// ---------------------------------------------------------------------------

// Test_SortAllRulesByPrecedence_ShuffleDeterministic reproduces the production
// symptom directly: if the comparator is not a strict weak ordering, sort.Slice
// yields an order that depends on the input permutation. We sort many shuffles
// of the same realistic MIXED (HTTP+GRPC) input and assert the flattened output
// order is identical every time. As an extra SWO check we also assert
// sort.Slice and sort.SliceStable agree (they can diverge for a non-SWO less).
func Test_SortAllRulesByPrecedence_ShuffleDeterministic(t *testing.T) {
	for _, seed := range swoSeeds {
		rng := rand.New(rand.NewSource(seed))
		input := genRandomRouteSet(rng, 10)

		baseline := keysOf(SortAllRulesByPrecedence(cloneRoutes(input), 0))

		for shuffle := 0; shuffle < 40; shuffle++ {
			shuffled := cloneRoutes(input)
			rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			got := keysOf(SortAllRulesByPrecedence(shuffled, 0))
			if !equalKeys(baseline, got) {
				t.Fatalf("seed %d shuffle %d: sort order depends on input order (comparator not a strict weak ordering)\n baseline=%v\n got     =%v",
					seed, shuffle, baseline, got)
			}
		}

		// sort.Slice vs sort.SliceStable must agree for a valid SWO comparator.
		flat := SortAllRulesByPrecedence(cloneRoutes(input), 0)
		a := append([]RulePrecedence(nil), flat...)
		b := append([]RulePrecedence(nil), flat...)
		rng.Shuffle(len(a), func(i, j int) { a[i], a[j] = a[j], a[i] })
		copy(b, a)
		sort.Slice(a, func(i, j int) bool { return compareRulePrecedenceUnified(a[i], a[j]) })
		sort.SliceStable(b, func(i, j int) bool { return compareRulePrecedenceUnified(b[i], b[j]) })
		if !equalKeys(rpKeys(a), rpKeys(b)) {
			t.Fatalf("seed %d: sort.Slice and sort.SliceStable disagree (comparator not a strict weak ordering)\n slice =%v\n stable=%v",
				seed, rpKeys(a), rpKeys(b))
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Golden ordering fixtures
// ---------------------------------------------------------------------------

// Test_SortAllRulesByPrecedence_Golden_MatchingAcrossRoutes encodes the exact
// gateway-api conformance scenario httproute-matching-across-routes as a fast
// unit test. part1 has TWO hostnames (example.com, example.net) and part2 has
// ONE (example.com); the merged-PR bug let part1's larger hostname list outrank
// part2, pushing part2's specific "/v2" path below part1's catch-all "/". The
// correct Gateway API order compares hostname specificity first (a tie here) and
// then PATH, so the "/v2" rule must be the highest-precedence rule.
func Test_SortAllRulesByPrecedence_Golden_MatchingAcrossRoutes(t *testing.T) {
	// part1 created earlier than part2 so ties break deterministically toward part1.
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	pathPrefix := (*gwv1.PathMatchType)(awssdk.String("PathPrefix"))

	part1 := &httpRouteDescription{
		route: &gwv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "matching-part1",
				Namespace:         "gateway-conformance-infra",
				CreationTimestamp: metav1.Time{Time: t1},
			},
			Spec: gwv1.HTTPRouteSpec{
				Hostnames: []gwv1.Hostname{"example.com", "example.net"},
			},
		},
		rules: []RouteRule{
			&convertedHTTPRouteRule{
				rule: &gwv1.HTTPRouteRule{
					Matches: []gwv1.HTTPRouteMatch{
						// m0: path "/" (catch-all)
						{Path: &gwv1.HTTPPathMatch{Type: pathPrefix, Value: awssdk.String("/")}},
						// m1: path "/" (defaulted) + header version=one
						{
							Path:    &gwv1.HTTPPathMatch{Type: pathPrefix, Value: awssdk.String("/")},
							Headers: []gwv1.HTTPHeaderMatch{{Name: "version", Value: "one"}},
						},
					},
				},
			},
		},
	}

	part2 := &httpRouteDescription{
		route: &gwv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "matching-part2",
				Namespace:         "gateway-conformance-infra",
				CreationTimestamp: metav1.Time{Time: t2},
			},
			Spec: gwv1.HTTPRouteSpec{
				Hostnames: []gwv1.Hostname{"example.com"},
			},
		},
		rules: []RouteRule{
			&convertedHTTPRouteRule{
				rule: &gwv1.HTTPRouteRule{
					Matches: []gwv1.HTTPRouteMatch{
						// m0: path "/v2" (specific)
						{Path: &gwv1.HTTPPathMatch{Type: pathPrefix, Value: awssdk.String("/v2")}},
						// m1: path "/" (defaulted) + header version=two
						{
							Path:    &gwv1.HTTPPathMatch{Type: pathPrefix, Value: awssdk.String("/")},
							Headers: []gwv1.HTTPHeaderMatch{{Name: "version", Value: "two"}},
						},
					},
				},
			},
		},
	}

	// Feed in shuffled order to also exercise sort robustness.
	input := []RouteDescriptor{part2, part1}
	result := SortAllRulesByPrecedence(input, 0)

	assert.Equal(t, 4, len(result), "should produce 4 rules (2 matches per route)")

	type want struct {
		name     string
		matchIdx int
		pathLen  int
		headers  int
	}
	// Correct Gateway API precedence order (highest first):
	expected := []want{
		{"gateway-conformance-infra/matching-part2", 0, 3, 0}, // "/v2"           -> most specific path
		{"gateway-conformance-infra/matching-part1", 1, 1, 1}, // "/" + version=one
		{"gateway-conformance-infra/matching-part2", 1, 1, 1}, // "/" + version=two (ties, later route)
		{"gateway-conformance-infra/matching-part1", 0, 1, 0}, // "/" catch-all   -> least specific
	}
	for i, w := range expected {
		got := result[i]
		assert.Equalf(t, w.name, got.CommonRulePrecedence.RouteNamespacedName, "priority %d route", i+1)
		assert.Equalf(t, w.matchIdx, got.CommonRulePrecedence.MatchIndexInRule, "priority %d matchIndex", i+1)
		assert.Equalf(t, w.pathLen, got.PrecedenceFactor.PathLength, "priority %d pathLen", i+1)
		assert.Equalf(t, w.headers, got.PrecedenceFactor.HeaderCount, "priority %d headerCount", i+1)
	}

	// The single most important invariant vs the reported bug: the specific
	// "/v2" rule must outrank the catch-all rules, i.e. be at priority 1.
	assert.Equal(t, "gateway-conformance-infra/matching-part2", result[0].CommonRulePrecedence.RouteNamespacedName,
		"specific /v2 path must be highest precedence, not the larger-hostname-list catch-all")
	assert.Equal(t, 3, result[0].PrecedenceFactor.PathLength,
		"priority-1 rule must be the /v2 path (len 3)")
}

// ---------------------------------------------------------------------------
// Route-set generation + serialization helpers for the determinism test
// ---------------------------------------------------------------------------

// genRandomRouteSet builds n realistic HTTP/GRPC routes with varied hostnames,
// paths, methods, and headers. Distinct names keep each rule identifiable.
func genRandomRouteSet(rng *rand.Rand, n int) []RouteDescriptor {
	pathTypes := []string{"Exact", "PathPrefix", "RegularExpression"}
	paths := []string{"/", "/v2", "/api/v1", "/health", "/api/v1/users"}
	hostSets := [][]gwv1.Hostname{
		nil,
		{"example.com"},
		{"example.com", "example.net"},
		{"*.example.com"},
		{"api.example.com"},
		{"a.b.example.com", "example.com"},
	}
	grpcServices := []string{"com.example.UserService", "svc.Order", ""}
	grpcMethods := []string{"GetUser", "List", ""}

	out := make([]RouteDescriptor, 0, n)
	ts := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for i := 0; i < n; i++ {
		hosts := hostSets[rng.Intn(len(hostSets))]
		created := ts[rng.Intn(len(ts))]
		ns := []string{"team-a", "team-b"}[rng.Intn(2)]

		if rng.Intn(2) == 0 {
			// HTTP route
			numMatches := 1 + rng.Intn(2)
			matches := make([]gwv1.HTTPRouteMatch, numMatches)
			for m := range matches {
				pt := (*gwv1.PathMatchType)(awssdk.String(pathTypes[rng.Intn(len(pathTypes))]))
				match := gwv1.HTTPRouteMatch{
					Path: &gwv1.HTTPPathMatch{Type: pt, Value: awssdk.String(paths[rng.Intn(len(paths))])},
				}
				if rng.Intn(2) == 0 {
					match.Headers = []gwv1.HTTPHeaderMatch{{Name: "version", Value: "v"}}
				}
				matches[m] = match
			}
			out = append(out, &httpRouteDescription{
				route: &gwv1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:              fmt.Sprintf("http-%d", i),
						Namespace:         ns,
						CreationTimestamp: metav1.Time{Time: created},
					},
					Spec: gwv1.HTTPRouteSpec{Hostnames: hosts},
				},
				rules: []RouteRule{&convertedHTTPRouteRule{rule: &gwv1.HTTPRouteRule{Matches: matches}}},
			})
			continue
		}

		// GRPC route
		var matches []gwv1.GRPCRouteMatch
		if rng.Intn(4) != 0 { // sometimes an empty (catch-all) rule
			svc := grpcServices[rng.Intn(len(grpcServices))]
			meth := grpcMethods[rng.Intn(len(grpcMethods))]
			mm := &gwv1.GRPCMethodMatch{Type: (*gwv1.GRPCMethodMatchType)(awssdk.String("Exact"))}
			if svc != "" {
				mm.Service = awssdk.String(svc)
			}
			if meth != "" {
				mm.Method = awssdk.String(meth)
			}
			match := gwv1.GRPCRouteMatch{Method: mm}
			if rng.Intn(2) == 0 {
				match.Headers = []gwv1.GRPCHeaderMatch{{Name: "x-tenant", Value: "acme"}}
			}
			matches = []gwv1.GRPCRouteMatch{match}
		}
		out = append(out, &grpcRouteDescription{
			route: &gwv1.GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:              fmt.Sprintf("grpc-%d", i),
					Namespace:         ns,
					CreationTimestamp: metav1.Time{Time: created},
				},
				Spec: gwv1.GRPCRouteSpec{Hostnames: hosts},
			},
			rules: []RouteRule{&convertedGRPCRouteRule{rule: &gwv1.GRPCRouteRule{Matches: matches}}},
		})
	}
	return out
}

func cloneRoutes(in []RouteDescriptor) []RouteDescriptor {
	return append([]RouteDescriptor(nil), in...)
}

// describeRule renders the salient factors of a RulePrecedence for failure output.
func describeRule(r RulePrecedence) string {
	c := r.CommonRulePrecedence
	f := r.PrecedenceFactor
	return fmt.Sprintf("{hosts=%v pt=%d pl=%d sec=%d hm=%t hdr=%d qp=%d name=%s ri=%d mi=%d ts=%s}",
		c.Hostnames, f.PathType, f.PathLength, f.SecondaryLength, f.HasMethod, f.HeaderCount, f.QueryParamCount,
		c.RouteNamespacedName, c.RuleIndexInRoute, c.MatchIndexInRule, c.RouteCreateTimestamp.Format("2006"))
}

// rpKey uniquely and stably identifies a flattened rule by its origin.
func rpKey(r RulePrecedence) string {
	c := r.CommonRulePrecedence
	return fmt.Sprintf("%s#r%d#m%d", c.RouteNamespacedName, c.RuleIndexInRoute, c.MatchIndexInRule)
}

func rpKeys(rs []RulePrecedence) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = rpKey(r)
	}
	return out
}

func keysOf(rs []RulePrecedence) []string { return rpKeys(rs) }

func equalKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
