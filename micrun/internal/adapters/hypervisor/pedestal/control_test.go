package pedestal

import (
	"context"
	"testing"

	"micrun/internal/ports"
	"micrun/internal/support/cpuset"
)

type controlTestPedestal struct{}

func (controlTestPedestal) Type() PedType                             { return Baremetal }
func (controlTestPedestal) String() string                            { return "control-test" }
func (controlTestPedestal) GeneratePedConf() string                   { return "" }
func (controlTestPedestal) MaxCPUNum(context.Context) uint32          { return 6 }
func (controlTestPedestal) MemoryMB(context.Context) (uint32, uint32) { return 256, 512 }
func (controlTestPedestal) MemLowThreshold() uint32                   { return 16 }
func (controlTestPedestal) MemHighThreshold(context.Context) uint32   { return 128 }
func (controlTestPedestal) HostCPUSeta(context.Context) cpuset.CPUSet { return cpuset.NewCPUSet(0, 1) }

func TestControlUsesExplicitFacade(t *testing.T) {
	control := NewControl(NewPedestalFacade(controlTestPedestal{}))

	if got := control.Type(); got != ports.HypervisorBaremetal {
		t.Fatalf("Type() = %s, want %s", got, ports.HypervisorBaremetal)
	}
	if got := control.MaxCPUNum(context.Background()); got != 6 {
		t.Fatalf("MaxCPUNum() = %d, want 6", got)
	}
	free, total := control.MemoryMB(context.Background())
	if free != 256 || total != 512 {
		t.Fatalf("MemoryMB() = (%d, %d), want (256, 512)", free, total)
	}
}

func TestControlWithoutFacadeIsUnsupported(t *testing.T) {
	control := Control{}

	if got := control.Type(); got != ports.HypervisorUnsupported {
		t.Fatalf("Type() = %s, want %s", got, ports.HypervisorUnsupported)
	}
	if got := control.MaxCPUNum(context.Background()); got != 0 {
		t.Fatalf("MaxCPUNum() = %d, want 0", got)
	}
	if _, err := control.DomainState(context.Background(), "demo"); err != ErrNotSupported {
		t.Fatalf("DomainState() error = %v, want ErrNotSupported", err)
	}
}
