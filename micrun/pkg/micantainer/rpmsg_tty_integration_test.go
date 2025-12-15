//go:build linux

package micantainer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	script := filepath.Join("..", "..", "tests", "mock_micad", "quick_pty.py")
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
	var outBuf bytes.Buffer
	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		_, _ = io.Copy(&outBuf, out)
	}()

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
		if strings.Contains(outBuf.String(), "Virt pty established") {
			break
		}
		if strings.Contains(outBuf.String(), "out of pty devices") {
			t.Skip("no PTY devices available in this environment")
		}
		select {
		case <-waitReady.C:
			t.Fatalf("quick_pty did not become ready, output: %q", outBuf.String())
		case <-outDone:
			if strings.Contains(outBuf.String(), "out of pty devices") {
				t.Skip("no PTY devices available in this environment")
			}
			t.Fatalf("quick_pty exited early, output: %q", outBuf.String())
		case <-time.After(10 * time.Millisecond):
		}
	}

	stdin, stdout, _, err := dialTTY(ctx, id)
	if err != nil {
		if strings.Contains(outBuf.String(), "out of pty devices") {
			t.Skip("no PTY devices available in this environment")
		}
		t.Fatalf("dialRPMSGTTY: %v (quick_pty output: %q)", err, outBuf.String())
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
