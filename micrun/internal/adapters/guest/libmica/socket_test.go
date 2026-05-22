package libmica

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	defs "micrun/internal/support/definitions"
)

func TestMicaSocketRxReturnsContextErrorWhenCanceled(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ms := &micaSocket{conn: client}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ms.rx(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("rx canceled error = %v, want context.Canceled", err)
	}
}

func TestMicaSocketTxReturnsContextErrorWhenCanceled(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ms := &micaSocket{conn: client}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ms.tx(ctx, []byte("status"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("tx canceled error = %v, want context.Canceled", err)
	}
}

func TestSocketDeadlineUsesEarlierContextDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Millisecond))
	defer cancel()

	deadline := socketDeadline(ctx)
	ctxDeadline, _ := ctx.Deadline()
	if !deadline.Equal(ctxDeadline) {
		t.Fatalf("socketDeadline = %v, want context deadline %v", deadline, ctxDeadline)
	}
}

func TestSocketDeadlineAtUsesDefaultTimeout(t *testing.T) {
	now := time.Date(2026, 4, 27, 16, 17, 18, 0, time.UTC)

	deadline := socketDeadlineAt(context.Background(), now)

	if !deadline.Equal(now.Add(defs.MicaSocketTimeout)) {
		t.Fatalf("socketDeadlineAt = %v, want %v", deadline, now.Add(defs.MicaSocketTimeout))
	}
}

func TestMicaSocketDeadlineUsesInjectedClock(t *testing.T) {
	now := time.Date(2026, 4, 27, 17, 18, 19, 0, time.UTC)
	ms := &micaSocket{now: func() time.Time { return now }}

	deadline := ms.deadline(context.Background())

	if !deadline.Equal(now.Add(defs.MicaSocketTimeout)) {
		t.Fatalf("mica socket deadline = %v, want %v", deadline, now.Add(defs.MicaSocketTimeout))
	}
}

func TestTransientMicaSocketConnectError(t *testing.T) {
	tests := map[string]struct {
		err  error
		want bool
	}{
		"missing socket path": {
			err:  &net.OpError{Op: "dial", Net: "unix", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ENOENT}},
			want: true,
		},
		"socket listener not ready": {
			err:  &net.OpError{Op: "dial", Net: "unix", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}},
			want: true,
		},
		"unexpected permission failure": {
			err:  &net.OpError{Op: "dial", Net: "unix", Err: &os.SyscallError{Syscall: "connect", Err: syscall.EACCES}},
			want: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := transientMicaSocketConnectError(tt.err); got != tt.want {
				t.Fatalf("transientMicaSocketConnectError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMicaResponseCompleteRecognizesTerminalMarkers(t *testing.T) {
	tests := map[string]bool{
		"partial output":          false,
		"payload MICA-SUCCESS":    true,
		"payload MICA-FAILED":     true,
		"MICA-SUCCESS trailing":   true,
		"MICA-FAILED diagnostic":  true,
		"payload MICA-INCOMPLETE": false,
	}

	for response, want := range tests {
		if got := micaResponseComplete(response); got != want {
			t.Fatalf("micaResponseComplete(%q) = %v, want %v", response, got, want)
		}
	}
}

func TestClientExistsReturnsContextErrorWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ClientExists(ctx, "demo")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ClientExists canceled error = %v, want context.Canceled", err)
	}
}
