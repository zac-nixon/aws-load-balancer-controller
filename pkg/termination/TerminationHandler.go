package termination

import (
	"context"
	"github.com/go-logr/logr"
	"net/http"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/targetgroupbinding"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type TerminationHandler struct {
	logger         logr.Logger
	kubeClient     client.Client
	targetsManager targetgroupbinding.TargetsManager
}

func NewTerminationHandler(kubeClient client.Client, targetsManager targetgroupbinding.TargetsManager, logger logr.Logger) http.Handler {
	return &TerminationHandler{
		logger:         logger,
		targetsManager: targetsManager,
		kubeClient:     kubeClient,
	}
}

func (t *TerminationHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	t.logger.Info("Invoked")
	targetIpAddress := request.URL.Query().Get("target_ip_address")
	if targetIpAddress == "" {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	t.logger.Info("Invoked with ip address " + targetIpAddress)
	_, errCode := t.getActiveTGBWithIPTargets()
	if errCode != http.StatusOK {
		writer.WriteHeader(errCode)
		return
	}

	inUse := t.targetsManager.IsIPInUse(targetIpAddress)
	if inUse {
		t.logger.Info("Target IP is in use", "ip", targetIpAddress)
		writer.WriteHeader(http.StatusConflict)
		return
	}

	t.logger.Info("Target IP is not in use", "ip", targetIpAddress)
	writer.WriteHeader(http.StatusOK)
}

func (t *TerminationHandler) getActiveTGBWithIPTargets() (int, int) {
	tgbList := &elbv2api.TargetGroupBindingList{}

	if err := t.kubeClient.List(context.Background(), tgbList); err != nil {
		t.logger.Error(err, "Unable to query for TGB composition")
		return 0, http.StatusInternalServerError
	}

	result := make(map[string]bool)
	for i := range tgbList.Items {
		tgb := &tgbList.Items[i]
		if tgb.Spec.TargetType != nil && *tgb.Spec.TargetType == elbv2api.TargetTypeIP {
			result[tgb.Spec.TargetGroupARN] = true
		}
	}
	return len(result), http.StatusOK
}
