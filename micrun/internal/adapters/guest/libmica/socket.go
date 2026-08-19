package libmica

import (
	"context"
	"errors"
	"fmt"
	"micrun/internal/support/contextx"
	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
	"micrun/internal/support/timex"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	ErrSocketNotConnected   = errors.New("socket not connected")
	ErrUnexpectedRespFormat = errors.New("unexpected response format")
	ErrMicadTimeout         = errors.New("timeout while waiting for micad response")
)

// micaMaxResponseSize bounds the accumulated micad socket response. The
// protocol is marker-terminated ("MICA-SUCCESS"/"MICA-FAILED"); without a
// cap a misbehaving micad could stream data until the deadline and exhaust
// memory. 64 KiB is far above any legitimate status/presence payload.
const micaMaxResponseSize = 64 * 1024

type micaSocket struct {
	socketPath string
	conn       net.Conn
	now        timex.Clock
	closeMu    sync.Mutex
	// timeout overrides the default control-message deadline. Long-running
	// synchronous daemon operations (create/start/rm) set a longer budget;
	// zero means defs.MicaSocketTimeout.
	timeout time.Duration
}

type micaSocketResponse struct {
	payload string
	status  string
}

func (r *micaSocketResponse) err() error {
	if r == nil {
		return ErrUnexpectedRespFormat
	}

	switch r.status {
	case defs.MicaSuccess:
		return nil
	case defs.MicaFailed:
		if r.payload != "" {
			return fmt.Errorf("mica daemon reported failure: %s", r.payload)
		}
		return fmt.Errorf("mica daemon reported failure")
	default:
		return fmt.Errorf("unexpected response format from mica daemon: %s, communication might broken?", r.status)
	}
}

func (r *micaSocketResponse) payloadOrError() (string, error) {
	if err := r.err(); err != nil {
		return "", err
	}
	return r.payload, nil
}

// Constructors
func newMicaSocket(socketPath string) *micaSocket {
	return &micaSocket{socketPath: socketPath}
}

// validSocketPath checks whether a Unix-domain socket exists at socketPath
// AND its listener is alive. A bare os.Stat cannot distinguish a live
// listener from a stale socket file left by a crashed micad — the file
// retains ModeSocket after the listener dies.
//
// A bounded connect distinguishes three cases:
//   - Success: listener is alive → true.
//   - ECONNREFUSED: socket file exists but no listener (micad crashed) → false,
//     so the xl-destroy fallback is reached.
//   - Timeout / other error: micad may be busy processing a long operation
//     (its synchronous budget is 30s, longer than the probe's 5s). Treating
//     this as stale would xl-destroy a healthy domain. Return true (assume
//     alive) so the normal RPC path runs; the caller's own deadline will
//     eventually surface a real timeout.
func validSocketPath(socketPath string) bool {
	st, err := os.Stat(socketPath)
	if err != nil || st.Mode()&os.ModeSocket == 0 {
		return false
	}
	conn, err := (&net.Dialer{Timeout: defs.MicaSocketTimeout}).Dial("unix", socketPath)
	if err != nil {
		// Only ECONNREFUSED definitively means the listener is gone.
		// A timeout or other transient error means "uncertain" — assume
		// alive to avoid xl-destroying a domain micad still manages.
		return !isConnectionRefused(err)
	}
	_ = conn.Close()
	return true
}

func isConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

// micaSocket methods
func (ms *micaSocket) connect(ctx context.Context) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := (&net.Dialer{Timeout: defs.MicaSocketTimeout}).DialContext(ctx, "unix", ms.socketPath)
	if err != nil {
		if transientMicaSocketConnectError(err) {
			log.Debugf("mica control socket unavailable; path=%s err=%v", ms.socketPath, err)
		} else {
			log.Errorf("failed to connect to mica control socket; path=%s err=%v", ms.socketPath, err)
		}
		return err
	}
	// Publish the connection under closeMu so it cannot race with close()
	// (which nils conn under the same lock) and a concurrent tx/rx either
	// sees the new conn or the pre-close nil — never a torn write.
	ms.closeMu.Lock()
	ms.conn = conn
	ms.closeMu.Unlock()
	return nil
}

func transientMicaSocketConnectError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED)
}

func (ms *micaSocket) close() error {
	ms.closeMu.Lock()
	defer ms.closeMu.Unlock()
	conn := ms.conn
	ms.conn = nil
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (ms *micaSocket) tx(ctx context.Context, data []byte) error {
	ctx = contextx.OrBackground(ctx)
	ms.closeMu.Lock()
	conn := ms.conn
	ms.closeMu.Unlock()
	if conn == nil {
		return ErrSocketNotConnected
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(ms.deadline(ctx)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}
	_, err := conn.Write(data)
	if err != nil {
		return contextAwareSocketError(ctx, err)
	}
	return nil
}

func parseMicaSocketResponse(responseBuffer string) (*micaSocketResponse, error) {
	// micad sends payload first, then the status marker. Use the LAST
	// occurrence of the marker so a payload that happens to contain the
	// literal "MICA-FAILED"/"MICA-SUCCESS" (e.g. a guest name or a diagnostic
	// line echoed over the control socket) does not shadow the real trailing
	// marker.
	status, markerIdx, ok := lastMicaMarker(responseBuffer)
	if ok {
		payload := strings.TrimSpace(responseBuffer[:markerIdx])
		if status == defs.MicaFailed && payload != "" {
			log.Error(payload)
		} else if payload != "" {
			log.Info(payload)
		}
		return &micaSocketResponse{payload: payload, status: status}, nil
	}

	return nil, ErrUnexpectedRespFormat
}

// lastMicaMarker returns the status and start index of the marker that
// appears LATEST in the buffer. micad appends the real marker after the
// payload, so the trailing marker wins even if the payload itself contains
// a literal "MICA-FAILED" or "MICA-SUCCESS" string (e.g. a diagnostic line
// echoed over the control socket).
func lastMicaMarker(responseBuffer string) (status string, idx int, ok bool) {
	failedIdx := strings.LastIndex(responseBuffer, defs.MicaFailed)
	successIdx := strings.LastIndex(responseBuffer, defs.MicaSuccess)
	// Return whichever marker appears later in the buffer.
	if failedIdx >= 0 && failedIdx >= successIdx {
		return defs.MicaFailed, failedIdx, true
	}
	if successIdx >= 0 {
		return defs.MicaSuccess, successIdx, true
	}
	return "", -1, false
}

func micaResponseComplete(responseBuffer string) bool {
	_, _, ok := lastMicaMarker(responseBuffer)
	return ok
}

func (ms *micaSocket) rx(ctx context.Context) (*micaSocketResponse, error) {
	ctx = contextx.OrBackground(ctx)
	ms.closeMu.Lock()
	conn := ms.conn
	ms.closeMu.Unlock()
	if conn == nil {
		return nil, ErrSocketNotConnected
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := conn.SetReadDeadline(ms.deadline(ctx)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	var responseBuffer strings.Builder
	buf := make([]byte, defs.MicaSocketBufSize)

	for {
		n, err := conn.Read(buf)
		// Per the io.Reader contract, a Read may return a non-zero n
		// together with a non-nil error (e.g. io.EOF when the server sends
		// the final marker and closes in one shot). Accumulate the valid
		// bytes BEFORE checking the error so we don't discard the payload.
		if n > 0 {
			responseBuffer.Write(buf[:n])
			if responseBuffer.Len() > micaMaxResponseSize {
				return nil, fmt.Errorf("mica response exceeds %d bytes without a completion marker", micaMaxResponseSize)
			}
			response := responseBuffer.String()
			if micaResponseComplete(response) {
				return parseMicaSocketResponse(response)
			}
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return nil, ErrMicadTimeout
			}
			// If we accumulated a complete response but hit EOF, treat it
			// as success (the buffer was already checked above).
			return nil, err
		}
		if n == 0 {
			break
		}
	}

	return nil, ErrUnexpectedRespFormat
}

func (ms *micaSocket) roundTrip(ctx context.Context, msg []byte) (*micaSocketResponse, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ms.connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to socket: %w", err)
	}
	stopClose := context.AfterFunc(ctx, func() {
		_ = ms.close()
	})
	defer stopClose()
	defer func() {
		ms.close()
	}()

	if err := ms.tx(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	response, err := ms.rx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}

	return response, nil
}

func socketDeadline(ctx context.Context) time.Time {
	return socketDeadlineAt(ctx, timex.Now(nil), defs.MicaSocketTimeout)
}

func (ms *micaSocket) deadline(ctx context.Context) time.Time {
	timeout := defs.MicaSocketTimeout
	if ms != nil && ms.timeout > 0 {
		timeout = ms.timeout
	}
	if ms == nil {
		return socketDeadlineAt(ctx, timex.Now(nil), timeout)
	}
	return socketDeadlineAt(ctx, timex.Now(ms.now), timeout)
}

func socketDeadlineAt(ctx context.Context, now time.Time, timeout time.Duration) time.Time {
	deadline := now.Add(timeout)
	if ctxDeadline, ok := contextx.OrBackground(ctx).Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}

func contextAwareSocketError(ctx context.Context, err error) error {
	if ctxErr := contextx.OrBackground(ctx).Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (ms *micaSocket) handleMsg(ctx context.Context, msg []byte) error {
	response, err := ms.roundTrip(ctx, msg)
	if err != nil {
		return err
	}

	return response.err()
}

func (ms *micaSocket) handleMsgWithResponse(ctx context.Context, msg []byte) (string, error) {
	response, err := ms.roundTrip(ctx, msg)
	if err != nil {
		return "", err
	}

	return response.payloadOrError()
}
