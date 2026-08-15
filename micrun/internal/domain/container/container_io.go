package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	log "micrun/internal/support/logger"

	"golang.org/x/sys/unix"
)

func (c *Container) ioStream(ctx context.Context, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	_ = taskID
	if c.config != nil && c.config.IsInfra {
		return noopWriteCloser{}, bytes.NewReader(nil), bytes.NewReader(nil), nil
	}

	stdin, stdout, _, err := dialTTYWithRoots(ctx, c.id, c.ttyDiscoveryRoots())
	if err != nil {
		return nil, nil, nil, err
	}

	// Both stdout and stderr share the same underlying *os.File. Wrap it in
	// a sharedCloser so that Close is called exactly once regardless of how
	// many wrappers reference it. Without this, the stdout fd would leak
	// (noCloseFile.Close was a no-op relied on by the copier path).
	wrapper := &sharedCloseFile{File: stdout}
	return stdin, wrapper, wrapper, nil
}

// sharedCloseFile wraps an *os.File and ensures Close is called exactly once.
// Multiple readers (stdout, stderr) can share the same wrapper safely.
type sharedCloseFile struct {
	*os.File
	once sync.Once
	err  error
}

func (f *sharedCloseFile) Close() error {
	f.once.Do(func() {
		f.err = f.File.Close()
	})
	return f.err
}

type noopWriteCloser struct{}

func (noopWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (noopWriteCloser) Close() error {
	return nil
}

func (c *Container) winresize(ctx context.Context, height, width uint32) (retErr error) {
	operational, err := c.operationalWithContext(ctx)
	if err != nil {
		return err
	}
	if !operational {
		return fmt.Errorf("container not ready or running, impossible to resize the container pty")
	}
	log.Tracef("resizing PTY for container %s to [%dx%d]", c.id, width, height)
	stdin, stdout, p, err := dialTTYWithRoots(ctx, c.id, c.ttyDiscoveryRoots())
	if err != nil {
		return err
	}
	// stdin is only needed for the dial; close immediately and log failures
	// — they must not turn a successful resize into an RPC error (the ioctl
	// below uses stdout, not stdin).
	if cerr := stdin.Close(); cerr != nil {
		log.Warnf("close resize stdin for %s: %v", c.id, cerr)
	}
	defer func() {
		if cerr := stdout.Close(); cerr != nil {
			log.Warnf("close resize stdout for %s: %v", c.id, cerr)
		}
	}()
	log.Tracef("resizing rpmsg tty at %s", p)

	ws := &unix.Winsize{
		Row: uint16(clampTermSize(height)),
		Col: uint16(clampTermSize(width)),
	}
	if err := unix.IoctlSetWinsize(int(stdout.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("set winsize: %w", err)
	}
	return nil
}

// clampTermSize clamps a terminal row/column value to the uint16 range to
// avoid silent truncation when the caller passes a uint32 from RPC input.
func clampTermSize(v uint32) uint32 {
	if v > 65535 {
		return 65535
	}
	return v
}

func (c *Container) OpenTTYs(ctx context.Context) (stdin, stdout *os.File, err error) {
	operational, err := c.operationalWithContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !operational {
		return nil, nil, fmt.Errorf("container not ready or running, impossible to open TTY")
	}
	log.Infof("[TTY] Opening fresh TTY handles for container %s", c.id)
	stdin, stdout, p, err := dialTTYWithRoots(ctx, c.id, c.ttyDiscoveryRoots())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open TTY for %s: %w", c.id, err)
	}
	log.Infof("[TTY] Opened fresh TTY handles for container %s: %s", c.id, p)
	return stdin, stdout, nil
}

func (c *Container) ttyDiscoveryRoots() []string {
	if c == nil || c.sandbox == nil || c.sandbox.deps == nil {
		return defaultTTYDiscoveryRoots()
	}
	roots := c.sandbox.deps.RPMSGTTYRoots()
	if len(roots) == 0 {
		return defaultTTYDiscoveryRoots()
	}
	return roots
}
