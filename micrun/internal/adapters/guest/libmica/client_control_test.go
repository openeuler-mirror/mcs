package libmica

import (
	"context"
	"errors"
	"strings"
	"testing"

	pedestal "micrun/internal/adapters/hypervisor/pedestal"
	"micrun/internal/ports"
)

type fakeHypervisor struct {
	kind           ports.HypervisorType
	pauseErr       error
	resumeErr      error
	pauseCalls     int
	resumeCalls    int
	pauseCtx       context.Context
	resumeCtx      context.Context
	setMemoryCtx   context.Context
	setMemoryID    string
	setMemoryMB    uint32
	setMemoryCalls int
	setVCPUCtx     context.Context
	setVCPUID      string
	setVCPUCount   uint32
	setVCPUCalls   int
}

func (f *fakeHypervisor) Type() ports.HypervisorType {
	if f.kind == "" {
		return ports.HypervisorBaremetal
	}
	return f.kind
}

func (f *fakeHypervisor) MaxCPUNum(context.Context) uint32 { return 8 }
func (f *fakeHypervisor) SetMemory(ctx context.Context, id string, memMB uint32) error {
	f.setMemoryCtx = ctx
	f.setMemoryCalls++
	f.setMemoryID = id
	f.setMemoryMB = memMB
	return nil
}
func (f *fakeHypervisor) SetMaxMemory(context.Context, string, uint32) error { return nil }
func (f *fakeHypervisor) SetCPUWeight(context.Context, string, uint32) error { return nil }
func (f *fakeHypervisor) SetCPUCapacity(context.Context, string, uint32) error {
	return nil
}
func (f *fakeHypervisor) SetVCPUCount(ctx context.Context, id string, count uint32) error {
	f.setVCPUCtx = ctx
	f.setVCPUID = id
	f.setVCPUCount = count
	f.setVCPUCalls++
	return nil
}
func (f *fakeHypervisor) Pause(ctx context.Context, _ string) error {
	f.pauseCtx = ctx
	f.pauseCalls++
	return f.pauseErr
}
func (f *fakeHypervisor) Resume(ctx context.Context, _ string) error {
	f.resumeCtx = ctx
	f.resumeCalls++
	return f.resumeErr
}

func TestPauseWithHypervisorUsesInjectedControl(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })

	micadCalled := false
	micaCtlFn = func(context.Context, MicaCommand, string, ...string) error {
		micadCalled = true
		return nil
	}

	h := &fakeHypervisor{}
	if err := PauseWithHypervisor("demo", h); err != nil {
		t.Fatalf("PauseWithHypervisor returned error: %v", err)
	}
	if h.pauseCalls != 1 {
		t.Fatalf("pauseCalls = %d, want 1", h.pauseCalls)
	}
	if micadCalled {
		t.Fatal("expected micad fallback not to be used when hypervisor pause succeeds")
	}
}

func TestPauseWithHypervisorFallsBackWhenUnsupported(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })

	var gotCmd MicaCommand
	micaCtlFn = func(_ context.Context, cmd MicaCommand, id string, opts ...string) error {
		gotCmd = cmd
		return nil
	}

	h := &fakeHypervisor{pauseErr: pedestal.ErrNotSupported}
	if err := PauseWithHypervisor("demo", h); err != nil {
		t.Fatalf("PauseWithHypervisor returned error: %v", err)
	}
	if gotCmd != MPause {
		t.Fatalf("fallback command = %s, want %s", gotCmd, MPause)
	}
}

func TestPauseWithHypervisorPropagatesControlError(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })

	micaCtlFn = func(context.Context, MicaCommand, string, ...string) error {
		t.Fatal("micad fallback should not run for non-unsupported hypervisor errors")
		return nil
	}

	expected := errors.New("pause failed")
	h := &fakeHypervisor{pauseErr: expected}
	if err := PauseWithHypervisor("demo", h); !errors.Is(err, expected) {
		t.Fatalf("PauseWithHypervisor error = %v, want %v", err, expected)
	}
}

func TestPauseResumeWithHypervisorContextPropagatesContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	h := &fakeHypervisor{}

	if err := PauseWithHypervisorContext(ctx, "demo", h); err != nil {
		t.Fatalf("PauseWithHypervisorContext returned error: %v", err)
	}
	if h.pauseCtx != ctx {
		t.Fatal("pause did not receive caller context")
	}
	if err := ResumeWithHypervisorContext(ctx, "demo", h); err != nil {
		t.Fatalf("ResumeWithHypervisorContext returned error: %v", err)
	}
	if h.resumeCtx != ctx {
		t.Fatal("resume did not receive caller context")
	}
}

func TestPauseResumeWithHypervisorContextHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &fakeHypervisor{}

	if err := PauseWithHypervisorContext(ctx, "demo", h); !errors.Is(err, context.Canceled) {
		t.Fatalf("PauseWithHypervisorContext canceled error = %v, want context.Canceled", err)
	}
	if h.pauseCalls != 0 {
		t.Fatalf("pauseCalls = %d, want 0", h.pauseCalls)
	}
	if err := ResumeWithHypervisorContext(ctx, "demo", h); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResumeWithHypervisorContext canceled error = %v, want context.Canceled", err)
	}
	if h.resumeCalls != 0 {
		t.Fatalf("resumeCalls = %d, want 0", h.resumeCalls)
	}
}

func TestStopRemoveContextReturnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := StopContext(ctx, "demo"); !errors.Is(err, context.Canceled) {
		t.Fatalf("StopContext canceled error = %v, want context.Canceled", err)
	}
	if err := RemoveContext(ctx, "demo"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveContext canceled error = %v, want context.Canceled", err)
	}
}

func TestCreateContextReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := CreateContext(ctx, MicaClientConf{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateContext canceled error = %v, want context.Canceled", err)
	}
}

func TestMicaCtlImplReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := micaCtlImpl(ctx, MStart, "demo")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("micaCtlImpl canceled error = %v, want context.Canceled", err)
	}
}

func TestMicaCommandMessage(t *testing.T) {
	tests := []struct {
		name string
		cmd  MicaCommand
		opts []string
		want string
	}{
		{name: "start", cmd: MStart, want: string(MStart)},
		{name: "pause uses stop wire command", cmd: MPause, want: string(MStop)},
		{name: "resume uses start wire command", cmd: MResume, want: string(MStart)},
		{name: "update appends resource payload", cmd: MUpdate, opts: []string{"Memory", "64"}, want: string(MUpdate) + " Memory 64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := micaCommandMessage(tt.cmd, tt.opts)
			if err != nil {
				t.Fatalf("micaCommandMessage returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("micaCommandMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMicaCommandMessageReturnsUpdateError(t *testing.T) {
	if _, err := micaCommandMessage(MUpdate, nil); err == nil {
		t.Fatal("expected update command without payload to fail")
	}
}

func TestBuildUpdateWireFormat(t *testing.T) {
	tests := []struct {
		name    string
		opts    []string
		want    string
		wantErr bool
	}{
		{name: "combined", opts: []string{"Memory 64"}, want: "Memory 64"},
		{name: "split", opts: []string{"CPU", "0-3"}, want: "CPU 0-3"},
		{name: "missing value", opts: []string{"Memory"}, wantErr: true},
		{name: "unsupported field", opts: []string{"Bogus", "1"}, wantErr: true},
		{name: "empty", opts: nil, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildUpdateWireFormat(tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildUpdateWireFormat returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("buildUpdateWireFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMicaUpdateRequestWireFormatUsesProtocolFields(t *testing.T) {
	tests := []struct {
		name  string
		field MicaUpdateField
		value string
		want  string
	}{
		{name: "vcpu", field: MicaUpdateVCPU, value: "2", want: "VCPU 2"},
		{name: "pcpu constraints", field: MicaUpdatePCPUConstraints, value: "0-3", want: "CPU 0-3"},
		{name: "cpu capacity protocol spelling", field: MicaUpdateCPUCapacity, value: "75", want: "CPUCpacity 75"},
		{name: "cpu weight", field: MicaUpdateCPUWeight, value: "512", want: "CPUWeight 512"},
		{name: "memory max", field: MicaUpdateMemoryMax, value: "128", want: "MaxMem 128"},
		{name: "memory current", field: MicaUpdateMemoryCurrent, value: "64", want: "Memory 64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MicaUpdateRequest{Field: tt.field, Value: tt.value}.WireFormat()
			if got != tt.want {
				t.Fatalf("WireFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestXLUpdateWorkaroundEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty disabled", value: "", want: false},
		{name: "zero disabled", value: "0", want: false},
		{name: "false disabled", value: "false", want: false},
		{name: "no disabled", value: "no", want: false},
		{name: "one enabled", value: "1", want: true},
		{name: "yes enabled", value: "yes", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(xlUpdateWorkaroundEnv, tt.value)
			if got := xlUpdateWorkaroundEnabled(); got != tt.want {
				t.Fatalf("xlUpdateWorkaroundEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMicaExecutorUpdateMemoryUsesInjectedHypervisorForXLWorkaround(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })
	t.Setenv(xlUpdateWorkaroundEnv, "1")

	micaCtlFn = func(context.Context, MicaCommand, string, ...string) error {
		t.Fatal("micad command should not run when xl workaround succeeds")
		return nil
	}

	h := &fakeHypervisor{kind: ports.HypervisorXen}
	exec := &MicaExecutor{ID: "demo", Hypervisor: h}
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	if err := exec.UpdateMemory(ctx, 64); err != nil {
		t.Fatalf("UpdateMemory returned error: %v", err)
	}
	if h.setMemoryCalls != 1 {
		t.Fatalf("setMemoryCalls = %d, want 1", h.setMemoryCalls)
	}
	if h.setMemoryID != "demo" || h.setMemoryMB != 64 {
		t.Fatalf("SetMemory called with (%q, %d), want (demo, 64)", h.setMemoryID, h.setMemoryMB)
	}
	if h.setMemoryCtx != ctx {
		t.Fatal("SetMemory context was not propagated")
	}
	if exec.CurrentMaxMem() != 64 {
		t.Fatalf("CurrentMaxMem = %d, want 64", exec.CurrentMaxMem())
	}
}

func TestMicaExecutorUpdateMemoryHonorsCanceledContextBeforeXLWorkaround(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })
	t.Setenv(xlUpdateWorkaroundEnv, "1")

	micaCtlFn = func(context.Context, MicaCommand, string, ...string) error {
		t.Fatal("micad fallback should not run after context cancellation")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &fakeHypervisor{kind: ports.HypervisorXen}
	exec := &MicaExecutor{ID: "demo", Hypervisor: h}
	if err := exec.UpdateMemory(ctx, 64); !errors.Is(err, context.Canceled) {
		t.Fatalf("UpdateMemory canceled error = %v, want context.Canceled", err)
	}
	if h.setMemoryCalls != 0 {
		t.Fatalf("setMemoryCalls = %d, want 0", h.setMemoryCalls)
	}
	if exec.CurrentMaxMem() != 0 {
		t.Fatalf("CurrentMaxMem = %d, want 0", exec.CurrentMaxMem())
	}
}

func TestMicaExecutorUpdateVCPUNumRecordsSuccessfulUpdate(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })
	t.Setenv(xlUpdateWorkaroundEnv, "0")
	micaCtlFn = func(context.Context, MicaCommand, string, ...string) error {
		return nil
	}

	exec := &MicaExecutor{
		ID:         "demo",
		records:    MicaClientConf{vcpuNum: 1},
		Hypervisor: &fakeHypervisor{},
	}

	oldCPUs, newCPUs, err := exec.UpdateVCPUNum(context.Background(), 3)
	if err != nil {
		t.Fatalf("UpdateVCPUNum returned error: %v", err)
	}
	if oldCPUs != 1 || newCPUs != 3 {
		t.Fatalf("UpdateVCPUNum returned old=%d new=%d, want old=1 new=3", oldCPUs, newCPUs)
	}
	if exec.records.vcpuNum != 3 {
		t.Fatalf("records.vcpuNum = %d, want 3", exec.records.vcpuNum)
	}
	if exec.NeedUpdateVCPUs(context.Background(), 3) {
		t.Fatal("NeedUpdateVCPUs should be false after successful update")
	}
}

func TestMicaExecutorUpdateVCPUUsesCallerContextForXLWorkaround(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })
	t.Setenv(xlUpdateWorkaroundEnv, "1")

	micaCtlFn = func(context.Context, MicaCommand, string, ...string) error {
		t.Fatal("micad command should not run when xl workaround succeeds")
		return nil
	}

	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	h := &fakeHypervisor{kind: ports.HypervisorXen}
	exec := &MicaExecutor{ID: "demo", Hypervisor: h}
	if _, _, err := exec.UpdateVCPUNum(ctx, 3); err != nil {
		t.Fatalf("UpdateVCPUNum returned error: %v", err)
	}
	if h.setVCPUCalls != 1 || h.setVCPUID != "demo" || h.setVCPUCount != 3 {
		t.Fatalf("SetVCPUCount call = (%d, %q, %d), want (1, demo, 3)", h.setVCPUCalls, h.setVCPUID, h.setVCPUCount)
	}
	if h.setVCPUCtx != ctx {
		t.Fatalf("SetVCPUCount context was not propagated")
	}
}

func TestHandleMicaUpdateWithXlRejectsInvalidUnsignedValues(t *testing.T) {
	t.Setenv(xlUpdateWorkaroundEnv, "1")

	for _, value := range []string{"-1", "4294967296"} {
		t.Run(value, func(t *testing.T) {
			h := &fakeHypervisor{kind: ports.HypervisorXen}
			err := handleMicaUpdateWithXl(context.Background(), h, "demo", string(MicaUpdateVCPU), value)
			if err == nil {
				t.Fatal("expected invalid VCPU value error")
			}
			if !strings.Contains(err.Error(), "invalid VCPU count value") {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.setVCPUCalls != 0 {
				t.Fatalf("setVCPUCalls = %d, want 0", h.setVCPUCalls)
			}
		})
	}
}

func TestMicaExecutorUpdateCPUSetFallsBackToMicadWhenXLUnsupported(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })
	t.Setenv(xlUpdateWorkaroundEnv, "1")

	var gotCmd MicaCommand
	var gotID string
	var gotOpts []string
	micaCtlFn = func(_ context.Context, cmd MicaCommand, id string, opts ...string) error {
		gotCmd = cmd
		gotID = id
		gotOpts = append([]string(nil), opts...)
		return nil
	}

	h := &fakeHypervisor{kind: ports.HypervisorXen}
	exec := &MicaExecutor{ID: "demo", Hypervisor: h}
	if err := exec.UpdatePCPUConstraints(context.Background(), "0-3"); err != nil {
		t.Fatalf("UpdatePCPUConstraints returned error: %v", err)
	}
	if gotCmd != MUpdate || gotID != "demo" || len(gotOpts) != 1 || gotOpts[0] != "CPU 0-3" {
		t.Fatalf("micad fallback = (%s, %q, %v), want (set, demo, [CPU 0-3])", gotCmd, gotID, gotOpts)
	}
}

func TestMicaExecutorVCPUPinFormatsCPUSet(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() { micaCtlFn = original })

	var gotCmd MicaCommand
	var gotID string
	var gotOpts []string
	micaCtlFn = func(_ context.Context, cmd MicaCommand, id string, opts ...string) error {
		gotCmd = cmd
		gotID = id
		gotOpts = append([]string(nil), opts...)
		return nil
	}

	exec := &MicaExecutor{ID: "demo"}
	if err := exec.VCPUPin(context.Background(), []int{3, 1, 2, 1, -1}); err != nil {
		t.Fatalf("VCPUPin returned error: %v", err)
	}
	if gotCmd != MUpdate || gotID != "demo" || len(gotOpts) != 1 || gotOpts[0] != "CPU 1-3" {
		t.Fatalf("VCPUPin update = (%s, %q, %v), want (set, demo, [CPU 1-3])", gotCmd, gotID, gotOpts)
	}
	if got := exec.ReadResource().ClientCPUSet; got != "1-3" {
		t.Fatalf("ClientCPUSet = %q, want 1-3", got)
	}
}

func TestMicaExecutorVCPUPinRejectsEmptyCPUSet(t *testing.T) {
	exec := &MicaExecutor{ID: "demo"}
	if err := exec.VCPUPin(context.Background(), []int{-2, -1}); err == nil {
		t.Fatal("VCPUPin expected error for empty CPU set")
	}
}
