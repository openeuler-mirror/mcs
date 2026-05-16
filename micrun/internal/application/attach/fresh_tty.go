package attach

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"micrun/internal/ports"
	log "micrun/internal/support/logger"
	"micrun/internal/support/sys"
	"micrun/internal/support/validation"
)

type freshTTYHandles struct {
	stdin  io.WriteCloser
	stdout io.Reader
}

func (h freshTTYHandles) attachInfo(src *ports.AttachInfo) ports.AttachInfo {
	return sessionAttachInfoWithTTY(src, h.stdin, h.stdout)
}

func (h freshTTYHandles) present() bool {
	return !validation.IsNil(h.stdin) || !validation.IsNil(h.stdout)
}

func (h freshTTYHandles) close() {
	if !validation.IsNil(h.stdin) {
		if err := h.stdin.Close(); err != nil {
			log.Debugf("failed to close fresh tty input: %v", err)
		}
	}
	outCloser, ok := h.stdout.(io.Closer)
	if !ok || validation.IsNil(outCloser) || sameTTYHandle(outCloser, h.stdin) {
		return
	}
	if err := outCloser.Close(); err != nil {
		log.Debugf("failed to close fresh tty output: %v", err)
	}
}

func sameTTYHandle(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	leftFD, leftHasFD := sys.FDOf(a)
	rightFD, rightHasFD := sys.FDOf(b)
	if leftHasFD || rightHasFD {
		return leftHasFD && rightHasFD && leftFD == rightFD
	}

	left := reflect.ValueOf(a)
	right := reflect.ValueOf(b)
	if left.Type() != right.Type() || !left.Comparable() || !right.Comparable() {
		return false
	}
	return left.Equal(right)
}

func openFreshTTYHandles(ctx context.Context, sandbox ports.Sandbox, containerID string) (freshTTYHandles, error) {
	ttyIn, ttyOut, err := sandbox.OpenTTYs(ctx, containerID)
	if err != nil {
		return freshTTYHandles{}, fmt.Errorf("open fresh TTY for %s: %w", containerID, err)
	}
	handles := freshTTYHandles{stdin: ttyIn, stdout: ttyOut}
	if ttyIn == nil || ttyOut == nil {
		handles.close()
		return freshTTYHandles{}, fmt.Errorf("open fresh TTY for %s returned nil handles", containerID)
	}
	return handles, nil
}
