package shim

import (
	"context"

	oci "micrun/internal/adapters/config/oci"
	guestmicad "micrun/internal/adapters/guest/micad"
	pedestal "micrun/internal/adapters/hypervisor/pedestal"
	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	defs "micrun/internal/support/definitions"
	"micrun/internal/support/validation"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type runtimeEnvironment struct {
	hostProfile            oci.HostProfile
	vcpuStats              func(ctx context.Context) (*pedestal.XlVcpuInfo, error)
	maxClientCPUs          func(ctx context.Context, exclusiveDom0CPU bool) int
	planEssentialResources func(*specs.Spec) *pedestal.EssentialResource
	guestControl           ports.GuestControl
	hypervisor             ports.HypervisorControl
}

type runtimeEnvironmentSource struct {
	host         func() *pedestal.PedestalFacade
	guestControl func(ports.HypervisorControl) ports.GuestControl
	hypervisor   func(*pedestal.PedestalFacade) ports.HypervisorControl
}

func detectRuntimeEnvironment(ctx context.Context) (runtimeEnvironment, error) {
	return detectRuntimeEnvironmentFrom(ctx, defaultRuntimeEnvironmentSource())
}

func defaultRuntimeEnvironmentSource() runtimeEnvironmentSource {
	return runtimeEnvironmentSource{
		host: func() *pedestal.PedestalFacade {
			return pedestal.DetectHost()
		},
		guestControl: func(h ports.HypervisorControl) ports.GuestControl {
			return guestmicad.NewControl(h)
		},
		hypervisor: func(host *pedestal.PedestalFacade) ports.HypervisorControl {
			return pedestal.NewControl(host)
		},
	}
}

func detectRuntimeEnvironmentFrom(ctx context.Context, source runtimeEnvironmentSource) (runtimeEnvironment, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return runtimeEnvironment{}, err
	}
	var host *pedestal.PedestalFacade
	if source.host != nil {
		host = source.host()
	}
	var hypervisor ports.HypervisorControl
	if source.hypervisor != nil {
		hypervisor = source.hypervisor(host)
	}
	bindings := runtimeEnvironment{
		hostProfile: oci.HostProfileFromFacade(ctx, host),
		hypervisor:  hypervisor,
	}
	if source.guestControl != nil {
		bindings.guestControl = source.guestControl(bindings.hypervisor)
	}
	if host != nil {
		bindings.vcpuStats = host.VCPUList
		bindings.maxClientCPUs = func(ctx context.Context, exclusiveDom0CPU bool) int {
			if defs.IsMock {
				return 1
			}
			return int(host.ClientCPUCapacity(ctx, exclusiveDom0CPU))
		}
		bindings.planEssentialResources = host.PlanEssentialResources
	}

	return bindings, bindings.validate()
}

func (b runtimeEnvironment) validate() error {
	return validation.RequireAll("runtime environment is incomplete",
		validation.Required("host vcpu stats provider", b.vcpuStats),
		validation.Required("max client cpu provider", b.maxClientCPUs),
		validation.Required("essential resource planner", b.planEssentialResources),
		validation.Required("guest control", b.guestControl),
		validation.Required("hypervisor control", b.hypervisor),
	)
}
