package container

import (
	"context"
	"errors"
	"fmt"
	"micrun/internal/support/contextx"
	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"micrun/internal/support/fs"
)

const (
	// Default timeout for TTY wait.
	// Set to 30s to accommodate micad startup time (typically 5-6s, but can be longer under load).
	timeout    = 30 * time.Second
	retryDelay = 50 * time.Millisecond
)

const (
	ttyPattern        = "ttyRPMSG_%s_0"
	deviceTTYRoot     = "/dev"
	legacyMockTTYRoot = "/tmp/mica"
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

func candidateTTYs(containerID string) []string {
	return buildCandidateTTYs(containerID, defaultTTYDiscoveryRoots())
}

func defaultTTYDiscoveryRoots() []string {
	return DefaultRPMSGTTYRoots(defs.MicrunStateDir)
}

func DefaultRPMSGTTYRoots(stateDir string) []string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		stateDir = defs.MicrunStateDir
	} else if clean, err := fs.CleanAbsolutePath(stateDir); err == nil {
		stateDir = clean
	} else {
		stateDir = defs.MicrunStateDir
	}
	return []string{
		deviceTTYRoot,
		stateDir,
		legacyMockTTYRoot,
	}
}

func buildCandidateTTYs(containerID string, roots []string) []string {
	name := sanitizeName(containerID)
	if name == "" {
		return nil
	}

	ttyName := fmt.Sprintf(ttyPattern, name)
	paths := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		path := filepath.Join(root, ttyName)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
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
	// Keep TTY in non-blocking mode for the IO copier
	// The new copier handles EAGAIN properly with retry logic
	return os.NewFile(uintptr(fd), path), nil
}

// drainTTY reads and discards any data already present in the TTY buffer.
// This prevents stale data from being read when we first attach to the TTY.
// Uses a limited number of reads to avoid indefinite blocking when RTOS is continuously sending data.
func drainTTY(file *os.File) {
	log.Debugf("[TTY] drainTTY called")
	const drainBufSize = 1024
	const maxDrainReads = 5 // Limit number of drain reads to avoid indefinite loop
	const drainThreshold = 4096
	buf := make([]byte, drainBufSize)
	drained := 0
	readCount := 0
	fd := file.Fd()

	// Check current flags
	oldFlags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
	if err != nil {
		log.Warnf("[TTY] Failed to get flags: %v", err)
		return
	}

	// Set non-blocking
	_, err = unix.FcntlInt(fd, unix.F_SETFL, oldFlags|unix.O_NONBLOCK)
	if err != nil {
		log.Warnf("[TTY] Failed to set non-blocking: %v", err)
		return
	}
	defer func() {
		// Restore original flags
		_, _ = unix.FcntlInt(fd, unix.F_SETFL, oldFlags)
	}()

	// Read until we get EAGAIN or reach max read count
	for readCount < maxDrainReads {
		readCount++
		// Use unix.Read directly on file descriptor to ensure non-blocking behavior
		n, err := unix.Read(int(fd), buf)
		if n > 0 {
			drained += n
			log.Debugf("[TTY] Drained %d bytes (read %d/%d)", n, readCount, maxDrainReads)
			// Don't drain too much - might be legitimate data
			if drained > drainThreshold {
				log.Warnf("[TTY] Drained %d bytes, stopping to avoid dropping valid data", drained)
				break
			}
			// Continue to next read (limited by maxDrainReads)
			continue
		}
		if err != nil {
			// EAGAIN means no more data (EWOULDBLOCK == EAGAIN on Linux)
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				break
			}
			// Other errors - stop draining
			log.Debugf("[TTY] Drain error: %v", err)
			break
		}
		// n == 0 and err == nil - EOF
		break
	}

	if drained > 0 {
		log.Debugf("[TTY] Total drained %d bytes in %d reads", drained, readCount)
	}
	if readCount >= maxDrainReads {
		log.Debugf("[TTY] Drain reached max read count (%d)", maxDrainReads)
	}
}

// configureTTY configures the RPMSG TTY device for proper terminal behavior.
//
// For RPMSG TTY devices used by RTOS containers:
// - Use RAW mode for transparent data transfer (no local processing)
// - Disable echo (handled by terminal emulator on client side)
// - Disable signal processing (RTOS has no POSIX signal mechanism)
// - Disable output processing (OPOST) to prevent double line breaks
// - Disable input processing (ICRNL, etc.) for transparent passthrough
//
// RTOS typically sends proper line endings (\r\n), so we don't convert them.
// This prevents double line breaks when RTOS output already contains \r\n.
// For non-TTY mode, the copier converts LF to CRLF, and the TTY must pass it through unchanged.
func configureTTY(fd uintptr, path string) error {
	log.Debugf("[TTY] Configuring RPMSG TTY for raw mode: %s", path)

	var termios unix.Termios
	if err := unix.IoctlSetTermios(int(fd), unix.TCGETS, &termios); err != nil {
		return fmt.Errorf("TCGETS failed: %w", err)
	}

	// Store original for logging (optional, for debugging)
	originalIflag := termios.Iflag
	originalOflag := termios.Oflag
	originalLflag := termios.Lflag

	termios = rawRPMSGTermios(termios)

	if err := unix.IoctlSetTermios(int(fd), unix.TCSETS, &termios); err != nil {
		return fmt.Errorf("TCSETS failed: %w", err)
	}

	log.Infof("[TTY] RPMSG TTY configured: iflag 0x%x->0x%x, oflag 0x%x->0x%x, lflag 0x%x->0x%x",
		originalIflag, termios.Iflag, originalOflag, termios.Oflag, originalLflag, termios.Lflag)

	return nil
}

func rawRPMSGTermios(termios unix.Termios) unix.Termios {
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK |
		unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL |
		unix.IXON | unix.IXANY | unix.IXOFF

	termios.Oflag &^= unix.OPOST | unix.ONLCR | unix.OCRNL | unix.OLCUC

	termios.Cflag &^= unix.CSIZE | unix.PARENB | unix.PARODD | unix.CSTOPB
	termios.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL

	termios.Lflag &^= unix.ICANON | unix.ECHO | unix.ECHOE | unix.ECHOK |
		unix.ECHOCTL | unix.ECHOKE | unix.ECHONL | unix.ISIG | unix.IEXTEN |
		unix.NOFLSH | unix.TOSTOP

	for i := range termios.Cc {
		termios.Cc[i] = 0
	}
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0
	return termios
}

// cleanupStaleSymlink removes stale TTY symlinks that point to non-existent devices.
// This prevents ENXIO errors when trying to open TTYs from previous container runs.
func cleanupStaleSymlink(path string) {
	// Check if path is a symlink
	fi, err := os.Lstat(path)
	if err != nil {
		// Path doesn't exist, nothing to clean up
		return
	}

	// Only process symlinks
	if fi.Mode()&os.ModeSymlink == 0 {
		return
	}

	// Resolve the symlink to check if target exists
	target, err := os.Readlink(path)
	if err != nil {
		log.Debugf("[TTY] Failed to read symlink %s: %v", path, err)
		return
	}
	targetPath := resolveSymlinkTarget(path, target)

	// Check if target file exists
	if _, err := os.Stat(targetPath); err != nil {
		if os.IsNotExist(err) {
			// Target doesn't exist, remove stale symlink
			log.Debugf("[TTY] Removing stale symlink %s -> %s", path, target)
			if removeErr := os.Remove(path); removeErr != nil {
				log.Warnf("[TTY] Failed to remove stale symlink %s: %v", path, removeErr)
			}
		}
	}
}

func resolveSymlinkTarget(linkPath, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(linkPath), target)
}

func dialTTY(ctx context.Context, containerID string) (stdin *os.File, stdout *os.File, openedPath string, err error) {
	return dialTTYWithRoots(ctx, containerID, defaultTTYDiscoveryRoots())
}

func dialTTYWithRoots(ctx context.Context, containerID string, roots []string) (stdin *os.File, stdout *os.File, openedPath string, err error) {
	ctx = contextx.OrBackground(ctx)
	paths := buildCandidateTTYs(containerID, roots)
	if len(paths) == 0 {
		return nil, nil, "", fmt.Errorf("empty container id")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, "", fmt.Errorf("wait for rpmsg tty for %s: %w", containerID, err)
	}

	// Clean up any stale symlinks before attempting to open TTY
	// This prevents ENXIO errors from previous container runs
	for _, p := range paths {
		cleanupStaleSymlink(p)
	}

	waitTimeout := timeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d > 0 && d < timeout {
			waitTimeout = d
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
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

			log.Infof("[TTY] Opened RPMSG TTY for %s: %s", containerID, p)
			return in, out, p, nil
		}

		select {
		case <-waitCtx.Done():
			return nil, nil, "", fmt.Errorf("wait for rpmsg tty for %s: %w", containerID, waitCtx.Err())
		case <-t.C:
		}
	}
}
