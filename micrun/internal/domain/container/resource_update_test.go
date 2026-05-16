package container

import (
	"context"
	"errors"
	"strings"
	"testing"

	"micrun/internal/ports"
)

type failingVCPUExecutor struct {
	recordingGuestExecutor
	err error
}

func (f failingVCPUExecutor) ReadResource() *ports.ResourceSnapshot {
	vcpu := uint32(1)
	return &ports.ResourceSnapshot{VCPU: &vcpu}
}

func (f failingVCPUExecutor) NeedUpdateVCPUs(context.Context, uint32) bool {
	return true
}

func (f failingVCPUExecutor) UpdateVCPUNum(context.Context, uint32) (uint32, uint32, error) {
	return 1, 1, f.err
}

func TestUpdateContainerResourceReturnsVCPUUpdateError(t *testing.T) {
	expectedErr := errors.New("vcpu update failed")
	err := updateContainerResource(context.Background(), &Container{
		id:        "container-vcpu",
		guestExec: failingVCPUExecutor{err: expectedErr},
	}, &ResourceChanges{VCPU: copyUint32(2)})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("updateContainerResource error = %v, want %v", err, expectedErr)
	}
}

func TestUpdateContainerResourceValidatesInputs(t *testing.T) {
	if err := updateContainerResource(context.Background(), nil, NewResourceChanges()); err == nil || !strings.Contains(err.Error(), "container reference") {
		t.Fatalf("nil container updateContainerResource error = %v, want container reference error", err)
	}

	if err := updateContainerResource(context.Background(), &Container{id: "container-noop"}, nil); err != nil {
		t.Fatalf("nil changes updateContainerResource error = %v, want nil", err)
	}

	err := updateContainerResource(context.Background(), &Container{id: "container-missing-exec"}, &ResourceChanges{CPUCapacity: copyUint32(100)})
	if err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("missing executor updateContainerResource error = %v, want guest executor error", err)
	}
}

type nilSnapshotExecutor struct {
	recordingGuestExecutor
}

func (nilSnapshotExecutor) ReadResource() *ports.ResourceSnapshot {
	return nil
}

func TestUpdateContainerResourceAllowsNilSnapshot(t *testing.T) {
	err := updateContainerResource(context.Background(), &Container{
		id:        "container-nil-snapshot",
		guestExec: nilSnapshotExecutor{},
	}, &ResourceChanges{})

	if err != nil {
		t.Fatalf("updateContainerResource with nil snapshot error = %v, want nil", err)
	}
}

type trackingResourceExecutor struct {
	recordingGuestExecutor
	readCalled bool
}

func (t *trackingResourceExecutor) ReadResource() *ports.ResourceSnapshot {
	t.readCalled = true
	return &ports.ResourceSnapshot{}
}

func TestUpdateContainerResourceHonorsCanceledContextBeforeReadingResources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	exec := &trackingResourceExecutor{}

	err := updateContainerResource(ctx, &Container{
		id:        "container-canceled",
		guestExec: exec,
	}, &ResourceChanges{CPUCapacity: copyUint32(100)})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("updateContainerResource error = %v, want context.Canceled", err)
	}
	if exec.readCalled {
		t.Fatal("ReadResource should not run after context cancellation")
	}
}

func TestResourceUpdateHelpersValidateExecutorOnlyWhenNeeded(t *testing.T) {
	if err := updateCPUCapacity(context.Background(), nil, "container1", nil); err != nil {
		t.Fatalf("nil CPU update error = %v, want nil", err)
	}
	if err := updateCPUCapacity(context.Background(), nil, "container1", &ResourceChanges{CPUCapacity: copyUint32(100)}); err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("CPU update missing executor error = %v, want guest executor error", err)
	}
	if err := updateCPUSet(context.Background(), nil, "0", "0"); err != nil {
		t.Fatalf("same cpuset update error = %v, want nil", err)
	}
	if err := updateCPUSet(context.Background(), nil, "0", "1"); err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("cpuset update missing executor error = %v, want guest executor error", err)
	}
	if err := updateVCPUCount(context.Background(), nil, "container1", nil); err != nil {
		t.Fatalf("nil vcpu update error = %v, want nil", err)
	}
	if err := updateVCPUCount(context.Background(), nil, "container1", &ResourceChanges{VCPU: copyUint32(2)}); err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("vcpu update missing executor error = %v, want guest executor error", err)
	}
}

type trackingVCPUExecutor struct {
	recordingGuestExecutor
	updatedTo uint32
	calls     int
}

func (t *trackingVCPUExecutor) NeedUpdateVCPUs(context.Context, uint32) bool {
	return true
}

func (t *trackingVCPUExecutor) UpdateVCPUNum(_ context.Context, vcpu uint32) (uint32, uint32, error) {
	t.updatedTo = vcpu
	t.calls++
	return 0, vcpu, nil
}

func TestUpdateVCPUCountDoesNotRequireOldSnapshotVCPU(t *testing.T) {
	exec := &trackingVCPUExecutor{}

	if err := updateVCPUCount(context.Background(), exec, "container1", &ResourceChanges{VCPU: copyUint32(3)}); err != nil {
		t.Fatalf("updateVCPUCount returned error: %v", err)
	}
	if exec.calls != 1 || exec.updatedTo != 3 {
		t.Fatalf("UpdateVCPUNum calls = %d, value = %d, want (1, 3)", exec.calls, exec.updatedTo)
	}
}

func TestApplyResourceUpdatePlanStopsOnFirstError(t *testing.T) {
	expectedErr := errors.New("stop here")
	var ran []string

	err := applyResourceUpdatePlan(context.Background(), []resourceUpdateStep{
		{name: "first", run: func() error {
			ran = append(ran, "first")
			return nil
		}},
		{name: "second", run: func() error {
			ran = append(ran, "second")
			return expectedErr
		}},
		{name: "third", run: func() error {
			ran = append(ran, "third")
			return nil
		}},
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("applyResourceUpdatePlan error = %v, want %v", err, expectedErr)
	}
	if got := strings.Join(ran, ","); got != "first,second" {
		t.Fatalf("ran steps = %s, want first,second", got)
	}
}

func TestApplyResourceUpdatePlanStopsAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var ran []string

	err := applyResourceUpdatePlan(ctx, []resourceUpdateStep{
		{name: "first", run: func() error {
			ran = append(ran, "first")
			cancel()
			return nil
		}},
		{name: "second", run: func() error {
			ran = append(ran, "second")
			return nil
		}},
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("applyResourceUpdatePlan error = %v, want context.Canceled", err)
	}
	if got := strings.Join(ran, ","); got != "first" {
		t.Fatalf("ran steps = %s, want first", got)
	}
}
