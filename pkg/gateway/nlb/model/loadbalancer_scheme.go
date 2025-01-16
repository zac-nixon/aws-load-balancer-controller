package nlbgatewaymodel

import elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"

func (t *defaultModelBuildTask) buildLoadBalancerScheme() (elbv2model.LoadBalancerScheme, error) {
	if t.combinedConfiguration.LoadBalancerScheme != nil {
		return elbv2model.LoadBalancerScheme(*t.combinedConfiguration.LoadBalancerScheme), nil
	}
	return t.defaultLoadBalancerScheme, nil
}
