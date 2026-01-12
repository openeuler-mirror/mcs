package shim

import (
	"context"
	"errors"
	"fmt"
	"io"
	log "micrun/logger"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containerd/containerd/namespaces"
	cioutil "github.com/containerd/containerd/pkg/ioutil"
	"github.com/containerd/fifo"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/execabs"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const ioprocTimeout = 12 * time.Second

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
	err *pipe
}

// fileIO implements IO for files, supporting writing stdout/stderr to the same file.
type fileIO struct {
	outw io.WriteCloser
	errw io.WriteCloser
	path string
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
		ioImpl, err = newStdinWrappedIO(ctx, stream.Stdin, func() (outputIO, error) {
			return newBinaryIO(ctx, id, uri)
		})
	case "file":
		log.Debugf("using file IO for container %s", id)
		ioImpl, err = newStdinWrappedIO(ctx, stream.Stdin, func() (outputIO, error) {
			return newFileIO(ctx, stream, uri)
		})
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

type outputIO interface {
	io.Closer
	Stdout() io.Writer
	Stderr() io.Writer
}

type stdinWrappedIO struct {
	stdin io.ReadCloser
	out   outputIO
}

func (w *stdinWrappedIO) Close() error {
	var err0, err1 error
	if w.stdin != nil {
		err0 = w.stdin.Close()
	}
	if w.out != nil {
		err1 = w.out.Close()
	}
	return errors.Join(err0, err1)
}

func (w *stdinWrappedIO) Stdin() io.ReadCloser {
	return w.stdin
}

func (w *stdinWrappedIO) Stdout() io.Writer {
	if w.out == nil {
		return nil
	}
	return w.out.Stdout()
}

func (w *stdinWrappedIO) Stderr() io.Writer {
	if w.out == nil {
		return nil
	}
	return w.out.Stderr()
}

func openStdinFifo(ctx context.Context, stdin string) (io.ReadCloser, error) {
	if stdin == "" {
		return nil, nil
	}
	fifoFlags := syscall.O_RDONLY | syscall.O_NONBLOCK
	perm := os.FileMode(0)
	return fifo.OpenFifo(ctx, stdin, fifoFlags, perm)
}

// openStdoutFifo opens or reopens the stdout FIFO for writing.
// Used for attach support when the FIFO is closed and needs to be reopened.
func openStdoutFifo(ctx context.Context, stdout string) (io.WriteCloser, error) {
	if stdout == "" {
		return nil, nil
	}
	// Open with O_RDWR for write access (same as initial open in newPipeIO)
	return fifo.OpenFifo(ctx, stdout, syscall.O_RDWR, 0)
}

func newStdinWrappedIO(ctx context.Context, stdin string, outFactory func() (outputIO, error)) (IO, error) {
	in, err := openStdinFifo(ctx, stdin)
	if err != nil {
		return nil, err
	}

	out, err := outFactory()
	if err != nil {
		if in != nil {
			in.Close()
		}
		return nil, err
	}

	return &stdinWrappedIO{
		stdin: in,
		out:   out,
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
	logFile := uri.Path
	if logFile == "" {
		logFile = uri.Opaque
	}
	if logFile == "" {
		return nil, fmt.Errorf("file io requires a path")
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return nil, err
	}

	w := cioutil.NewSerialWriteCloser(f)
	var errw io.WriteCloser
	if !stdio.Terminal && stdio.Stderr != "" {
		errw = w
	}

	return &fileIO{
		outw: w,
		errw: errw,
		path: logFile,
	}, nil
}

// newBinaryIO runs a custom binary process for pluggable shim logging
// containerd newBinaryIO(ctx context.Context, id string, uri *url.URL) (_ runc.IO, err error)
func newBinaryIO(ctx context.Context, id string, uri *url.URL) (bio *binaryIO, err error) {
	var closers []func() error
	defer func() {
		if err == nil {
			return
		}
		var joined error = err
		for _, fn := range closers {
			joined = errors.Join(joined, fn())
		}
		err = joined
	}()

	out, err := newPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipes: %w", err)
	}
	closers = append(closers, out.Close)

	serr, err := newPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipes: %w", err)
	}
	closers = append(closers, serr.Close)

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	closers = append(closers, r.Close, w.Close)

	ns, _ := namespaces.Namespace(ctx)
	cmd := newBinaryCmd(uri, id, ns)
	cmd.ExtraFiles = append(cmd.ExtraFiles, out.r, serr.r, w)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start binary process: %w", err)
	}
	closers = append(closers, func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	})

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close write pipe after start: %w", err)
	}

	ready := make(chan error, 1)
	go func() {
		b := make([]byte, 1)
		_, readErr := r.Read(b)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			ready <- readErr
			return
		}
		ready <- nil
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case readErr := <-ready:
		if readErr != nil {
			return nil, fmt.Errorf("failed to read from logging binary: %w", readErr)
		}
	}

	return &binaryIO{
		cmd: cmd,
		out: out,
		err: serr,
	}, nil
}

func (p *pipeIO) Close() error {
	var err0, err1 error
	if p.in != nil {
		err0 = p.in.Close()
	}
	if p.out != nil {
		err1 = p.out.Close()
	}
	if err := errors.Join(err0, err1); err != nil {
		return fmt.Errorf("failed to close pipe io: %w", err)
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
	var err0, err1, err2 error
	if b.out != nil {
		err0 = b.out.Close()
	}
	if b.err != nil {
		err1 = b.err.Close()
	}
	err2 = b.cancel()
	return errors.Join(err0, err1, err2)
}

func (b *binaryIO) Stdin() io.ReadCloser {
	return nil
}

func (b *binaryIO) Stdout() io.Writer {
	return b.out.w
}

func (b *binaryIO) Stderr() io.Writer {
	if b.err == nil {
		return io.Discard
	}
	return b.err.w
}

func (f *fileIO) Close() error {
	if f.outw != nil {
		return f.outw.Close()
	}
	if f.errw != nil {
		return f.errw.Close()
	}
	return nil
}

func (f *fileIO) Stdin() io.ReadCloser {
	return nil
}

func (f *fileIO) Stdout() io.Writer {
	return f.outw
}

func (f *fileIO) Stderr() io.Writer {
	if f.errw == nil {
		return io.Discard
	}
	return f.errw
}

func (b *binaryIO) cancel() error {
	if b.cmd == nil || b.cmd.Process == nil {
		return nil
	}

	if err := b.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = b.cmd.Process.Kill()
		return err
	}

	done := make(chan error, 1)
	go func() { done <- b.cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(ioprocTimeout):
		return b.cmd.Process.Kill()
	}
}

func newBinaryCmd(binaryURI *url.URL, id, ns string) *execabs.Cmd {
	var args []string
	for k, vs := range binaryURI.Query() {
		args = append(args, k)
		if len(vs) > 0 {
			args = append(args, vs[0])
		}
	}

	cmd := execabs.Command(binaryURI.Path, args...)
	cmd.Env = append(os.Environ(),
		"CONTAINER_ID="+id,
		"CONTAINER_NAMESPACE="+ns,
	)
	return cmd
}

func newPipe() (*pipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &pipe{r: r, w: w}, nil
}

// ioCopy manages copying data between the container's IO streams and the pipe.
// It handles bidirectional communication between containerd's FIFOs and the RTOS TTY.
//
// Data flow:
//   stdin (FIFO) -> stdinPipe -> RPMSG TTY -> RTOS container
//   RTOS container -> RPMSG TTY -> stdout (FIFO) -> containerd client
//
// The function uses buffered I/O for efficiency and proper line handling.
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

	// Buffer sizes tuned for RTOS shell output
	// Larger buffer ensures we don't lose multi-line output (e.g., from 'help' command)
	const stdoutBufSize = 32 * 1024 // 32KB for stdout
	const stdinBufSize = 4 * 1024  // 4KB for stdin

	// Mica client **always** create ONE pty slave, we have to handle bytes from it
	// for all different io stream methods of containerd
	if tty.io.Stdout() != nil {
		wg.Add(1)
		go func() {
			log.Infof("[IO] Starting stdout copy from RPMSG TTY to containerd FIFO")
			defer wg.Done()
			defer func() {
				log.Debugf("[IO] Closing stdout pipe")
				if c, ok := stdoutPipe.(io.Closer); ok {
					_ = c.Close()
				}
			}()

			// Use explicit buffer for better control over data flow
			buf := make([]byte, stdoutBufSize)
			totalBytes := 0
			filteredBytes := 0

			for {
				select {
				case <-ctx.Done():
					log.Debugf("[IO] stdout copy canceled by context")
					return
				default:
				}

				nr, err := stdoutPipe.Read(buf)
				if nr > 0 {
					chunk := buf[:nr]
					totalBytes += nr

					// Filter out NUL (0x00) characters - these show up as ^@ in terminal
					// NUL bytes can come from RPMSG TTY padding or encoding issues
					filtered := filterNULBytes(chunk)
					filteredCount := nr - len(filtered)
					if filteredCount > 0 {
						filteredBytes += filteredCount
						log.Debugf("[IO] Filtered %d NUL bytes from %d total", filteredCount, nr)
					}

					// Filter extra consecutive newlines to prevent double-spacing
					// RTOS sometimes sends \n\n which becomes extra blank lines
					filtered = filterExtraNewlines(filtered)
					if len(chunk) > len(filtered) {
						log.Debugf("[IO] Filtered %d extra newline bytes", len(chunk)-len(filtered))
					}

					// Log data for debugging (first few bytes)
					if totalBytes <= 100 || nr < 50 {
						log.Debugf("[IO] stdout read %d bytes: %q", nr, string(chunk[:min(nr, 100)]))
					} else if totalBytes == stdoutBufSize+100 {
						log.Debugf("[IO] stdout logging throttled after %d bytes", totalBytes)
					}

					// Write to containerd's FIFO (filtered data)
					// For attach support: handle write errors and reopen FIFO if needed
					var writeErr error
					var nw int
					maxRetries := 3

					for attempt := 0; attempt < maxRetries; attempt++ {
						nw, writeErr = tty.io.Stdout().Write(filtered)
						if writeErr == nil {
							break // Write successful
						}

						// Check if this is a recoverable error (FIFO closed, no readers, etc.)
						if errors.Is(writeErr, io.EOF) || errors.Is(writeErr, syscall.EPIPE) {
							log.Infof("[IO] stdout FIFO closed (attempt %d/%d), reopening for attach: %v", attempt+1, maxRetries, writeErr)

							// Close the old stdout
							if closer, ok := tty.io.Stdout().(io.Closer); ok {
								closer.Close()
							}

							// Try to reopen the stdout FIFO
							time.Sleep(100 * time.Millisecond)
							newStdout, openErr := openStdoutFifo(ctx, tty.stream.Stdout)
							if openErr != nil {
								log.Debugf("[IO] Failed to reopen stdout FIFO (will retry): %v", openErr)
								time.Sleep(500 * time.Millisecond)
								continue
							}

							// Update the stdout in the IO wrapper
							if pipe, ok := tty.io.(*pipeIO); ok {
								pipe.out = newStdout
								log.Infof("[IO] Successfully reopened stdout FIFO (pipeIO)")
								// Retry the write with the new stdout
								continue
							} else {
								log.Errorf("[IO] Cannot update stdout - unknown IO type")
								return
							}
						}

						// For non-recoverable errors, log and exit
						log.Errorf("[IO] stdout write error (unrecoverable): %v", writeErr)
						return
					}

					// Check if all retries failed
					if writeErr != nil {
						// Don't exit on write failures - just log and continue reading
						// This allows the container to keep running even when attach disconnects
						// When a new attach connects, the FIFO reopen mechanism will handle reconnection
						log.Warnf("[IO] stdout write failed after %d retries (no reader, waiting for attach): %v", maxRetries, writeErr)
						// Continue to next iteration to keep reading from TTY
						continue
					}

					if nw != len(filtered) {
						log.Warnf("[IO] stdout partial write: %d/%d", nw, len(filtered))
					}

					// Try to flush if the writer supports it (for immediate output)
					if flusher, ok := tty.io.Stdout().(interface{ Flush() error }); ok {
						if ferr := flusher.Flush(); ferr != nil {
							log.Debugf("[IO] flush error (non-critical): %v", ferr)
						}
					}
				}
				if err != nil {
					if errors.Is(err, io.EOF) {
						// TTY EOF - this typically means the TTY has closed (container stopped)
						log.Infof("[IO] stdout TTY EOF reached, total bytes: %d, filtered NUL: %d", totalBytes, filteredBytes)
						return
					}
					// Check for specific errors that indicate the TTY is truly closed
					if strings.Contains(err.Error(), "use of closed") || strings.Contains(err.Error(), "file already closed") {
						log.Infof("[IO] stdout TTY closed, exiting: %v", err)
						return
					}
					// For other errors (like EAGAIN), just log and continue
					if !errors.Is(err, context.Canceled) && !errors.Is(err, syscall.EAGAIN) {
						log.Debugf("[IO] stdout read error (continuing): %v", err)
					}
					// Don't exit - continue reading to allow reconnection
				}
			}
		}()
	}

	// stdin: containerd FIFO -> RPMSG TTY
	if tty.io.Stdin() != nil {
		wg.Add(1)
		go func() {
			log.Infof("[IO] Starting stdin copy from containerd FIFO to RPMSG TTY")
			defer wg.Done()
			defer close(stdinCloser)

			buf := make([]byte, stdinBufSize)
			totalBytes := 0

			for {
				select {
				case <-ctx.Done():
					log.Debugf("[IO] stdin copy canceled by context")
					return
				default:
				}

				n, err := tty.io.Stdin().Read(buf)
				if n > 0 {
					chunk := buf[:n]
					totalBytes += n

					// Log data for debugging (helpful for control character debugging)
					safeChunk := chunk
					if len(chunk) > 100 {
						safeChunk = chunk[:100]
					}
					log.Debugf("[IO] stdin read %d bytes: %q", n, string(safeChunk))

					// Check for control characters (Ctrl+C, etc.)
					if sig, ok := control.detect(chunk); ok {
						log.Infof("[IO] Captured control character, sending interrupt signal")
						// Write friendly exit message to stdout
						// Use carriage return + newline to get to a new line
						if tty.io.Stdout() != nil {
							fmt.Fprint(tty.io.Stdout(), "\r\n")
							if flusher, ok := tty.io.Stdout().(interface{ Flush() error }); ok {
								flusher.Flush()
							}
						}
						notifyInterrupt(sig, "host-control")
						return
					}

					if stdinPipe == nil {
						log.Debugf("[IO] stdin pipe is nil, stopping stdin copy")
						return
					}

					// Write to TTY
					written, werr := stdinPipe.Write(chunk)
					if werr != nil {
						log.Errorf("[IO] stdin write error: %v", werr)
						return
					}
					if written != n {
						log.Warnf("[IO] stdin partial write: %d/%d", written, n)
					}
				}
				if err != nil {
					// Check for EOF or closed FIFO errors (both indicate the writer has closed)
					isEOFOrClosed := errors.Is(err, io.EOF)
					// Also check for "closed fifo" error string from fifo package
					if !isEOFOrClosed && err != nil {
						// Use string match as fallback for fifo package errors
						isEOFOrClosed = strings.Contains(err.Error(), "closed fifo") ||
							strings.Contains(err.Error(), "use of closed")
					}

					if isEOFOrClosed {
						// For attach support: When ctr task start -d closes stdin,
						// we get EOF. Don't exit - instead reopen the FIFO to wait
						// for ctr task attach to reconnect.
						log.Infof("[IO] stdin EOF/closed after %d bytes (writer closed, reopening FIFO for attach)", totalBytes)
						// Close the old stdin
						tty.io.Stdin().Close()

						// Keep trying to reopen the stdin FIFO until successful
						// This allows attach to reconnect at any time
						for {
							// Check if context is canceled while waiting
							select {
							case <-ctx.Done():
								log.Debugf("[IO] stdin reopen canceled by context")
								return
							default:
							}

							newStdin, openErr := openStdinFifo(ctx, tty.stream.Stdin)
							if openErr == nil {
								// Successfully reopened - update the stdin in the IO wrapper
								// Try pipeIO first (most common case for FIFO)
								if pipe, ok := tty.io.(*pipeIO); ok {
									pipe.in = newStdin
									log.Infof("[IO] Successfully reopened stdin FIFO (pipeIO) for attach reconnection")
									totalBytes = 0
									break // Exit the reopen loop and continue reading
								} else if wrapped, ok := tty.io.(*stdinWrappedIO); ok {
									wrapped.stdin = newStdin
									log.Infof("[IO] Successfully reopened stdin FIFO (stdinWrappedIO) for attach reconnection")
									totalBytes = 0
									break
								} else {
									log.Errorf("[IO] Cannot update stdin - tty.io is unknown type")
									return
								}
							}

							// Reopen failed (likely no writer yet), wait and retry
							log.Debugf("[IO] FIFO reopen attempt failed (waiting for attach): %v", openErr)
							time.Sleep(500 * time.Millisecond)
						}
						continue // Continue the main read loop with the new stdin
					}
					// EAGAIN means no data available yet (non-blocking read), continue waiting
					if errors.Is(err, syscall.EAGAIN) {
						// Brief sleep before retrying to avoid busy waiting
						time.Sleep(10 * time.Millisecond)
						continue
					}
					// Log other non-cancellation errors
					if !errors.Is(err, context.Canceled) {
						log.Debugf("[IO] stdin error: %v", err)
					}
					return
				}
			}
		}()
	} else {
		log.Debugf("[IO] No stdin stream, closing stdinCloser immediately")
		close(stdinCloser)
	}

	log.Debugf("[IO] Waiting for IO copy goroutines to complete")
	wg.Wait()
	close(exitch)
	log.Infof("[IO] All IO copies completed")
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// filterNULBytes filters out NUL (0x00) bytes from the input.
// NUL bytes can appear in RPMSG TTY data due to padding or encoding issues,
// and they show up as ^@ in terminal output which is undesirable.
func filterNULBytes(data []byte) []byte {
	// First, check if there are any NUL bytes
	hasNUL := false
	for _, b := range data {
		if b == 0 {
			hasNUL = true
			break
		}
	}
	if !hasNUL {
		return data
	}

	// Filter out NUL bytes
	result := make([]byte, 0, len(data))
	for _, b := range data {
		if b != 0 {
			result = append(result, b)
		}
	}
	return result
}

// filterExtraNewlines removes extra consecutive newlines to prevent double-spacing.
// RTOS sometimes sends \n\n which with ONLCR becomes \r\n\r\n, causing blank lines.
// This function collapses consecutive newlines (with or without CR) to single \r\n.
func filterExtraNewlines(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	// Check if there are consecutive newlines
	hasExtraNewlines := false
	for i := 0; i < len(data)-1; i++ {
		// Check for \n\n or \r\n\r\n or \r\n\n patterns
		if data[i] == '\n' && data[i+1] == '\n' {
			hasExtraNewlines = true
			break
		}
		if i < len(data)-2 && data[i] == '\r' && data[i+1] == '\n' && data[i+2] == '\n' {
			hasExtraNewlines = true
			break
		}
	}
	if !hasExtraNewlines {
		return data
	}

	// Filter: collapse consecutive newlines to single \r\n
	result := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		if data[i] == '\r' {
			result = append(result, '\r')
			if i+1 < len(data) && data[i+1] == '\n' {
				result = append(result, '\n')
				i += 2
			} else {
				i++
			}
			// Skip any consecutive \r or \n
			for i < len(data) && (data[i] == '\r' || data[i] == '\n') {
				i++
			}
		} else if data[i] == '\n' {
			result = append(result, '\r', '\n')
			i++
			// Skip any consecutive \r or \n
			for i < len(data) && (data[i] == '\r' || data[i] == '\n') {
				i++
			}
		} else {
			result = append(result, data[i])
			i++
		}
	}
	return result
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

// getDurationAnnotation parses a duration annotation from the container spec with a default value.
// Supports both duration string format (e.g., "300s", "5m") and plain integer seconds (e.g., "300").
// Returns (value, isExplicitlySet) where isExplicitlySet indicates if the annotation was provided.
func getDurationAnnotation(spec *specs.Spec, key string, defaultValue time.Duration) (time.Duration, bool) {
	if spec == nil || spec.Annotations == nil {
		return defaultValue, false
	}

	if value, ok := spec.Annotations[key]; ok {
		log.Debugf("[getDurationAnnotation] Parsing annotation %s with value: %s", key, value)
		// Try parsing as duration string first (e.g., "300s", "5m")
		duration, parseErr := time.ParseDuration(value)
		log.Debugf("[getDurationAnnotation] time.ParseDuration result: duration=%v err=%v", duration, parseErr)
		if parseErr == nil {
			if duration > 0 {
				log.Infof("[getDurationAnnotation] Successfully parsed duration: %s -> %v", value, duration)
				return duration, true
			}
			log.Warnf("annotation %s has invalid duration %s, using default %v", key, value, defaultValue)
		} else {
			// Fallback to parsing as plain integer seconds (for backward compatibility)
			log.Debugf("[getDurationAnnotation] time.ParseDuration failed, trying strconv.ParseInt")
			if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
				duration := time.Duration(seconds) * time.Second
				if duration > 0 {
					log.Infof("[getDurationAnnotation] Successfully parsed as integer seconds: %s -> %v", value, duration)
					return duration, true
				}
				log.Warnf("annotation %s has invalid duration %s, using default %v", key, value, defaultValue)
			} else {
				log.Warnf("annotation %s parse error: %v, defaulting to %v", key, err, defaultValue)
			}
		}
	}
	return defaultValue, false
}
