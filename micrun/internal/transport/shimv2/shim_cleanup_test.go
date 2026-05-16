package shim

import (
	"testing"
	"time"
)

func TestCleanupDeleteResponseUsesCleanupStatusAndTimestamp(t *testing.T) {
	exitedAt := time.Date(2026, 4, 27, 15, 16, 17, 0, time.UTC)

	resp := cleanupDeleteResponse(exitedAt)

	if resp.ExitStatus != cleanupExitStatus() {
		t.Fatalf("cleanup exit status = %d, want %d", resp.ExitStatus, cleanupExitStatus())
	}
	if resp.Pid != 0 {
		t.Fatalf("cleanup response pid = %d, want 0", resp.Pid)
	}
	if !resp.ExitedAt.AsTime().Equal(exitedAt) {
		t.Fatalf("cleanup exitedAt = %s, want %s", resp.ExitedAt.AsTime(), exitedAt)
	}
}
