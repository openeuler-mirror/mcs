package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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

	return stdin, &noCloseFile{stdout}, &noCloseFile{stdout}, nil
}

type noCloseFile struct {
	*os.File
}

func (f *noCloseFile) Close() error {
	return nil
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
	appendCloseError(&retErr, "resize stdin", stdin)
	defer appendCloseError(&retErr, "resize stdout", stdout)
	log.Tracef("resizing rpmsg tty at %s", p)

	ws := &unix.Winsize{Row: uint16(height), Col: uint16(width)}
	if err := unix.IoctlSetWinsize(int(stdout.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("set winsize: %w", err)
	}
	return nil
}

func appendCloseError(target *error, name string, closer io.Closer) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		*target = errors.Join(*target, fmt.Errorf("close %s: %w", name, err))
	}
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
	if c == nil || c.sandbox == nil || c.sandbox.deps == nil || c.sandbox.deps.TTYDiscoveryRoots == nil {
		return defaultTTYDiscoveryRoots()
	}
	roots := c.sandbox.deps.TTYDiscoveryRoots()
	if len(roots) == 0 {
		return defaultTTYDiscoveryRoots()
	}
	return roots
}
