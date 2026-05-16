package shim

import (
	"context"
	"strings"
	"testing"
)

func TestCleanupContainerRejectsTypedNilGuestControl(t *testing.T) {
	var guest *stubGuestControl

	err := cleanupContainer(context.Background(), guest, nil, "sandbox", "container", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "guest control") {
		t.Fatalf("cleanupContainer error = %v, want guest control", err)
	}
}
