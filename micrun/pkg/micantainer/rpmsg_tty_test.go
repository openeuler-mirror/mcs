package micantainer

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSanitizeRPMSGClientName(t *testing.T) {
	got := sanitizeName("abc-DEF_012:/weird")
	if got != "abc-DEF_012__weird" {
		t.Fatalf("unexpected sanitized name: %q", got)
	}
}

func TestCandidateTTYs(t *testing.T) {
	paths := candiateTTYs("id:with/weird")
	if len(paths) != 4 {
		t.Fatalf("unexpected number of paths: %d", len(paths))
	}
	if paths[0] != "/dev/ttyRPMSG_id_with_weird_0" {
		t.Fatalf("unexpected first path: %q", paths[0])
	}
}

func TestIsRetryableRPMSGOpenError(t *testing.T) {
	if !retryableOpenError(os.ErrNotExist) {
		t.Fatalf("expected not-exist to be retryable")
	}
	if !retryableOpenError(unix.ENXIO) {
		t.Fatalf("expected ENXIO to be retryable")
	}
	if retryableOpenError(errors.New("boom")) {
		t.Fatalf("unexpected retryable error")
	}
}
