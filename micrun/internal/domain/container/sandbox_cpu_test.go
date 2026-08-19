package container

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"micrun/internal/support/cpuset"
	er "micrun/internal/support/errors"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestCheckVCPUsPinningValidatesSandbox(t *testing.T) {
	var sandbox *Sandbox
	if err := sandbox.checkVCPUsPinning(context.Background()); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox checkVCPUsPinning error = %v, want SandboxNotFound", err)
	}

	if err := (&Sandbox{}).checkVCPUsPinning(context.Background()); err == nil || !strings.Contains(err.Error(), "sandbox config") {
		t.Fatalf("missing config checkVCPUsPinning error = %v, want sandbox config error", err)
	}
}

func TestGetSandboxCpusetStrValidatesConfigEntries(t *testing.T) {
	var sandbox *Sandbox
	if _, _, err := sandbox.getSandboxCpusetStr(); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox getSandboxCpusetStr error = %v, want SandboxNotFound", err)
	}

	sandbox = &Sandbox{
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{
				"bad": nil,
			},
		},
	}
	_, _, err := sandbox.getSandboxCpusetStr()
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("nil config getSandboxCpusetStr error = %v, want container key", err)
	}
}

func TestPinVCPUValidatesSandboxState(t *testing.T) {
	var sandbox *Sandbox
	if err := sandbox.pinVCPU(context.Background(), cpuset.NewCPUSet(0)); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox pinVCPU error = %v, want SandboxNotFound", err)
	}

	sandbox = &Sandbox{}
	if err := sandbox.pinVCPU(context.Background(), cpuset.NewCPUSet(0)); err == nil || !strings.Contains(err.Error(), "sandbox config") {
		t.Fatalf("missing config pinVCPU error = %v, want sandbox config error", err)
	}

	sandbox = &Sandbox{
		id: "sandbox-cpu",
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{},
		},
		containers: map[string]*Container{
			"bad": nil,
		},
	}
	if err := sandbox.pinVCPU(context.Background(), cpuset.NewCPUSet(0)); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil container pinVCPU error = %v, want ContainerNotFound", err)
	}
}

func TestPinVCPUInitializesResourceMaps(t *testing.T) {
	cfgA := &ContainerConfig{ID: "worker-a"}
	cfgB := &ContainerConfig{ID: "worker-b"}
	execA := &pinningGuestExecutor{}
	execB := &pinningGuestExecutor{}
	sandbox := &Sandbox{
		id: "sandbox-cpu",
		config: &SandboxConfig{
			SharedCPUPool: true,
			ContainerConfigs: map[string]*ContainerConfig{
				cfgB.ID: cfgB,
				cfgA.ID: cfgA,
			},
		},
		resManager: sandboxResource{},
	}
	sandbox.containers = map[string]*Container{
		cfgB.ID: {id: cfgB.ID, config: cfgB, guestExec: execB, sandbox: sandbox},
		cfgA.ID: {id: cfgA.ID, config: cfgA, guestExec: execA, sandbox: sandbox},
	}

	if err := sandbox.pinVCPU(context.Background(), cpuset.NewCPUSet(2, 0)); err != nil {
		t.Fatalf("pinVCPU returned error: %v", err)
	}

	wantPinned := []int{0, 2}
	if !reflect.DeepEqual(execA.pinned, wantPinned) {
		t.Fatalf("worker-a pinned CPUs = %v, want %v", execA.pinned, wantPinned)
	}
	if !reflect.DeepEqual(execB.pinned, wantPinned) {
		t.Fatalf("worker-b pinned CPUs = %v, want %v", execB.pinned, wantPinned)
	}
	if got := sandbox.resManager.ContainerCPUSet[cfgA.ID].String(); got != "0,2" {
		t.Fatalf("worker-a resource cpuset = %q, want 0,2", got)
	}
	if got := sandbox.resManager.ContainerCPUSet[cfgB.ID].String(); got != "0,2" {
		t.Fatalf("worker-b resource cpuset = %q, want 0,2", got)
	}
	if sandbox.resManager.ContainerVCPUs == nil {
		t.Fatal("pinVCPU did not initialize ContainerVCPUs map")
	}
	if sandbox.resManager.VCPUCount != 4 {
		t.Fatalf("VCPUCount = %d, want 4", sandbox.resManager.VCPUCount)
	}
}

func TestCalculateSandboxResourcesRejectNilConfigEntries(t *testing.T) {
	sandbox := &Sandbox{
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{
				"bad": nil,
			},
		},
	}

	if _, err := calculateSandboxVCPUs(context.Background(), sandbox); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("calculateSandboxVCPUs error = %v, want container key", err)
	}
	if _, err := calculateSandboxMemory(context.Background(), sandbox); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("calculateSandboxMemory error = %v, want container key", err)
	}
}

func TestCalculateSandboxResourcesReturnActiveContainerErrors(t *testing.T) {
	expected := errors.New("exists failed")
	memoryLimit := int64(64)
	cfg := &ContainerConfig{
		ID: "worker",
		Resources: &specs.LinuxResources{
			Memory: &specs.LinuxMemory{Limit: &memoryLimit},
		},
	}
	sandbox := &Sandbox{
		ctx: context.Background(),
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{cfg.ID: cfg},
		},
		containers: map[string]*Container{
			cfg.ID: {
				ctx:    context.Background(),
				id:     cfg.ID,
				config: cfg,
				state:  ContainerState{State: StateReady},
				sandbox: &Sandbox{
					guestControl: &stubGuestControl{existsErr: expected},
				},
			},
		},
	}

	if _, err := calculateSandboxVCPUs(context.Background(), sandbox); !errors.Is(err, expected) {
		t.Fatalf("calculateSandboxVCPUs error = %v, want %v", err, expected)
	}
	if _, err := calculateSandboxMemory(context.Background(), sandbox); !errors.Is(err, expected) {
		t.Fatalf("calculateSandboxMemory error = %v, want %v", err, expected)
	}
}

func TestCalculateSandboxResourcesHonorCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := &ContainerConfig{ID: "worker"}
	sandbox := &Sandbox{
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{cfg.ID: cfg},
		},
		containers: map[string]*Container{cfg.ID: {id: cfg.ID}},
	}

	if _, err := calculateSandboxVCPUs(ctx, sandbox); !errors.Is(err, context.Canceled) {
		t.Fatalf("calculateSandboxVCPUs error = %v, want context.Canceled", err)
	}
	if _, err := calculateSandboxMemory(ctx, sandbox); !errors.Is(err, context.Canceled) {
		t.Fatalf("calculateSandboxMemory error = %v, want context.Canceled", err)
	}
}

// The infra (pause) container never registers a mica client, so its control
// socket does not exist and any pin against it fails. With SharedCPUPool +
// EnableVCPUsPinning that failure poisoned the aggregated result and failed
// every CRI pod's CreateContainer on an error about the pause container.
func TestPinVCPUSkipsInfraContainer(t *testing.T) {
	infraCfg := &ContainerConfig{ID: "pause", IsInfra: true}
	workerCfg := &ContainerConfig{
		ID:        "worker",
		Resources: &specs.LinuxResources{CPU: &specs.LinuxCPU{Cpus: "0,2"}},
	}
	infraExec := &pinningGuestExecutor{err: errors.New("dial unix /run/mica/pause.sock: no such file or directory")}
	workerExec := &pinningGuestExecutor{}
	sandbox := &Sandbox{
		id: "sandbox-cpu",
		config: &SandboxConfig{
			SharedCPUPool: true,
			ContainerConfigs: map[string]*ContainerConfig{
				infraCfg.ID:  infraCfg,
				workerCfg.ID: workerCfg,
			},
		},
		resManager: sandboxResource{},
	}
	sandbox.containers = map[string]*Container{
		infraCfg.ID:  {id: infraCfg.ID, config: infraCfg, guestExec: infraExec, sandbox: sandbox, state: ContainerState{State: StateRunning}},
		workerCfg.ID: {id: workerCfg.ID, config: workerCfg, guestExec: workerExec, sandbox: sandbox, state: ContainerState{State: StateRunning}},
	}

	if err := sandbox.pinVCPU(context.Background(), cpuset.NewCPUSet(0, 2)); err != nil {
		t.Fatalf("pinVCPU returned error: %v (infra pin failure must not poison the sandbox)", err)
	}
	if len(infraExec.pinned) != 0 {
		t.Fatalf("infra container was pinned to %v, want no pin call", infraExec.pinned)
	}
	if !reflect.DeepEqual(workerExec.pinned, []int{0, 2}) {
		t.Fatalf("worker pinned CPUs = %v, want [0 2]", workerExec.pinned)
	}
}

// A stopped container's domain is destroyed, so micad's underlying
// xl vcpu-pin against it always fails. Aggregating that failure poisoned the
// sibling's Create/Start/Update: kubelet's standard container restart leaves
// the old stopped container registered until GC deletes it, so the very next
// CreateContainer in the pod failed on the dead sibling's pin error.
func TestPinVCPUSkipsStoppedSibling(t *testing.T) {
	stoppedCfg := &ContainerConfig{
		ID:        "worker-old",
		Resources: &specs.LinuxResources{CPU: &specs.LinuxCPU{Cpus: "0-1"}},
	}
	liveCfg := &ContainerConfig{
		ID:        "worker-new",
		Resources: &specs.LinuxResources{CPU: &specs.LinuxCPU{Cpus: "2-3"}},
	}
	stoppedExec := &pinningGuestExecutor{err: errors.New("mica update failed: xl vcpu-pin: domain not found")}
	liveExec := &pinningGuestExecutor{}
	sandbox := &Sandbox{
		id: "sandbox-cpu",
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{
				stoppedCfg.ID: stoppedCfg,
				liveCfg.ID:    liveCfg,
			},
		},
		resManager: sandboxResource{},
	}
	sandbox.containers = map[string]*Container{
		stoppedCfg.ID: {id: stoppedCfg.ID, config: stoppedCfg, guestExec: stoppedExec, sandbox: sandbox, state: ContainerState{State: StateStopped}},
		liveCfg.ID:    {id: liveCfg.ID, config: liveCfg, guestExec: liveExec, sandbox: sandbox, state: ContainerState{State: StateRunning}},
	}

	if err := sandbox.pinVCPU(context.Background(), cpuset.NewCPUSet()); err != nil {
		t.Fatalf("pinVCPU returned error: %v (stopped sibling's pin failure must not poison the sandbox)", err)
	}
	if len(stoppedExec.pinned) != 0 {
		t.Fatalf("stopped container was pinned to %v, want no pin call", stoppedExec.pinned)
	}
	if !reflect.DeepEqual(liveExec.pinned, []int{2, 3}) {
		t.Fatalf("live worker pinned CPUs = %v, want [2 3]", liveExec.pinned)
	}
}
