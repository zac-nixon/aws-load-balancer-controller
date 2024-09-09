package config

import "k8s.io/apimachinery/pkg/util/sets"

const (
	SharedTargetGroupTagKey = "elbv2.k8s.aws/shared-targetgroup"
)

var (
	ImmutableTagKeys = []string{
		SharedTargetGroupTagKey,
	}

	trackingTagKeys = sets.NewString(
		"elbv2.k8s.aws/cluster",
		"elbv2.k8s.aws/resource",
		"ingress.k8s.aws/stack",
		"ingress.k8s.aws/resource",
		"service.k8s.aws/stack",
		"service.k8s.aws/resource",
	)
)
