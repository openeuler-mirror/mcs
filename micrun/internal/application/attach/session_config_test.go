package attach

import (
	"bytes"
	"testing"

	"micrun/internal/ports"
)

type nopWriteCloser struct {
	*bytes.Buffer
}

func (n nopWriteCloser) Close() error {
	return nil
}

func TestIOSessionConfigFromAttachInfoCopiesUserVisibleIOFields(t *testing.T) {
	ttyIn := nopWriteCloser{bytes.NewBuffer(nil)}
	ttyOut := bytes.NewBuffer(nil)
	ttyErr := bytes.NewBuffer(nil)
	attachInfo := ports.AttachInfo{
		Stdin:    "/run/stdin",
		Stdout:   "/run/stdout",
		Stderr:   "/run/stderr",
		Terminal: true,
		TTYIn:    ttyIn,
		TTYOut:   ttyOut,
		TTYErr:   ttyErr,
	}

	got := ioSessionConfigFromAttachInfo("container1", attachInfo)

	if got.ContainerID != "container1" || got.StdinFIFO != attachInfo.Stdin || got.StdoutFIFO != attachInfo.Stdout || got.StderrFIFO != attachInfo.Stderr {
		t.Fatalf("session config paths = %+v", got)
	}
	if got.TTYIn != ttyIn || got.TTYOut != ttyOut || got.TTYErr != ttyErr {
		t.Fatalf("session config TTYs = %+v", got)
	}
	if !got.Terminal || !got.FilterNUL {
		t.Fatalf("session config flags = terminal:%v filterNUL:%v", got.Terminal, got.FilterNUL)
	}
}
