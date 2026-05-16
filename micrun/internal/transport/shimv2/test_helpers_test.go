package shim

import (
	"context"

	oci "micrun/internal/adapters/config/oci"
	pedestal "micrun/internal/adapters/hypervisor/pedestal"
	appruntime "micrun/internal/application/runtime"
	"micrun/internal/ports"
	"micrun/internal/support/cpuset"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type stubGuestControl struct{}

func (stubGuestControl) Start(context.Context, string) error  { return nil }
func (stubGuestControl) Stop(context.Context, string) error   { return nil }
func (stubGuestControl) Remove(context.Context, string) error { return nil }
func (stubGuestControl) Pause(context.Context, string) error  { return nil }
func (stubGuestControl) Resume(context.Context, string) error { return nil }
func (stubGuestControl) Exists(context.Context, string) (bool, error) {
	return true, nil
}
func (stubGuestControl) Status(context.Context, string) (ports.GuestStatus, error) {
	return ports.GuestStatus{Running: true}, nil
}

type stubHypervisorControl struct{}

func (stubHypervisorControl) Type() ports.HypervisorType       { return ports.HypervisorBaremetal }
func (stubHypervisorControl) MaxCPUNum(context.Context) uint32 { return 8 }
func (stubHypervisorControl) MemoryMB(context.Context) (uint32, uint32) {
	return 1024, 2048
}
func (stubHypervisorControl) DomainState(context.Context, string) (string, error) {
	return "running", nil
}
func (stubHypervisorControl) Pause(context.Context, string) error                { return nil }
func (stubHypervisorControl) Resume(context.Context, string) error               { return nil }
func (stubHypervisorControl) SetVCPUCount(context.Context, string, uint32) error { return nil }
func (stubHypervisorControl) SetMemory(context.Context, string, uint32) error    { return nil }
func (stubHypervisorControl) SetMaxMemory(context.Context, string, uint32) error { return nil }
func (stubHypervisorControl) SetCPUWeight(context.Context, string, uint32) error { return nil }
func (stubHypervisorControl) SetCPUCapacity(context.Context, string, uint32) error {
	return nil
}

type stubPedestal struct{}

func (stubPedestal) Type() pedestal.PedType                    { return pedestal.Baremetal }
func (stubPedestal) String() string                            { return "stub" }
func (stubPedestal) GeneratePedConf() string                   { return "" }
func (stubPedestal) MaxCPUNum(context.Context) uint32          { return 8 }
func (stubPedestal) MemoryMB(context.Context) (uint32, uint32) { return 1024, 2048 }
func (stubPedestal) MemLowThreshold() uint32                   { return 16 }
func (stubPedestal) MemHighThreshold(context.Context) uint32   { return 128 }
func (stubPedestal) HostCPUSeta(context.Context) cpuset.CPUSet { return cpuset.NewCPUSet() }

func testRuntimeEnvironment() runtimeEnvironment {
	return runtimeEnvironment{
		hostProfile: oci.HostProfile{
			Type:             pedestal.Baremetal,
			MemLowThreshold:  16,
			MemHighThreshold: 128,
		},
		vcpuStats: func(context.Context) (*pedestal.XlVcpuInfo, error) {
			return &pedestal.XlVcpuInfo{}, nil
		},
		maxClientCPUs: func(context.Context, bool) int {
			return 8
		},
		planEssentialResources: func(anySpec *specs.Spec) *pedestal.EssentialResource {
			return pedestal.InitResource()
		},
		guestControl: stubGuestControl{},
		hypervisor:   stubHypervisorControl{},
	}
}

func testRuntimeServices() appruntime.Services {
	services, err := appruntime.NewServicesChecked(appruntime.Options{})
	if err != nil {
		panic(err)
	}
	return services
}

func newTaskRPCShimService() *shimService {
	service := &shimService{
		id:         "test-shim",
		containers: make(map[string]*shimContainer),
		events:     make(chan shimEvent, channelSize),
		ec:         make(chan exitEvent, channelSize),
	}
	services := testRuntimeServices()
	manager, err := newTaskManager(taskManagerDepsFromShimService(service), services.Task())
	if err != nil {
		panic(err)
	}
	service.tm = manager
	return service
}
