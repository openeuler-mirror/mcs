package shim

import (
	"testing"

	micrunio "micrun/internal/adapters/io"
	"micrun/internal/ports"
)

func TestIOModeKindMatchesString(t *testing.T) {
	mode := IOMode{IsTTY: true, IsForeground: false, HasStdin: true}

	if got := mode.Kind(); got != IOModeTTYBackground {
		t.Fatalf("Kind() = %v, want %v", got, IOModeTTYBackground)
	}
	if got := mode.String(); got != IOModeTTYBackground.String() {
		t.Fatalf("String() = %q, want %q", got, IOModeTTYBackground.String())
	}
}

func TestDetermineIOModeClassifiesTTYReadOnly(t *testing.T) {
	mode := DetermineIOMode(ports.TaskCreateRequest{
		ID:       "container-a",
		Terminal: true,
		Stdout:   "/provided/stdout",
	})

	if got := mode.Kind(); got != IOModeTTYReadOnly {
		t.Fatalf("Kind() = %v, want %v", got, IOModeTTYReadOnly)
	}
	if mode.HasStdin {
		t.Fatal("TTY read-only mode should not report stdin")
	}
	if mode.SupportsDetach {
		t.Fatal("TTY read-only mode should not support detach")
	}
}

func TestDetermineIOModeClassifiesTTYBackgroundWithPlaceholderStdin(t *testing.T) {
	mode := DetermineIOMode(ports.TaskCreateRequest{
		ID:       "container-a",
		Terminal: true,
		Stdin:    "binary://stdin",
		Stdout:   "binary://stdout",
	})

	if got := mode.Kind(); got != IOModeTTYBackground {
		t.Fatalf("Kind() = %v, want %v", got, IOModeTTYBackground)
	}
	if mode.IsForeground {
		t.Fatal("placeholder stdin/stdout should not make TTY mode foreground")
	}
	if !mode.HasStdin {
		t.Fatal("placeholder stdin should still record a stdin request")
	}
}

func TestDetermineIOModeIgnoresBlankStdinRequest(t *testing.T) {
	mode := DetermineIOMode(ports.TaskCreateRequest{
		ID:       "container-a",
		Terminal: true,
		Stdin:    " \t ",
		Stdout:   "binary://stdout",
	})

	if mode.HasStdin {
		t.Fatal("blank stdin should not count as a stdin request")
	}
	if got := mode.Kind(); got != IOModeTTYReadOnly {
		t.Fatalf("Kind() = %v, want %v", got, IOModeTTYReadOnly)
	}
}

func TestDetermineIOModeClassifiesNonTTYForegroundWithoutStdin(t *testing.T) {
	mode := DetermineIOMode(ports.TaskCreateRequest{
		ID:     "container-a",
		Stdout: "/provided/stdout",
		Stderr: "/provided/stderr",
	})

	if got := mode.Kind(); got != IOModeNonTTYForeground {
		t.Fatalf("Kind() = %v, want %v", got, IOModeNonTTYForeground)
	}
	if !mode.IsForeground {
		t.Fatal("valid stdout/stderr should make non-TTY mode foreground")
	}
}

func TestGenerateFIFOPathsNonTTYForegroundGeneratesInvalidStdin(t *testing.T) {
	req := ports.TaskCreateRequest{
		ID:     "container-a",
		Stdin:  " \t ",
		Stdout: "/provided/stdout",
		Stderr: "/provided/stderr",
	}

	stdin, stdout, stderr := GenerateFIFOPaths(req, "default")

	if want := micrunio.GenerateStandardFIFOPath("default", req.ID, "stdin"); stdin != want {
		t.Fatalf("stdin = %q, want %q", stdin, want)
	}
	if stdout != req.Stdout || stderr != req.Stderr {
		t.Fatalf("stdout/stderr = (%q, %q), want provided paths", stdout, stderr)
	}
}

func TestGenerateFIFOPathsTTYForegroundUsesProvidedPaths(t *testing.T) {
	req := ports.TaskCreateRequest{
		ID:       "container-a",
		Terminal: true,
		Stdin:    "/provided/stdin",
		Stdout:   "/provided/stdout",
		Stderr:   "/provided/stderr",
	}

	stdin, stdout, stderr := GenerateFIFOPaths(req, "default")

	if stdin != req.Stdin || stdout != req.Stdout || stderr != req.Stderr {
		t.Fatalf("paths = (%q, %q, %q), want provided paths", stdin, stdout, stderr)
	}
}

func TestGenerateFIFOPathsTTYForegroundNormalizesPlaceholderStdin(t *testing.T) {
	req := ports.TaskCreateRequest{
		ID:       "container-a",
		Terminal: true,
		Stdin:    "binary://stdin",
		Stdout:   "/provided/stdout",
		Stderr:   "/provided/stderr",
	}

	stdin, stdout, stderr := GenerateFIFOPaths(req, "default")

	if want := micrunio.GenerateStandardFIFOPath("default", req.ID, "stdin"); stdin != want {
		t.Fatalf("stdin = %q, want %q", stdin, want)
	}
	if stdout != req.Stdout || stderr != req.Stderr {
		t.Fatalf("stdout/stderr = (%q, %q), want provided paths", stdout, stderr)
	}
}

func TestGenerateFIFOPathsTTYForegroundNormalizesPlaceholderOutputs(t *testing.T) {
	req := ports.TaskCreateRequest{
		ID:       "container-a",
		Terminal: true,
		Stdin:    "/provided/stdin",
		Stdout:   "binary://stdout",
		Stderr:   " \t ",
	}

	stdin, stdout, stderr := GenerateFIFOPaths(req, "default")

	if stdin != req.Stdin {
		t.Fatalf("stdin = %q, want provided path", stdin)
	}
	if want := micrunio.GenerateStandardFIFOPath("default", req.ID, "stdout"); stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	if want := micrunio.GenerateStandardFIFOPath("default", req.ID, "stderr"); stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestGenerateFIFOPathsNonTTYBackgroundGeneratesInvalidPaths(t *testing.T) {
	req := ports.TaskCreateRequest{
		ID:     "container-a",
		Stdout: "binary://stdout",
		Stderr: " /invalid/stderr ",
	}

	stdin, stdout, stderr := GenerateFIFOPaths(req, "default")

	if want := micrunio.GenerateStandardFIFOPath("default", req.ID, "stdin"); stdin != want {
		t.Fatalf("stdin = %q, want %q", stdin, want)
	}
	if want := micrunio.GenerateStandardFIFOPath("default", req.ID, "stdout"); stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	if want := micrunio.GenerateStandardFIFOPath("default", req.ID, "stderr"); stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestFIFOPathGeneratorPreservesValidPath(t *testing.T) {
	generator := newFIFOPathGenerator("default", "container-a")
	got := generator.providedOrStandard("/provided/stdout", "stdout")

	if got != "/provided/stdout" {
		t.Fatalf("providedOrStandard = %q, want provided path", got)
	}
}
