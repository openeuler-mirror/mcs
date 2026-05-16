package shim

import (
	"context"
	"errors"
	"strings"
	"testing"

	pedestal "micrun/internal/adapters/hypervisor/pedestal"
	"micrun/internal/ports"
)

func TestDetectRuntimeEnvironmentFromSource(t *testing.T) {
	host := pedestal.NewPedestalFacade(stubPedestal{})
	hypervisor := stubHypervisorControl{}
	var gotHypervisor ports.HypervisorControl

	ctx := context.Background()
	bindings, err := detectRuntimeEnvironmentFrom(ctx, runtimeEnvironmentSource{
		host: func() *pedestal.PedestalFacade { return host },
		guestControl: func(h ports.HypervisorControl) ports.GuestControl {
			gotHypervisor = h
			return stubGuestControl{}
		},
		hypervisor: func(*pedestal.PedestalFacade) ports.HypervisorControl {
			return hypervisor
		},
	})
	if err != nil {
		t.Fatalf("detectRuntimeEnvironmentFrom returned unexpected error: %v", err)
	}

	if bindings.hostProfile.Type != host.Type() {
		t.Fatalf("unexpected host type: %v", bindings.hostProfile.Type)
	}
	if bindings.hostProfile.MemLowThreshold != host.MemLowThreshold() {
		t.Fatalf("unexpected low memory threshold: %d", bindings.hostProfile.MemLowThreshold)
	}
	if bindings.hostProfile.MemHighThreshold != host.MemHighThreshold(ctx) {
		t.Fatalf("unexpected host profile: %+v", bindings.hostProfile)
	}
	if bindings.vcpuStats == nil {
		t.Fatal("expected vcpu stats provider to be configured")
	}
	if bindings.maxClientCPUs == nil {
		t.Fatal("expected max client cpu provider to be configured")
	}
	if bindings.planEssentialResources == nil {
		t.Fatal("expected essential resource planner to be configured")
	}
	if gotHypervisor != hypervisor {
		t.Fatalf("guest control factory received %T, want %T", gotHypervisor, hypervisor)
	}
}

func TestDetectRuntimeEnvironmentFromHonorsCanceledContextBeforeDetection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hostCalled := false

	_, err := detectRuntimeEnvironmentFrom(ctx, runtimeEnvironmentSource{
		host: func() *pedestal.PedestalFacade {
			hostCalled = true
			return pedestal.NewPedestalFacade(stubPedestal{})
		},
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("detectRuntimeEnvironmentFrom error = %v, want context.Canceled", err)
	}
	if hostCalled {
		t.Fatal("host detection should not run after context cancellation")
	}
}

func TestRuntimeEnvironmentValidateRejectsTypedNilControls(t *testing.T) {
	bindings := testRuntimeEnvironment()
	var guest *stubGuestControl
	var hypervisor *stubHypervisorControl
	bindings.guestControl = guest
	bindings.hypervisor = hypervisor

	err := bindings.validate()
	if err == nil {
		t.Fatal("expected typed nil controls to fail validation")
	}
	if !strings.Contains(err.Error(), "guest control") || !strings.Contains(err.Error(), "hypervisor control") {
		t.Fatalf("runtime environment error = %v, want guest and hypervisor reasons", err)
	}
}

func TestRuntimeEnvironmentValidateReportsMissingProvidersTogether(t *testing.T) {
	bindings := testRuntimeEnvironment()
	bindings.vcpuStats = nil
	bindings.maxClientCPUs = nil
	bindings.planEssentialResources = nil

	err := bindings.validate()
	if err == nil {
		t.Fatal("expected missing providers to fail validation")
	}
	for _, want := range []string{"host vcpu stats provider", "max client cpu provider", "essential resource planner"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("runtime environment error = %v, want %q", err, want)
		}
	}
}
