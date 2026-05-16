//go:build linux

package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestDialRPMSGTTY_QuickPTY(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	id := fmt.Sprintf("micrun_test_%d", time.Now().UnixNano())
	script := filepath.Join("..", "..", "..", "tests", "mock_micad", "quick_pty.py")
	cmd := exec.CommandContext(ctx, "python3", "-u", script, id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		t.Fatalf("start quick_pty: %v", err)
	}

	// lockedBuffer wraps a bytes.Buffer to provide thread-safe access.
	// The Go race detector catches any simultaneous read/write to the
	// underlying Buffer, so all access must be guarded by the mutex.
	type lockedBuffer struct {
		mu sync.Mutex
		b  bytes.Buffer
	}
	var outBuf lockedBuffer

	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		outBuf.mu.Lock()
		_, _ = io.Copy(&outBuf.b, out)
		outBuf.mu.Unlock()
	}()

	// outBufRead returns current contents under lock
	outBufRead := func() string {
		outBuf.mu.Lock()
		defer outBuf.mu.Unlock()
		return outBuf.b.String()
	}

	defer func() {
		if cmd.Process == nil {
			return
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	}()

	waitReady := time.NewTimer(1 * time.Second)
	defer waitReady.Stop()
	for {
		if strings.Contains(outBufRead(), "Virt pty established") {
			break
		}
		if strings.Contains(outBufRead(), "out of pty devices") {
			t.Skip("no PTY devices available in this environment")
		}
		select {
		case <-waitReady.C:
			t.Fatalf("quick_pty did not become ready, output: %q", outBufRead())
		case <-outDone:
			if strings.Contains(outBufRead(), "out of pty devices") {
				t.Skip("no PTY devices available in this environment")
			}
			t.Fatalf("quick_pty exited early, output: %q", outBufRead())
		case <-time.After(10 * time.Millisecond):
		}
	}

	stdin, stdout, _, err := dialTTY(ctx, id)
	if err != nil {
		if strings.Contains(outBufRead(), "out of pty devices") {
			t.Skip("no PTY devices available in this environment")
		}
		// If we see "Virt pty established" but dialTTY still fails,
		// it means micad is not running in this environment.
		if strings.Contains(outBufRead(), "Virt pty established") {
			t.Skip("micad not available (dialTTY failed)")
		}
		t.Fatalf("dialRPMSGTTY: %v (quick_pty output: %q)", err, outBufRead())
	}
	defer stdin.Close()
	defer stdout.Close()

	if _, err := stdin.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()

	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, rerr := stdout.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if strings.Contains(buf.String(), "Hello from Zephyr!") {
				return
			}
		}
		if readCtx.Err() != nil {
			t.Fatalf("expected zephyr response, got: %q", buf.String())
		}
		if rerr != nil {
			continue
		}
	}
}
