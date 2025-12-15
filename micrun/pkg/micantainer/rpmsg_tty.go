package micantainer

import (
	"context"
	"errors"
	"fmt"
	defs "micrun/definitions"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	timeout    = 10 * time.Second
	retryDelay = 50 * time.Millisecond
)

const (
	ttyPattern = "ttyRPMSG_%s_0"
)

// micad sanitization rule
func sanitizeName(name string) string {
	if name == "" {
		return ""
	}
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '_' || c == '-':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func candiateTTYs(containerID string) []string {
	name := sanitizeName(containerID)
	if name == "" {
		return nil
	}
	return []string{
		filepath.Join("/dev", fmt.Sprintf(ttyPattern, name)),
		filepath.Join(defs.MockDir, fmt.Sprintf(ttyPattern, name)),
	}
}

func retryableOpenError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, unix.ENXIO) ||
		errors.Is(err, unix.EIO) ||
		errors.Is(err, unix.EAGAIN) ||
		errors.Is(err, unix.EINTR)
}

func openTTYOnce(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	_ = unix.SetNonblock(fd, false)
	return os.NewFile(uintptr(fd), path), nil
}

func dialTTY(ctx context.Context, containerID string) (stdin *os.File, stdout *os.File, openedPath string, err error) {
	paths := candiateTTYs(containerID)
	if len(paths) == 0 {
		return nil, nil, "", fmt.Errorf("empty container id")
	}

	timeout := timeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d > 0 && d < timeout {
			timeout = d
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	t := time.NewTicker(retryDelay)
	defer t.Stop()

	for {
		for _, p := range paths {
			in, openErr := openTTYOnce(p)
			if openErr != nil {
				if retryableOpenError(openErr) {
					continue
				}
				return nil, nil, "", fmt.Errorf("open rpmsg tty %s: %w", p, openErr)
			}

			out, openErr := openTTYOnce(p)
			if openErr != nil {
				in.Close()
				if retryableOpenError(openErr) {
					continue
				}
				return nil, nil, "", fmt.Errorf("open rpmsg tty %s: %w", p, openErr)
			}

			return in, out, p, nil
		}

		select {
		case <-waitCtx.Done():
			return nil, nil, "", fmt.Errorf("wait for rpmsg tty for %s: %w", containerID, waitCtx.Err())
		case <-t.C:
		}
	}
}
