package routeutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func Test_buildHttpPathCondition(t *testing.T) {
	prefixType := gwv1.PathMatchPathPrefix
	exactType := gwv1.PathMatchExact
	regexType := gwv1.PathMatchRegularExpression
	pathValue := "/prefix"
	pathValueWithWildcard := "/prefix*"
	regexPathValue := "/v+?/*"

	tests := []struct {
		name    string
		path    gwv1.HTTPPathMatch
		want    []elbv2model.RuleCondition
		wantErr bool
	}{
		{
			name: "prefix path type",
			path: gwv1.HTTPPathMatch{
				Type:  &prefixType,
				Value: &pathValue,
			},
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldPathPattern,
					PathPatternConfig: &elbv2model.PathPatternConditionConfig{
						Values: []string{"/prefix", "/prefix/*"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "prefix path type with wildcard",
			path: gwv1.HTTPPathMatch{
				Type:  &prefixType,
				Value: &pathValueWithWildcard,
			},
			wantErr: true,
		},
		{
			name: "exact path type",
			path: gwv1.HTTPPathMatch{
				Type:  &exactType,
				Value: &pathValue,
			},
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldPathPattern,
					PathPatternConfig: &elbv2model.PathPatternConditionConfig{
						Values: []string{"/prefix"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "exact path type with wildcard",
			path: gwv1.HTTPPathMatch{
				Type:  &exactType,
				Value: &pathValueWithWildcard,
			},
			wantErr: true,
		},
		{
			name: "regular expression path type",
			path: gwv1.HTTPPathMatch{
				Type:  &regexType,
				Value: &regexPathValue,
			},
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldPathPattern,
					PathPatternConfig: &elbv2model.PathPatternConditionConfig{
						Values: []string{regexPathValue},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildHttpPathCondition(&tt.path)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "shouldn't contain wildcards")
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_buildHttpHeaderCondition(t *testing.T) {
	testKey := "testKey"
	testValue := "testValue"
	testKeyTwo := "testKeyTwo"
	testValueTwo := "testValueTwo"
	tests := []struct {
		name        string
		headerMatch []gwv1.HTTPHeaderMatch
		want        []elbv2model.RuleCondition
	}{
		{
			name: "single header match",
			headerMatch: []gwv1.HTTPHeaderMatch{
				{
					Name:  gwv1.HTTPHeaderName(testKey),
					Value: testValue,
				},
			},
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldHTTPHeader,
					HTTPHeaderConfig: &elbv2model.HTTPHeaderConditionConfig{
						HTTPHeaderName: testKey,
						Values:         []string{testValue},
					},
				},
			},
		},
		{
			name: "multiple header match",
			headerMatch: []gwv1.HTTPHeaderMatch{
				{
					Name:  gwv1.HTTPHeaderName(testKey),
					Value: testValue,
				},
				{
					Name:  gwv1.HTTPHeaderName(testKeyTwo),
					Value: testValueTwo,
				},
			},
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldHTTPHeader,
					HTTPHeaderConfig: &elbv2model.HTTPHeaderConditionConfig{
						HTTPHeaderName: testKey,
						Values:         []string{testValue},
					},
				},
				{
					Field: elbv2model.RuleConditionFieldHTTPHeader,
					HTTPHeaderConfig: &elbv2model.HTTPHeaderConditionConfig{
						HTTPHeaderName: testKeyTwo,
						Values:         []string{testValueTwo},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildHttpHeaderCondition(tt.headerMatch)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_buildHttpQueryParamCondition(t *testing.T) {
	testKey := "testKey"
	testValue := "testValue"
	testKeyTwo := "testKeyTwo"
	testValueTwo := "testValueTwo"
	tests := []struct {
		name        string
		queryParams []gwv1.HTTPQueryParamMatch
		want        []elbv2model.RuleCondition
	}{
		{
			name: "single query param",
			queryParams: []gwv1.HTTPQueryParamMatch{
				{
					Name:  gwv1.HTTPHeaderName(testKey),
					Value: testValue,
				},
			},
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldQueryString,
					QueryStringConfig: &elbv2model.QueryStringConditionConfig{
						Values: []elbv2model.QueryStringKeyValuePair{
							{
								Key:   &testKey,
								Value: testValue,
							},
						},
					},
				},
			},
		},
		{
			name: "multiple query params",
			queryParams: []gwv1.HTTPQueryParamMatch{
				{
					Name:  gwv1.HTTPHeaderName(testKey),
					Value: testValue,
				},
				{
					Name:  gwv1.HTTPHeaderName(testKeyTwo),
					Value: testValueTwo,
				},
			},
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldQueryString,
					QueryStringConfig: &elbv2model.QueryStringConditionConfig{
						Values: []elbv2model.QueryStringKeyValuePair{
							{
								Key:   &testKey,
								Value: testValue,
							},
						},
					},
				},
				{
					Field: elbv2model.RuleConditionFieldQueryString,
					QueryStringConfig: &elbv2model.QueryStringConditionConfig{
						Values: []elbv2model.QueryStringKeyValuePair{
							{
								Key:   &testKeyTwo,
								Value: testValueTwo,
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildHttpQueryParamCondition(tt.queryParams)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_buildHttpMethodCondition(t *testing.T) {
	methodValue := "GET"

	tests := []struct {
		name   string
		method gwv1.HTTPMethod
		want   []elbv2model.RuleCondition
	}{
		{
			name:   "simple method",
			method: gwv1.HTTPMethod(methodValue),
			want: []elbv2model.RuleCondition{
				{
					Field: elbv2model.RuleConditionFieldHTTPRequestMethod,
					HTTPRequestMethodConfig: &elbv2model.HTTPRequestMethodConditionConfig{
						Values: []string{methodValue},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildHttpMethodCondition(&tt.method)
			assert.Equal(t, tt.want, got)
		})
	}
}
