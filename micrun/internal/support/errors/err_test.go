package errors

import (
	"errors"
	"testing"
)

// sentinels lists every exported sentinel in this package. Keep it in sync
// with the var block in err.go — TestSentinelErrorsAreDistinct is only as
// strong as this list.
func sentinels() map[string]*MicrunError {
	return map[string]*MicrunError{
		"InvalidState":            InvalidState,
		"InvalidCID":              InvalidCID,
		"EmptyContainerID":        EmptyContainerID,
		"EmptySandboxID":          EmptySandboxID,
		"InvalidSignal":           InvalidSignal,
		"Missing":                 Missing,
		"ContainerNotFound":       ContainerNotFound,
		"SandboxNotFound":         SandboxNotFound,
		"AlreadyExists":           AlreadyExists,
		"DuplicatedKey":           DuplicatedKey,
		"SandboxDown":             SandboxDown,
		"ContainerDown":           ContainerDown,
		"SandboxNotReady":         SandboxNotReady,
		"ContainerNotReady":       ContainerNotReady,
		"ContainerNotRunning":     ContainerNotRunning,
		"ContainerNotPaused":      ContainerNotPaused,
		"GuestNotReady":           GuestNotReady,
		"ContainerSandboxNil":     ContainerSandboxNil,
		"InvalidTaskHandle":       InvalidTaskHandle,
		"InvalidAttachInfo":       InvalidAttachInfo,
		"FactoryNotConfigured":    FactoryNotConfigured,
		"IOClosed":                IOClosed,
		"NotSupported":            NotSupported,
		"SocketFailed":            SocketFailed,
		"PedestalMismatched":      PedestalMismatched,
		"ErrOutputParse":          ErrOutputParse,
		"MicadOpFailed":           MicadOpFailed,
		"MicadNotRunning":         MicadNotRunning,
		"MicaSocketDown":          MicaSocketDown,
		"FlexibleTaskUnsupported": FlexibleTaskUnsupported,
		"ContainerVCPUNotPinned":  ContainerVCPUNotPinned,
	}
}

// TestSentinelErrorsAreDistinct guards the sentinel contract: Is() matches on
// (msg, typ) rather than pointer identity, so two sentinels that share both
// fields become indistinguishable under errors.Is. That silently routes one
// error down another's handling path — ContainerDown and ContainerNotRunning
// collided this way, which would have made a pause-class rejection read as
// "the guest domain is gone" in the exit watcher.
func TestSentinelErrorsAreDistinct(t *testing.T) {
	all := sentinels()
	for leftName, left := range all {
		for rightName, right := range all {
			if leftName == rightName {
				continue
			}
			if errors.Is(left, right) {
				t.Errorf("errors.Is(%s, %s) = true, want false (msg=%q type=%d shared)",
					leftName, rightName, left.msg, left.typ)
			}
		}
	}
}

func TestSentinelMatchesItselfThroughWrap(t *testing.T) {
	for name, sentinel := range sentinels() {
		wrapped := Wrapf(sentinel, "while doing %s", name)
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("errors.Is(Wrapf(%s), %s) = false, want true", name, name)
		}
		var target *MicrunError
		if !errors.As(wrapped, &target) {
			t.Errorf("errors.As on wrapped %s failed", name)
			continue
		}
		if target.Type() != sentinel.Type() {
			t.Errorf("Wrapf(%s) type = %d, want %d", name, target.Type(), sentinel.Type())
		}
	}
}
