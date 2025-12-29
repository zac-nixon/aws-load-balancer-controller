package routeutils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwxalpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
)

func TestListXListenerSets(t *testing.T) {
	scheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(scheme)
	gwxalpha1.AddToScheme(scheme)

	ls1 := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls1", Namespace: "default"},
	}
	ls2 := &gwxalpha1.XListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ls2", Namespace: "default"},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ls1, ls2).
		Build()

	result, err := ListXListenerSets(context.Background(), client)
	assert.NoError(t, err)
	assert.Len(t, result, 2)
}
