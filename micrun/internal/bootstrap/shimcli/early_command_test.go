package shimcli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHandleEarlyCommandPrintsHelp(t *testing.T) {
	var out bytes.Buffer
	startup := NewStartup("io.containerd.mica.v2", []string{"micrun", "--help"})

	if !HandleEarlyCommand(startup, &out) {
		t.Fatal("HandleEarlyCommand() = false, want true")
	}
	if got := out.String(); !strings.Contains(got, "Usage: containerd-shim-mica-v2") {
		t.Fatalf("help output = %q, want usage line", got)
	}
}

func TestHandleEarlyCommandIgnoresFlagValues(t *testing.T) {
	var out bytes.Buffer
	startup := NewStartup("io.containerd.mica.v2", []string{"micrun", "-id", "--help"})

	if HandleEarlyCommand(startup, &out) {
		t.Fatal("HandleEarlyCommand() = true for flag value, want false")
	}
	if out.Len() != 0 {
		t.Fatalf("early command output = %q, want empty", out.String())
	}
}

func TestHandleEarlyCommandSupportsEqualsForm(t *testing.T) {
	var out bytes.Buffer
	startup := NewStartup("io.containerd.mica.v2", []string{"micrun", "--help=true"})

	if !HandleEarlyCommand(startup, &out) {
		t.Fatal("HandleEarlyCommand() = false, want true")
	}
	if got := out.String(); !strings.Contains(got, "Usage: containerd-shim-mica-v2") {
		t.Fatalf("help output = %q, want usage line", got)
	}
}

func TestHandleEarlyCommandHonorsExplicitFalse(t *testing.T) {
	var out bytes.Buffer
	startup := NewStartup("io.containerd.mica.v2", []string{"micrun", "--help=false"})

	if HandleEarlyCommand(startup, &out) {
		t.Fatal("HandleEarlyCommand() = true for --help=false, want false")
	}
	if out.Len() != 0 {
		t.Fatalf("early command output = %q, want empty", out.String())
	}
}

func TestHandleEarlyCommandIgnoresOptionsAfterAction(t *testing.T) {
	var out bytes.Buffer
	startup := NewStartup("io.containerd.mica.v2", []string{"micrun", "-debug", "start", "--help"})

	if HandleEarlyCommand(startup, &out) {
		t.Fatal("HandleEarlyCommand() = true for action argument, want false")
	}
	if out.Len() != 0 {
		t.Fatalf("early command output = %q, want empty", out.String())
	}
}
