package nlbgatewaymodel

import (
	"context"
	"fmt"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/gateway/common"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	elbgwv1beta1 "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/annotations"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
)

func (t *defaultModelBuildTask) buildListeners(ctx context.Context, scheme elbv2model.LoadBalancerScheme) error {
	for _, listener := range t.gw.Spec.Listeners {
		routeList, ok := t.routes[listener]
		if !ok || len(routeList) == 0 {
			t.logger.Info("Ignoring Gateway Listener with no routes attached", "gw", t.gw, "listener", listener)
			continue
		}
		sslCfg := t.buildListenerConfig(listener)
		_, err := t.buildListener(ctx, listener, routeList[0], sslCfg, scheme)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *defaultModelBuildTask) buildListener(ctx context.Context, gwListener gwv1.Listener, route common.ControllerRoute, sslCfg *elbgwv1beta1.SSLConfiguration,
	scheme elbv2model.LoadBalancerScheme) (*elbv2model.Listener, error) {

	lsSpec, err := t.buildListenerSpec(ctx, string(gwListener.Protocol), gwListener.Port, route, sslCfg, scheme)
	if err != nil {
		return nil, err
	}
	listenerResID := fmt.Sprintf("%v", gwListener.Port)
	ls := elbv2model.NewListener(t.stack, listenerResID, lsSpec)
	return ls, nil
}

func (t *defaultModelBuildTask) buildListenerSpec(ctx context.Context, protocol string, port gwv1.PortNumber, route common.ControllerRoute, sslCfg *elbgwv1beta1.SSLConfiguration,
	scheme elbv2model.LoadBalancerScheme) (elbv2model.ListenerSpec, error) {
	tgProtocol := elbv2model.Protocol(protocol)
	listenerProtocol := elbv2model.Protocol(protocol)
	if tgProtocol != elbv2model.ProtocolUDP && sslCfg != nil {
		if sslCfg.BackendProtocol == "ssl" {
			tgProtocol = elbv2model.ProtocolTLS
		}
		listenerProtocol = elbv2model.ProtocolTLS
	}

	tags, err := t.buildListenerTags()
	if err != nil {
		return elbv2model.ListenerSpec{}, err
	}
	// TODO -- This is bad :)
	targetGroup, err := t.buildTargetGroup(ctx, route.GetServiceRefs()[0].TargetGroupConfig, route.GetServiceRefs()[0], tgProtocol, scheme)
	if err != nil {
		return elbv2model.ListenerSpec{}, err
	}

	alpnPolicy, err := t.buildListenerALPNPolicy(listenerProtocol, sslCfg)
	if err != nil {
		return elbv2model.ListenerSpec{}, err
	}

	var sslPolicy *string
	var certificates []elbv2model.Certificate
	if listenerProtocol == elbv2model.ProtocolTLS {
		sslPolicy = t.buildSSLNegotiationPolicy(sslCfg)
		certificates = t.buildListenerCertificates(sslCfg)
	}

	defaultActions := t.buildListenerDefaultActions(ctx, targetGroup)
	lsAttributes, attributesErr := t.buildListenerAttributes(port, listenerProtocol)
	if attributesErr != nil {
		return elbv2model.ListenerSpec{}, attributesErr
	}
	return elbv2model.ListenerSpec{
		LoadBalancerARN:    t.loadBalancer.LoadBalancerARN(),
		Port:               int32(port),
		Protocol:           listenerProtocol,
		Certificates:       certificates,
		SSLPolicy:          sslPolicy,
		ALPNPolicy:         alpnPolicy,
		DefaultActions:     defaultActions,
		Tags:               tags,
		ListenerAttributes: lsAttributes,
	}, nil
}

func (t *defaultModelBuildTask) buildListenerDefaultActions(_ context.Context, targetGroup *elbv2model.TargetGroup) []elbv2model.Action {
	return []elbv2model.Action{
		{
			Type: elbv2model.ActionTypeForward,
			ForwardConfig: &elbv2model.ForwardActionConfig{
				TargetGroups: []elbv2model.TargetGroupTuple{
					{
						TargetGroupARN: targetGroup.TargetGroupARN(),
					},
				},
			},
		},
	}
}

func validateTLSPortsSet(rawTLSPorts []string, ports []corev1.ServicePort) error {
	unusedPorts := make([]string, 0)

	for _, tlsPort := range rawTLSPorts {
		isPortUsed := false
		for _, portObj := range ports {
			if portObj.Name == tlsPort || strconv.Itoa(int(portObj.Port)) == tlsPort {
				isPortUsed = true
				break
			}
		}

		if !isPortUsed {
			unusedPorts = append(unusedPorts, tlsPort)
		}
	}

	if len(unusedPorts) > 0 {
		return errors.Errorf("Unused port in ssl-ports annotation %v", unusedPorts)
	}

	return nil
}

func (t *defaultModelBuildTask) buildListenerALPNPolicy(listenerProtocol elbv2model.Protocol, sslCfg *elbgwv1beta1.SSLConfiguration) ([]string, error) {
	if listenerProtocol != elbv2model.ProtocolTLS || sslCfg.ALPNPolicy == "" {
		return nil, nil
	}
	return []string{string(sslCfg.ALPNPolicy)}, nil
}

func (t *defaultModelBuildTask) buildListenerConfig(listener gwv1.Listener) *elbgwv1beta1.SSLConfiguration {
	if v, ok := t.combinedConfiguration.SSLConfiguration[string(listener.Port)]; ok {
		return &v
	}
	return nil
}

func (t *defaultModelBuildTask) buildListenerTags() (map[string]string, error) {
	return t.buildAdditionalResourceTags()
}

// Build attributes for listener
func (t *defaultModelBuildTask) buildListenerAttributes(port gwv1.PortNumber, listenerProtocol elbv2model.Protocol) ([]elbv2model.ListenerAttribute, error) {
	annotationKey := fmt.Sprintf("%v.%v-%v", annotations.SvcLBSuffixlsAttsAnnotationPrefix, listenerProtocol, port)

	var rawAttributes map[string]string
	var ok bool
	if rawAttributes, ok = t.combinedConfiguration.ListenerAttributes[annotationKey]; !ok {
		return []elbv2model.ListenerAttribute{}, nil
	}
	attributes := make([]elbv2model.ListenerAttribute, 0, len(rawAttributes))
	for attrKey, attrValue := range rawAttributes {
		attributes = append(attributes, elbv2model.ListenerAttribute{
			Key:   attrKey,
			Value: attrValue,
		})
	}
	return attributes, nil
}

func (t *defaultModelBuildTask) buildListenerCertificates(sslCfg *elbgwv1beta1.SSLConfiguration) []elbv2model.Certificate {
	var certificates []elbv2model.Certificate

	certificates = append(certificates, elbv2model.Certificate{CertificateARN: aws.String(sslCfg.DefaultCertificate)})

	for _, cert := range sslCfg.Certificates {
		certificates = append(certificates, elbv2model.Certificate{CertificateARN: aws.String(cert)})
	}
	return certificates
}

func (t *defaultModelBuildTask) buildSSLNegotiationPolicy(sslCfg *elbgwv1beta1.SSLConfiguration) *string {
	if sslCfg.NegotiationPolicy == nil {
		return &t.defaultSSLPolicy
	}
	return sslCfg.NegotiationPolicy
}
