package micad

import (
	"context"
	"errors"
	"testing"

	"micrun/internal/ports"
)

type fakeHypervisorControl struct {
	domainState    string
	domainStateErr error
	destroyErr     error
	calls          int
	destroyCalls   int
}

func (f *fakeHypervisorControl) Type() ports.HypervisorType { return ports.HypervisorXen }
func (f *fakeHypervisorControl) MaxCPUNum(context.Context) uint32 {
	return 4
}
func (f *fakeHypervisorControl) MemoryMB(context.Context) (uint32, uint32) {
	return 1024, 2048
}
func (f *fakeHypervisorControl) DomainState(context.Context, string) (string, error) {
	f.calls++
	return f.domainState, f.domainStateErr
}
func (f *fakeHypervisorControl) Destroy(context.Context, string) error {
	f.destroyCalls++
	return f.destroyErr
}
func (f *fakeHypervisorControl) Pause(context.Context, string) error  { return nil }
func (f *fakeHypervisorControl) Resume(context.Context, string) error { return nil }
func (f *fakeHypervisorControl) SetVCPUCount(context.Context, string, uint32) error {
	return nil
}
func (f *fakeHypervisorControl) SetMemory(context.Context, string, uint32) error {
	return nil
}
func (f *fakeHypervisorControl) SetMaxMemory(context.Context, string, uint32) error {
	return nil
}
func (f *fakeHypervisorControl) SetCPUWeight(context.Context, string, uint32) error {
	return nil
}
func (f *fakeHypervisorControl) SetCPUCapacity(context.Context, string, uint32) error {
	return nil
}

func TestExistsIsSocketOnlyEvenWhenDomainPresent(t *testing.T) {
	// Domain fallback on Exists broke checkState: Ready guests after micad
	// restart stayed Ready and Start failed on the missing socket.
	h := &fakeHypervisorControl{domainState: "running"}
	ctl := NewControl(h)
	exists, err := ctl.Exists(context.Background(), "orphan-after-micad-restart")
	if err != nil {
		t.Fatalf("Exists error = %v", err)
	}
	if exists {
		t.Fatal("Exists = true, want false when only the Xen domain remains")
	}
	if h.calls != 0 {
		t.Fatalf("DomainState calls = %d, want 0 (Exists must stay socket-only)", h.calls)
	}
}

func TestRemoveDestroysLingeringDomainWhenSocketMissing(t *testing.T) {
	h := &fakeHypervisorControl{domainState: "running"}
	ctl := NewControl(h)
	if err := ctl.Remove(context.Background(), "orphan-after-micad-restart"); err != nil {
		t.Fatalf("Remove error = %v", err)
	}
	if h.destroyCalls != 1 {
		t.Fatalf("Destroy calls = %d, want 1", h.destroyCalls)
	}
}

func TestStopDestroysLingeringDomainWhenSocketMissing(t *testing.T) {
	h := &fakeHypervisorControl{domainState: "running"}
	ctl := NewControl(h)
	if err := ctl.Stop(context.Background(), "orphan-after-micad-restart"); err != nil {
		t.Fatalf("Stop error = %v", err)
	}
	if h.destroyCalls != 1 {
		t.Fatalf("Destroy calls = %d, want 1", h.destroyCalls)
	}
}

func TestRemoveSkipsDestroyWhenDomainAlreadyGone(t *testing.T) {
	h := &fakeHypervisorControl{domainStateErr: errors.New("domain demo not found in xl list output")}
	ctl := NewControl(h)
	if err := ctl.Remove(context.Background(), "demo"); err != nil {
		t.Fatalf("Remove error = %v", err)
	}
	if h.destroyCalls != 0 {
		t.Fatalf("Destroy calls = %d, want 0", h.destroyCalls)
	}
}
