package shim

import "github.com/containerd/containerd/errdefs"

func grpcUnsupportedExec(err error) error {
	return errdefs.ToGRPCf(errdefs.ErrNotImplemented, "%v", err)
}

func grpcExecAwareError(execID string, err error) error {
	if execID != "" {
		return grpcUnsupportedExec(err)
	}
	return err
}

func grpcExecAwareErrorWithFallback(execID string, err error, fallback func(error) error) error {
	if execID != "" {
		return grpcUnsupportedExec(err)
	}
	return fallback(err)
}

func grpcExecAwareRequestError(r execIDTransportRequest, err error) error {
	return grpcExecAwareError(execIDFromTransport(r), err)
}

func grpcExecAwareRequestErrorWithFallback(r execIDTransportRequest, err error, fallback func(error) error) error {
	return grpcExecAwareErrorWithFallback(execIDFromTransport(r), err, fallback)
}

func grpcNilRequest(operation string) error {
	return errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "%s request is nil", operation)
}

func requireTransportRequest[T any](operation string, request *T) error {
	if request == nil {
		return grpcNilRequest(operation)
	}
	return nil
}
