package shim

import (
	"context"
	"errors"
	"fmt"
	"io"
	log "micrun/logger"
	"net/url"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/fifo"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/execabs"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// stdioInfo defines the standard IO paths for a container.
// in practice, since the client RTOS doesn't distinguish stderr, merge stdout and stderr forever
type stdioInfo struct {
	Stdin    string
	Stdout   string
	Stderr   string
	Terminal bool
}

// pipe is a wrapper around an OS pipe.
type pipe struct {
	r *os.File
	w *os.File
}

func (p *pipe) Close() error {
	errw := p.w.Close()
	errr := p.r.Close()
	return errors.Join(errw, errr)
}

// IO defines the interface for handling container IO streams.
type IO interface {
	io.Closer
	Stdin() io.ReadCloser
	// NOTE: stdout() and stderr() are the same writer for our current IO components.
	Stdout() io.Writer
	Stderr() io.Writer
}

// pipeIO implements IO for FIFO pipes.
type pipeIO struct {
	in  io.ReadCloser
	out io.WriteCloser
}

// binaryIO implements IO by running a custom binary for logging.
// NOTE: Related code is from https://github.com/containerd/containerd/blob/v1.6.6/pkg/process/io.go#L311
type binaryIO struct {
	cmd *execabs.Cmd
	out *pipe
}

// fileIO implements IO for files, supporting writing stdout/stderr to the same file.
type fileIO struct {
	out io.WriteCloser
	in  string // File path.
}

// ttyIO manages the TTY and IO streams for a container.
type ttyIO struct {
	io     IO
	stream *stdioInfo
}

func (tty *ttyIO) close() {
	tty.io.Close()
}

// newTtyIO creates a new TTY IO handler based on the provided URI scheme.
func newTtyIO(ctx context.Context, id, stdin, stdout, stderr string, terminal bool) (*ttyIO, error) {
	// TODO: Implement this function.
	var err error
	var ioImpl IO
	stream := &stdioInfo{
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stderr,
		Terminal: terminal,
	}

	// Containerd's default IO URI is fifo.
	uri, err := url.Parse(stdout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse stdout uri: %w", err)
	}

	if uri.Scheme == "" {
		uri.Scheme = "fifo"
	}

	log.Debugf("URI parsed => %+v", uri)
	switch uri.Scheme {
	case "fifo":
		ioImpl, err = newPipeIO(ctx, stream)
	case "binary":
		log.Debugf("using binary IO for container %s", id)
		ioImpl, err = newBinaryIO(ctx, id, uri)
	case "file":
		log.Debugf("using file IO for container %s", id)
		ioImpl, err = newFileIO(ctx, stream, uri)
	default:
		return nil, fmt.Errorf("unknown STDIO scheme %s", uri.Scheme)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create io stream: %w", err)
	}

	return &ttyIO{
		io:     ioImpl,
		stream: stream,
	}, nil
}

// newPipeIO creates a new pipe-based IO handler.
func newPipeIO(ctx context.Context, stdio *stdioInfo) (*pipeIO, error) {
	var in io.ReadCloser
	var out io.WriteCloser
	var err error
	if stdio.Stdin != "" {
		fifoFlags := syscall.O_RDONLY | syscall.O_NONBLOCK
		perm := os.FileMode(0) // Default perm, let containerd set it.
		in, err = fifo.OpenFifo(ctx, stdio.Stdin, fifoFlags, perm)
		if err != nil {
			return nil, err
		}
	}

	if stdio.Stdout != "" {
		out, err = fifo.OpenFifo(ctx, stdio.Stdout, syscall.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
	}

	pipeIO := &pipeIO{
		in:  in,
		out: out,
	}

	return pipeIO, nil
}

func newFileIO(ctx context.Context, stdio *stdioInfo, uri *url.URL) (*fileIO, error) {
	return nil, errdefs.ErrNotImplemented
}

// newBinaryIO runs a custom binary process for pluggable shim logging
// containerd newBinaryIO(ctx context.Context, id string, uri *url.URL) (_ runc.IO, err error)
func newBinaryIO(ctx context.Context, id string, uri *url.URL) (bio *binaryIO, err error) {
	return nil, errdefs.ErrNotImplemented
}

func (p *pipeIO) Close() error {
	var err error
	if err = p.in.Close(); err != nil {
		return fmt.Errorf("failed to close stdin: %w", err)
	}
	if err = p.out.Close(); err != nil && p.out != nil {
		return fmt.Errorf("failed to close stdout: %w", err)
	}
	return nil
}

func (p *pipeIO) Stdin() io.ReadCloser {
	return p.in
}

func (p *pipeIO) Stdout() io.Writer {
	return p.out
}

func (p *pipeIO) Stderr() io.Writer {
	return p.out
}

func (b *binaryIO) Close() error {
	err0 := b.out.Close()
	err1 := b.cmd.Cancel()
	return errors.Join(err0, err1)
}

func (b *binaryIO) Stdin() io.ReadCloser {
	return nil
}

func (b *binaryIO) Stdout() io.Writer {
	return b.out.w
}

func (b *binaryIO) Stderr() io.Writer {
	return b.out.w
}

func (f *fileIO) Close() error {
	var err error
	if err = f.out.Close(); err != nil && f.out != nil {
		return err
	}
	return nil
}

func (f *fileIO) Stdin() io.ReadCloser {
	return nil
}

func (f *fileIO) Stdout() io.Writer {
	return f.out
}

func (f *fileIO) Stderr() io.Writer {
	return f.out
}

// ioCopy manages copying data between the container's IO streams and the pipe.
func ioCopy(ctx context.Context, exitch, stdinCloser chan struct{}, tty *ttyIO, stdinPipe io.WriteCloser, stdoutPipe io.Reader, onInterrupt func(syscall.Signal, string)) {
	var wg sync.WaitGroup
	killOnce := sync.Once{}
	notifyInterrupt := func(sig syscall.Signal, reason string) {
		if onInterrupt == nil {
			return
		}
		killOnce.Do(func() {
			onInterrupt(sig, reason)
		})
	}
	control := detectControlChars()

	// Mica client **always** create ONE pty slave, we have to handle bytes from it for all different io stream methods of containerd
	if tty.io.Stdout() != nil {
		wg.Add(1)
		go func() {
			log.Debug("Starting stdout copy from PTY to containerd.")
			if _, err := io.Copy(tty.io.Stdout(), stdoutPipe); err != nil {
				log.Debugf("stdout copy finished with error: %v", err)
			} else {
				log.Debug("Stdout copy completed.")
			}
			wg.Done()
			if tty.io.Stdin() != nil {
				tty.io.Stdin().Close()
			}
			log.Info("Out stream copy exited.")
		}()
	}

	if tty.io.Stdin() != nil {
		wg.Add(1)
		go func() {
			log.Debug("Starting stdin copy from containerd to PTY.")
			defer wg.Done()
			defer close(stdinCloser)
			buf := make([]byte, 4096)
			for {
				select {
				case <-ctx.Done():
					log.Debug("Stdin copy canceled by context.")
					return
				default:
				}

				n, err := tty.io.Stdin().Read(buf)
				if n > 0 {
					chunk := buf[:n]
					if sig, ok := control.detect(chunk); ok {
						log.Infof("Captured host control character, interrupting container IO.")
						notifyInterrupt(sig, "host-control")
						return
					}
					if stdinPipe == nil {
						log.Debug("stdin pipe is nil, stop copying stdin.")
						return
					}
					if _, werr := stdinPipe.Write(chunk); werr != nil {
						log.Debugf("Stdin write failed: %v", werr)
						return
					}
				}
				if err != nil {
					if !errors.Is(err, io.EOF) {
						log.Debugf("Stdin copy ended with error: %v", err)
					} else {
						log.Debug("Stdin copy completed.")
					}
					return
				}
			}
		}()
	} else {
		close(stdinCloser)
	}

	wg.Wait()
	close(exitch)
	log.Debug("All IO copies completed.")
}

type controlCharSet struct {
	intr byte
	quit byte
}

func detectControlChars() controlCharSet {
	cc := controlCharSet{intr: 0x03, quit: 0x1c} // Defaults: INTR (^C), QUIT (^\)

	// current console, parse termios configurations
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return cc
	}
	defer tty.Close()

	if !term.IsTerminal(int(tty.Fd())) {
		return cc
	}

	if t, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS); err == nil && len(t.Cc) > 0 {
		if t.Cc[unix.VINTR] != 0 {
			cc.intr = t.Cc[unix.VINTR]
		}
		if t.Cc[unix.VQUIT] != 0 {
			cc.quit = t.Cc[unix.VQUIT]
		}
	}

	return cc
}

func (c controlCharSet) detect(p []byte) (syscall.Signal, bool) {
	for _, b := range p {
		switch b {
		case c.intr, c.quit:
			return syscall.SIGKILL, true
		}
	}
	return 0, false
}

// getBoolAnnotation parses a boolean annotation from the container spec with a default value.
// Returns (value, isExplicitlySet) where isExplicitlySet indicates if the annotation was provided.
func getBoolAnnotation(spec *specs.Spec, key string, defaultValue bool) (bool, bool) {
	if spec == nil || spec.Annotations == nil {
		return defaultValue, false
	}

	if value, ok := spec.Annotations[key]; ok {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed, true
		}
		log.Warnf("Failed to parse boolean annotation, using default: %v", defaultValue)
	}
	return defaultValue, false
}

// getDurationAnnotation parses a duration annotation (in seconds) from the container spec with a default value.
// Returns (value, isExplicitlySet) where isExplicitlySet indicates if the annotation was provided.
func getDurationAnnotation(spec *specs.Spec, key string, defaultValue time.Duration) (time.Duration, bool) {
	if spec == nil || spec.Annotations == nil {
		return defaultValue, false
	}

	if value, ok := spec.Annotations[key]; ok {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			duration := time.Duration(seconds) * time.Second
			if duration > 0 {
				return duration, true
			}
			log.Warnf("annotation %s has invalid duration %s, using default %v", key, value, defaultValue)
		} else {
			log.Warnf("annotation %s parse error: %v, defaulting to %v", key, err, defaultValue)
		}
	}
	return defaultValue, false
}
