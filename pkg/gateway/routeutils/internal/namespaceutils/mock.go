package namespaceutils

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type MockNamespaceSelector struct {
	Nss sets.Set[string]
	Err error
}

func (mnss *MockNamespaceSelector) GetNamespacesFromSelector(_ context.Context, _ *metav1.LabelSelector) (sets.Set[string], error) {
	return mnss.Nss, mnss.Err
}
