package shim

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	oci "micrun/internal/adapters/config/oci"
	apprecovery "micrun/internal/application/recovery"
	apptask "micrun/internal/application/task"
	cntr "micrun/internal/domain/container"

	"github.com/containerd/containerd/namespaces"
)

type fixedProcessIDProvider uint32

func (p fixedProcessIDProvider) PID() uint32 {
	return uint32(p)
}

func TestIsOneShotAction(t *testing.T) {
	tests := map[string]bool{
		"start":  true,
		"delete": true,
		"serve":  false,
		"":       false,
	}

	for action, want := range tests {
		if got := isOneShotAction(action); got != want {
			t.Fatalf("isOneShotAction(%q) = %v, want %v", action, got, want)
		}
	}
}

func TestNewOneShotShimServiceCreatesMinimalService(t *testing.T) {
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)

	service := newOneShotShimService("demo", buildRuntimeDependencies(bindings, containerDeps), "start")
	if service.id != "demo" {
		t.Fatalf("unexpected id %q", service.id)
	}
	if service.tm != nil {
		t.Fatal("one-shot service should not allocate task manager")
	}
	if service.containers != nil {
		t.Fatal("one-shot service should not allocate containers map")
	}
	if service.runtimeDeps.containerDeps != containerDeps {
		t.Fatal("one-shot service should retain runtime dependencies")
	}
	if service.processID == nil {
		t.Fatal("one-shot service should keep a process id provider")
	}
}

func TestNewDaemonShimServiceInitializesRuntimeState(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "ns-test")
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)

	services := testRuntimeServices()
	service, err := newDaemonShimService(ctx, "demo", func() {}, services.Task(), buildRuntimeDependencies(bindings, containerDeps))
	if err != nil {
		t.Fatalf("newDaemonShimService returned unexpected error: %v", err)
	}

	if service.id != "demo" {
		t.Fatalf("unexpected id %q", service.id)
	}
	if service.namespace != "ns-test" {
		t.Fatalf("unexpected namespace %q", service.namespace)
	}
	if service.ctx != ctx {
		t.Fatal("daemon service should keep the provided context")
	}
	if service.containers == nil {
		t.Fatal("daemon service should allocate containers map")
	}
	if service.events == nil || cap(service.events) != channelSize {
		t.Fatalf("unexpected events channel capacity: %d", cap(service.events))
	}
	if service.ec == nil || cap(service.ec) != channelSize {
		t.Fatalf("unexpected exit channel capacity: %d", cap(service.ec))
	}
	if service.tm == nil {
		t.Fatal("daemon service should allocate task manager")
	}
	if service.runtimeDeps.containerDeps != containerDeps {
		t.Fatal("daemon service should retain runtime dependencies")
	}
}

func TestNewDaemonShimServiceUsesInjectedProcessID(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "ns-test")
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)
	deps := buildRuntimeDependencies(bindings, containerDeps)
	deps.processID = fixedProcessIDProvider(5555)

	services := testRuntimeServices()
	service, err := newDaemonShimService(ctx, "demo", func() {}, services.Task(), deps)
	if err != nil {
		t.Fatalf("newDaemonShimService returned unexpected error: %v", err)
	}
	if service.ShimPID() != 5555 {
		t.Fatalf("shim PID = %d, want injected process ID", service.ShimPID())
	}
}

func TestNewDaemonShimServiceRequiresServices(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "ns-test")
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)

	if _, err := newDaemonShimService(ctx, "demo", func() {}, nil, runtimeDependencies{
		guestControl:   bindings.guestControl,
		hypervisor:     bindings.hypervisor,
		resourcePolicy: cntr.ResourcePolicyFromDependencies(containerDeps),
	}); err == nil {
		t.Fatal("expected error when task service is missing")
	} else if !errors.Is(err, errTaskServiceRequired) {
		t.Fatalf("newDaemonShimService error = %v, want task service required sentinel", err)
	}
}

func TestNewDaemonShimServiceRejectsTypedNilTaskService(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "ns-test")
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)
	var task *apptask.Service

	if _, err := newDaemonShimService(ctx, "demo", func() {}, task, buildRuntimeDependencies(bindings, containerDeps)); err == nil {
		t.Fatal("expected error when task service is typed nil")
	} else if !errors.Is(err, errTaskServiceRequired) {
		t.Fatalf("newDaemonShimService error = %v, want task service required sentinel", err)
	}
}

func TestNewTaskManagerRequiresDependencies(t *testing.T) {
	services := testRuntimeServices()
	if manager, err := newTaskManager(taskManagerDeps{}, services.Task()); err == nil {
		t.Fatal("expected error when task manager dependencies are missing")
	} else if manager != nil {
		t.Fatal("expected nil task manager when dependencies are missing")
	} else if !errors.Is(err, errTaskManagerDependenciesRequired) {
		t.Fatalf("newTaskManager error = %v, want task manager dependencies sentinel", err)
	}
}

func TestNewTaskManagerRequiresTaskService(t *testing.T) {
	service := newTaskRPCShimService()
	if manager, err := newTaskManager(taskManagerDepsFromShimService(service), nil); err == nil {
		t.Fatal("expected error when task service is missing")
	} else if manager != nil {
		t.Fatal("expected nil task manager when task service is missing")
	} else if !errors.Is(err, errTaskServiceRequired) {
		t.Fatalf("newTaskManager error = %v, want task service required sentinel", err)
	}
}

func TestNewDaemonShimServiceRequiresNamespace(t *testing.T) {
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)

	services := testRuntimeServices()
	_, err := newDaemonShimService(context.Background(), "demo", func() {}, services.Task(), buildRuntimeDependencies(bindings, containerDeps))

	if err == nil {
		t.Fatal("expected error when namespace is missing")
	}
}

func TestNewDaemonShimServiceRejectsPaddedNamespace(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), " ns-test")
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)

	services := testRuntimeServices()
	_, err := newDaemonShimService(ctx, "demo", func() {}, services.Task(), buildRuntimeDependencies(bindings, containerDeps))

	if err == nil {
		t.Fatal("expected error when namespace contains surrounding whitespace")
	}
}

func TestNewDaemonShimServiceRequiresDependencies(t *testing.T) {
	ctx := namespaces.WithNamespace(context.Background(), "ns-test")
	services := testRuntimeServices()
	if _, err := newDaemonShimService(ctx, "demo", func() {}, services.Task(), runtimeDependencies{}); err == nil {
		t.Fatal("expected error when runtime dependencies are incomplete")
	}
}

func TestNewDaemonShimServiceHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)

	services := testRuntimeServices()
	_, err := newDaemonShimService(ctx, "demo", func() {}, services.Task(), buildRuntimeDependencies(bindings, containerDeps))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("newDaemonShimService error = %v, want context.Canceled", err)
	}
}

func TestInitializeDaemonModeHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := initializeDaemonMode(ctx, &shimService{}, nil, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("initializeDaemonMode error = %v, want context.Canceled", err)
	}
}

func TestInitializeDaemonModeRejectsTypedNilRecoveryService(t *testing.T) {
	var recovery *apprecovery.Service

	err := initializeDaemonMode(context.Background(), &shimService{}, nil, recovery)

	if !errors.Is(err, errRecoveryServiceRequired) {
		t.Fatalf("initializeDaemonMode error = %v, want recovery service required", err)
	}
}

func TestShimServiceRecoveryBackendUsesRuntimeDependencies(t *testing.T) {
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)
	service := newBaseShimService("demo", buildRuntimeDependencies(bindings, containerDeps))

	backend := service.recoveryBackend()
	if backend.guestControl == nil {
		t.Fatal("expected recovery backend to use guest control dependency")
	}
	if backend.containerDeps != containerDeps {
		t.Fatal("expected recovery backend to use container dependencies")
	}
}

func TestShimServiceRecoveryBackendUsesConfiguredStateDir(t *testing.T) {
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)
	service := newBaseShimService("demo", buildRuntimeDependencies(bindings, containerDeps))
	service.config = oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	service.config.StateDir = "/custom/micrun"

	backend := service.recoveryBackend()
	want := filepath.Join("/custom/micrun", "containers")
	if backend.containersDir != want {
		t.Fatalf("recovery containers dir = %q, want %q", backend.containersDir, want)
	}
}

func TestRuntimeDependenciesRequireContainerDependencies(t *testing.T) {
	bindings := testRuntimeEnvironment()
	deps := buildRuntimeDependencies(bindings, nil)

	if err := deps.validate(); err == nil {
		t.Fatal("expected error when container dependencies are missing")
	}
}

func TestRuntimeDependenciesValidateRejectsTypedNilControls(t *testing.T) {
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)
	deps := buildRuntimeDependencies(bindings, containerDeps)
	var guest *stubGuestControl
	var hypervisor *stubHypervisorControl
	deps.guestControl = guest
	deps.hypervisor = hypervisor

	err := deps.validate()
	if err == nil {
		t.Fatal("expected typed nil runtime dependencies to fail validation")
	}
	if !strings.Contains(err.Error(), "guest control") || !strings.Contains(err.Error(), "hypervisor control") {
		t.Fatalf("runtime dependencies error = %v, want guest and hypervisor reasons", err)
	}
}

func TestRuntimeDependenciesValidateWrapsContainerDependencyErrors(t *testing.T) {
	bindings := testRuntimeEnvironment()
	containerDeps := buildContainerDependencies(bindings)
	containerDeps.CreateGuest = nil
	deps := buildRuntimeDependencies(bindings, containerDeps)

	err := deps.validate()
	if err == nil {
		t.Fatal("expected invalid container dependencies to fail validation")
	}
	if !strings.Contains(err.Error(), "container dependencies are invalid") || !strings.Contains(err.Error(), "CreateGuest") {
		t.Fatalf("runtime dependencies error = %v, want wrapped CreateGuest reason", err)
	}
}
