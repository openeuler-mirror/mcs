package shim

import (
	"errors"

	er "micrun/internal/support/errors"

	"github.com/containerd/containerd/errdefs"
)

func grpcUnsupportedExec(err error) error {
	return errdefs.ToGRPCf(errdefs.ErrNotImplemented, "%v", err)
}

// micrunErrorToGRPC maps the internal *MicrunError classification to the
// corresponding containerd errdefs/grpc code so callers (containerd, CRI)
// receive NotFound/InvalidArgument/Unavailable instead of a generic Unknown.
func micrunErrorToGRPC(err error) error {
	var me *er.MicrunError
	if errors.As(err, &me) {
		switch me.Type() {
		case er.TypeNotFound:
			return errdefs.ToGRPCf(errdefs.ErrNotFound, "%v", err)
		case er.TypeAlreadyExists:
			return errdefs.ToGRPCf(errdefs.ErrAlreadyExists, "%v", err)
		case er.TypeInvalid:
			return errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "%v", err)
		case er.TypeUnavailable:
			return errdefs.ToGRPCf(errdefs.ErrUnavailable, "%v", err)
		case er.TypeNotSupported:
			return errdefs.ToGRPCf(errdefs.ErrNotImplemented, "%v", err)
		}
	}
	return errdefs.ToGRPC(err)
}

func grpcExecAwareError(execID string, err error) error {
	if execID != "" {
		return grpcUnsupportedExec(err)
	}
	return micrunErrorToGRPC(err)
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
