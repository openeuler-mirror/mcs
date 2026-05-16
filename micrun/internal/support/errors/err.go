package errors

import (
	"fmt"
)

// --- Error types for errors.As() categorization ---

// ErrorType classifies errors for programmatic handling.
type ErrorType int

const (
	TypeInvalid       ErrorType = iota // Invalid input or state
	TypeNotFound                       // Resource not found
	TypeAlreadyExists                  // Resource already exists
	TypeUnavailable                    // Service/resource unavailable
	TypeIO                             // IO-related failure
	TypeNotSupported                   // Operation not supported
	TypeInternal                       // Internal infrastructure failure
)

// MicrunError is the base error type for all MicRun errors.
// Supports errors.Is() for equality checks and errors.As() for type classification.
type MicrunError struct {
	msg   string
	typ   ErrorType
	cause error
}

func (e *MicrunError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.msg, e.cause)
	}
	return e.msg
}

func (e *MicrunError) Unwrap() error {
	return e.cause
}

// Is supports errors.Is() for sentinel comparison.
func (e *MicrunError) Is(target error) bool {
	t, ok := target.(*MicrunError)
	if !ok {
		return false
	}
	return e.msg == t.msg && e.typ == t.typ
}

// Type returns the error classification for programmatic handling.
func (e *MicrunError) Type() ErrorType {
	return e.typ
}

// newError creates a new MicrunError.
func newError(typ ErrorType, msg string) *MicrunError {
	return &MicrunError{msg: msg, typ: typ}
}

// Wrap adds context to an existing MicrunError or wraps a generic error.
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	if me, ok := err.(*MicrunError); ok {
		return &MicrunError{msg: msg, typ: me.typ, cause: err}
	}
	return &MicrunError{msg: msg, typ: TypeInternal, cause: err}
}

// Wrapf adds formatted context to an error.
func Wrapf(err error, format string, args ...any) error {
	return Wrap(err, fmt.Sprintf(format, args...))
}

// --- Pre-defined sentinel errors ---
// Use errors.Is(err, ErrXxx) for comparison. Direct == also works for sentinels.

var (
	// Invalid state
	InvalidState = newError(TypeInvalid, "invalid state")

	// Invalid identifiers
	InvalidCID       = newError(TypeInvalid, "invalid container id")
	EmptyContainerID = newError(TypeInvalid, "empty container id")
	EmptySandboxID   = newError(TypeInvalid, "empty sandbox id")
	InvalidSignal    = newError(TypeInvalid, "invalid signal for client os")

	// Not found
	Missing           = newError(TypeNotFound, "target is empty or not existing")
	ContainerNotFound = newError(TypeNotFound, "container not found")
	SandboxNotFound   = newError(TypeNotFound, "sandbox is nil")

	// Already exists
	AlreadyExists = newError(TypeAlreadyExists, "already exists")
	DuplicatedKey = newError(TypeAlreadyExists, "duplicated key in the map")

	// Unavailable
	SandboxDown         = newError(TypeUnavailable, "sandbox is not running")
	ContainerDown       = newError(TypeUnavailable, "container is not running")
	SandboxNotReady     = newError(TypeUnavailable, "sandbox is not in an actionable state")
	ContainerNotReady   = newError(TypeUnavailable, "container is not ready or stopped")
	ContainerNotRunning = newError(TypeUnavailable, "container is not running")
	ContainerNotPaused  = newError(TypeUnavailable, "container is not paused")
	GuestNotReady       = newError(TypeUnavailable, "client os is not in an actionable state")

	// Invalid
	ContainerSandboxNil  = newError(TypeInvalid, "container sandbox reference is nil")
	InvalidTaskHandle    = newError(TypeInvalid, "missing task handle")
	InvalidAttachInfo    = newError(TypeInvalid, "missing attach info")
	FactoryNotConfigured = newError(TypeInvalid, "required factory is not configured")

	// IO
	IOClosed = newError(TypeIO, "io closed")

	// Not supported
	NotSupported = newError(TypeNotSupported, "micrun or mica does not support this")

	// Internal / infrastructure
	SocketFailed       = newError(TypeInternal, "socket failed")
	PedestalMismatched = newError(TypeInvalid, "host pedestal type mismatch with image pedestal type")
	ErrOutputParse     = newError(TypeInternal, "failed to parse command output")

	// Micad failures
	MicadOpFailed   = newError(TypeInternal, "mica operation failed")
	MicadNotRunning = newError(TypeUnavailable, "mica daemon is not running")
	MicaSocketDown  = newError(TypeUnavailable, "mica-create socket is not alive")

	// Informational / warnings
	FlexibleTaskUnsupported = newError(TypeNotSupported, "micrun does not support exec task, task are immutable inside client os")
	ContainerVCPUNotPinned  = newError(TypeInternal, "container's vcpus are not pinned")
)

// NewWithType creates a new MicrunError with explicit type for programmatic handling.
func NewWithType(typ ErrorType, msg string) error {
	return &MicrunError{msg: msg, typ: typ}
}
