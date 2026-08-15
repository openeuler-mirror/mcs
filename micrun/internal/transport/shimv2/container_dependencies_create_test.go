package shim

import (
	"context"
	"strings"
	"testing"

	"micrun/internal/adapters/guest/libmica"
	cntr "micrun/internal/domain/container"
)

func TestMapCreateGuestRejectsOversizedCPUSet(t *testing.T) {
	// cpu_str on the micad wire is MaxCPUStringLen bytes including the NUL
	// terminator; longer inputs were silently truncated, pinning the guest to
	// fewer pCPUs than expected.
	oversized := strings.Repeat("0,", libmica.MaxCPUStringLen)
	err := mapCreateGuest(context.Background(), cntr.GuestClientConfig{CPU: oversized})
	if err == nil || !strings.Contains(err.Error(), "exceeds mica limit") {
		t.Fatalf("mapCreateGuest error = %v, want cpuset length rejection", err)
	}
}

func TestMapCreateGuestRejectsOversizedFirmwarePath(t *testing.T) {
	// path on the micad wire is MaxFirmwarePathLen bytes including the NUL
	// terminator; longer firmware paths were silently truncated, making micad
	// load a different file than intended (or fail with a confusing error).
	oversized := strings.Repeat("a", libmica.MaxFirmwarePathLen)
	err := mapCreateGuest(context.Background(), cntr.GuestClientConfig{Path: oversized})
	if err == nil || !strings.Contains(err.Error(), "firmware path length") {
		t.Fatalf("mapCreateGuest error = %v, want firmware path length rejection", err)
	}
}

func TestMapCreateGuestRejectsOversizedPedestalConfPath(t *testing.T) {
	// ped_cfg shares the MaxFirmwarePathLen wire limit with path.
	oversized := strings.Repeat("a", libmica.MaxFirmwarePathLen)
	err := mapCreateGuest(context.Background(), cntr.GuestClientConfig{PedCfg: oversized})
	if err == nil || !strings.Contains(err.Error(), "pedestal config path length") {
		t.Fatalf("mapCreateGuest error = %v, want pedestal config path length rejection", err)
	}
}
