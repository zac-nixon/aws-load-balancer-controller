package routeutils

import (
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type LoaderError interface {
	Error() string
	GetRawError() error
	GetCondition() gwv1.GatewayConditionReason
	IsUpdate() bool
}

type loaderErrorImpl struct {
	updateStatus      bool
	resolvedCondition gwv1.GatewayConditionReason
	underlyingErr     error
}

func wrapError(underlyingErr error, resolvedCondition gwv1.GatewayConditionReason) LoaderError {
	return &loaderErrorImpl{
		underlyingErr:     underlyingErr,
		updateStatus:      true,
		resolvedCondition: resolvedCondition,
	}
}

func wrapErrorNoStatusUpdate(underlyingErr error) LoaderError {
	return &loaderErrorImpl{
		underlyingErr: underlyingErr,
	}
}

func (e *loaderErrorImpl) GetRawError() error {
	return e.underlyingErr
}

func (e *loaderErrorImpl) GetCondition() gwv1.GatewayConditionReason {
	return e.resolvedCondition
}

func (e *loaderErrorImpl) IsUpdate() bool {
	return e.updateStatus
}

func (e *loaderErrorImpl) Error() string {
	return e.underlyingErr.Error()
}
