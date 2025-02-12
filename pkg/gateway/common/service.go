package common

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	elbgwv1beta1 "sigs.k8s.io/aws-load-balancer-controller/apis/gateway/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type ServiceRef struct {
	Weight            *int32
	Service           *corev1.Service
	TargetGroupConfig *elbgwv1beta1.TargetGroupConfiguration
	Port              corev1.ServicePort
}

func loadServices(context context.Context, client client.Client, backendRefs []gwv1.BackendRef, inferredNamespace string) ([]*ServiceRef, error) {
	svcRefs := make([]*ServiceRef, 0, len(backendRefs))
	for _, backendRef := range backendRefs {
		if backendRef.Port == nil {
			// TODO -- add name and stuff
			return nil, errors.Errorf("Missing port in backend reference")
		}
		var namespace string
		if backendRef.Namespace == nil {
			namespace = inferredNamespace
		} else {
			namespace = string(*backendRef.Namespace)
		}

		svcName := types.NamespacedName{
			Namespace: namespace,
			Name:      string(backendRef.Name),
		}
		svc := &corev1.Service{}
		err := client.Get(context, svcName, svc)

		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("Unable to fetch svc object %+v", svcName))
		}

		// TODO -- Is this correct?
		var servicePort *corev1.ServicePort

		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Port == int32(*backendRef.Port) {
				servicePort = &svcPort
				break
			}
		}

		if servicePort == nil {
			return nil, errors.Errorf("Unable to find service port for port %d", *backendRef.Port)
		}

		tgConfig, err := resolveTargetGroupConfig(context, client, svc)

		if err != nil {
			return nil, errors.Wrapf(err, "Unable to resolve tg config")
		}

		svcRefs = append(svcRefs, &ServiceRef{
			Weight:            backendRef.Weight,
			Service:           svc,
			Port:              *servicePort,
			TargetGroupConfig: tgConfig,
		})

	}

	return svcRefs, nil
}

func resolveTargetGroupConfig(ctx context.Context, client client.Client, svc *corev1.Service) (*elbgwv1beta1.TargetGroupConfiguration, error) {
	tgConfigList := &elbgwv1beta1.TargetGroupConfigurationList{}
	err := client.List(ctx, tgConfigList)

	if err != nil {
		return nil, err
	}

	for _, tgConfig := range tgConfigList.Items {
		// TODO -- Make this more robust (read: correct hehe)!
		if tgConfig.Namespace == svc.Namespace && tgConfig.Spec.TargetReference.Name == svc.Name {
			return &tgConfig, nil
		}
	}

	return nil, nil
}
