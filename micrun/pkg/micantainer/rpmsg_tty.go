package micantainer

import (
	"context"
	"errors"
	"fmt"
	defs "micrun/definitions"
	log "micrun/logger"
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
		filepath.Join(defs.MicrunStateDir, fmt.Sprintf(ttyPattern, name)),
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

// drainTTY reads and discards any data already present in the TTY buffer.
// This prevents stale data from being read when we first attach to the TTY.
func drainTTY(file *os.File) {
	const drainBufSize = 1024
	buf := make([]byte, drainBufSize)
	drained := 0

	// Set a brief timeout for draining
	// We use NONBLOCK mode temporarily to avoid blocking
	oldFlags, err := unix.FcntlInt(uintptr(file.Fd()), unix.F_GETFL, 0)
	if err != nil {
		return
	}

	// Set non-blocking
	_, _ = unix.FcntlInt(uintptr(file.Fd()), unix.F_SETFL, oldFlags|unix.O_NONBLOCK)
	defer func() {
		// Restore original flags
		_, _ = unix.FcntlInt(uintptr(file.Fd()), unix.F_SETFL, oldFlags)
	}()

	// Read until we get EAGAIN
	for {
		n, err := file.Read(buf)
		if n > 0 {
			drained += n
			log.Debugf("[TTY] Drained %d bytes from TTY buffer", n)
			// Don't drain too much - might be legitimate data
			if drained > 4096 {
				log.Warnf("[TTY] Drained %d bytes, stopping to avoid dropping valid data", drained)
				break
			}
			continue
		}
		if err != nil {
			// EAGAIN means no more data (EWOULDBLOCK == EAGAIN on Linux)
			if errors.Is(err, unix.EAGAIN) {
				break
			}
			// Other errors - stop draining
			break
		}
		// n == 0 and err == nil - EOF
		break
	}

	if drained > 0 {
		log.Infof("[TTY] Total drained %d bytes from TTY buffer", drained)
	}
}

// configureTTY configures the RPMSG TTY device for proper terminal behavior.
//
// For RPMSG TTY devices used by RTOS containers:
// - Use RAW mode for transparent data transfer (no local processing)
// - Disable echo (handled by terminal emulator on client side)
// - Disable signal processing (shim handles Ctrl+C, etc.)
// - Disable output processing (OPOST) to prevent double line breaks
//
// RTOS typically sends proper line endings (\r\n), so we don't convert them.
// This prevents double line breaks when RTOS output already contains \r\n.
func configureTTY(fd uintptr, path string) error {
	log.Infof("[TTY] Configuring RPMSG TTY for raw mode: %s", path)

	var termios unix.Termios
	if err := unix.IoctlSetTermios(int(fd), unix.TCGETS, &termios); err != nil {
		return fmt.Errorf("TCGETS failed: %w", err)
	}

	// Store original for logging (optional, for debugging)
	originalIflag := termios.Iflag
	originalOflag := termios.Oflag
	originalLflag := termios.Lflag

	// Configure for RAW mode with minimal processing
	//
	// Input flags (c_iflag):
	// - ICRNL: Translate CR to NL on input (Enter key -> newline)
	// - IXON/IXANY/IXOFF: Software flow control
	// Clear: IGNBRK, BRKINT, PARMRK, ISTRIP, INLCR, IGNCR
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK |
		unix.ISTRIP | unix.INLCR | unix.IGNCR
	termios.Iflag |= unix.ICRNL | unix.IXON | unix.IXANY

	// Output flags (c_oflag):
	// - OPOST: Enable output processing (needed for proper terminal display)
	// - ONLCR: Map NL to CR-NL (standard terminal behavior)
	// Clear: OCRNL, OLCUC (these cause extra conversions)
	// The goal is: RTOS sends \n -> TTY converts to \r\n -> terminal displays correctly
	termios.Oflag |= unix.OPOST | unix.ONLCR
	termios.Oflag &^= unix.OCRNL | unix.OLCUC

	// Control flags (c_cflag):
	// - CS8: 8-bit data
	// - CREAD: Enable receiver
	// - CLOCAL: Ignore modem control lines
	// Clear: PARENB, PARODD, CSTOPB, CSIZE
	termios.Cflag &^= unix.CSIZE | unix.PARENB | unix.PARODD | unix.CSTOPB
	termios.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL

	// Local flags (c_lflag):
	// - CLEAR ALL: This is the key for RAW mode
	// - No ICANON: Non-canonical mode (no line buffering)
	// - No ECHO*: No echo (handled by client terminal)
	// - No ISIG: No signal processing (we handle Ctrl+C ourselves)
	// - No IEXTEN: No implementation-defined input processing
	termios.Lflag &^= unix.ICANON | unix.ECHO | unix.ECHOE | unix.ECHOK |
		unix.ECHOCTL | unix.ECHOKE | unix.ECHONL | unix.ISIG | unix.IEXTEN |
		unix.NOFLSH | unix.TOSTOP

	// Special characters (c_cc):
	// VMIN=1, VTIME=0: Block until at least 1 character is available
	// This gives us responsive reads without polling
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0

	// Clear all special characters except VMIN/VTIME
	// NCCS is typically 19 on Linux systems
	const nccs = 19
	for i := 0; i < nccs; i++ {
		if i != unix.VMIN && i != unix.VTIME {
			termios.Cc[i] = 0
		}
	}

	if err := unix.IoctlSetTermios(int(fd), unix.TCSETS, &termios); err != nil {
		return fmt.Errorf("TCSETS failed: %w", err)
	}

	log.Infof("[TTY] RPMSG TTY configured: iflag 0x%x->0x%x, oflag 0x%x->0x%x, lflag 0x%x->0x%x",
		originalIflag, termios.Iflag, originalOflag, termios.Oflag, originalLflag, termios.Lflag)

	return nil
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
			log.Debugf("[TTY] Attempting to open TTY: %s", p)
			in, openErr := openTTYOnce(p)
			if openErr != nil {
				if retryableOpenError(openErr) {
					continue
				}
				return nil, nil, "", fmt.Errorf("open rpmsg tty %s: %w", p, openErr)
			}

			// Configure TTY for proper terminal behavior
			if cfgErr := configureTTY(in.Fd(), p); cfgErr != nil {
				log.Warnf("[TTY] Failed to configure TTY (continuing anyway): %v", cfgErr)
				// Continue anyway - TTY may work with default settings
			}

			// Drain any stale data from TTY buffer
			// This prevents reading old data when we first attach
			drainTTY(in)

			// Open stdout (same TTY device)
			// We use the same underlying fd, so we don't need to configure again
			out, openErr := openTTYOnce(p)
			if openErr != nil {
				in.Close()
				if retryableOpenError(openErr) {
					continue
				}
				return nil, nil, "", fmt.Errorf("open rpmsg tty %s (stdout): %w", p, openErr)
			}

			log.Infof("[TTY] Successfully opened RPMSG TTY: %s", p)
			return in, out, p, nil
		}

		select {
		case <-waitCtx.Done():
			return nil, nil, "", fmt.Errorf("wait for rpmsg tty for %s: %w", containerID, waitCtx.Err())
		case <-t.C:
		}
	}
}
