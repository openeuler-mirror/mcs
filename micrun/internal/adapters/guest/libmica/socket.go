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
	"syscall"
	"time"
)

var (
	ErrSocketNotConnected   = errors.New("socket not connected")
	ErrUnexpectedRespFormat = errors.New("unexpected response format")
	ErrMicadTimeout         = errors.New("timeout while waiting for micad response")
)

type micaSocket struct {
	socketPath string
	conn       net.Conn
	now        timex.Clock
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

// Helper functions
func validSocketPath(socketPath string) bool {
	if st, err := os.Stat(socketPath); err != nil {
		return false
	} else {
		return st.Mode()&os.ModeSocket != 0
	}
}

// micaSocket methods
func (ms *micaSocket) connect(ctx context.Context) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", ms.socketPath)
	if err != nil {
		if transientMicaSocketConnectError(err) {
			log.Debugf("mica control socket unavailable; path=%s err=%v", ms.socketPath, err)
		} else {
			log.Errorf("failed to connect to mica control socket; path=%s err=%v", ms.socketPath, err)
		}
		return err
	}
	ms.conn = conn
	return nil
}

func transientMicaSocketConnectError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED)
}

func (ms *micaSocket) close() error {
	if ms.conn != nil {
		return ms.conn.Close()
	}
	return nil
}

func (ms *micaSocket) tx(ctx context.Context, data []byte) error {
	ctx = contextx.OrBackground(ctx)
	if ms.conn == nil {
		return ErrSocketNotConnected
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ms.conn.SetWriteDeadline(ms.deadline(ctx)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}
	_, err := ms.conn.Write(data)
	if err != nil {
		return contextAwareSocketError(ctx, err)
	}
	return err
}

func parseMicaSocketResponse(responseBuffer string) (*micaSocketResponse, error) {
	if strings.Contains(responseBuffer, defs.MicaFailed) {
		payload := strings.TrimSpace(strings.SplitN(responseBuffer, defs.MicaFailed, 2)[0])
		if payload != "" {
			log.Error(payload)
		}
		return &micaSocketResponse{payload: payload, status: defs.MicaFailed}, nil
	}

	if strings.Contains(responseBuffer, defs.MicaSuccess) {
		payload := strings.TrimSpace(strings.SplitN(responseBuffer, defs.MicaSuccess, 2)[0])
		if payload != "" {
			log.Info(payload)
		}
		return &micaSocketResponse{payload: payload, status: defs.MicaSuccess}, nil
	}

	return nil, ErrUnexpectedRespFormat
}

func micaResponseComplete(responseBuffer string) bool {
	return strings.Contains(responseBuffer, defs.MicaFailed) || strings.Contains(responseBuffer, defs.MicaSuccess)
}

func (ms *micaSocket) rx(ctx context.Context) (*micaSocketResponse, error) {
	ctx = contextx.OrBackground(ctx)
	if ms.conn == nil {
		return nil, ErrSocketNotConnected
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := ms.conn.SetReadDeadline(ms.deadline(ctx)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	var responseBuffer strings.Builder
	buf := make([]byte, defs.MicaSocketBufSize)

	for {
		n, err := ms.conn.Read(buf)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return nil, ErrMicadTimeout
			}
			return nil, err
		}

		if n == 0 {
			break
		}

		responseBuffer.Write(buf[:n])

		response := responseBuffer.String()
		if micaResponseComplete(response) {
			return parseMicaSocketResponse(response)
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
	return socketDeadlineAt(ctx, timex.Now(nil))
}

func (ms *micaSocket) deadline(ctx context.Context) time.Time {
	if ms == nil {
		return socketDeadline(ctx)
	}
	return socketDeadlineAt(ctx, timex.Now(ms.now))
}

func socketDeadlineAt(ctx context.Context, now time.Time) time.Time {
	deadline := now.Add(defs.MicaSocketTimeout)
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
