package common

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type ServiceRef struct {
	Weight  *int32
	Service *corev1.Service
	Port    int32
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

		svcRefs = append(svcRefs, &ServiceRef{
			Weight:  backendRef.Weight,
			Service: svc,
			Port:    int32(*backendRef.Port),
		})

	}

	return svcRefs, nil
}
