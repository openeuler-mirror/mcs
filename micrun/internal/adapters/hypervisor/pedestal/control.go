package pedestal

import (
	"context"

	"micrun/internal/ports"
)

type Control struct {
	host *PedestalFacade
}

func NewControl(host *PedestalFacade) Control {
	return Control{host: host}
}

func (c Control) facade() *PedestalFacade {
	return c.host
}

func (c Control) Type() ports.HypervisorType {
	host := c.facade()
	if host == nil {
		return ports.HypervisorUnsupported
	}
	switch host.Type() {
	case Xen:
		return ports.HypervisorXen
	case Baremetal:
		return ports.HypervisorBaremetal
	default:
		return ports.HypervisorUnsupported
	}
}

func (c Control) MaxCPUNum(ctx context.Context) uint32 {
	if host := c.facade(); host != nil {
		return host.MaxCPUNum(ctx)
	}
	return 0
}

func (c Control) MemoryMB(ctx context.Context) (uint32, uint32) {
	if host := c.facade(); host != nil {
		return host.MemoryMB(ctx)
	}
	return 0, 0
}

func (c Control) DomainState(ctx context.Context, id string) (string, error) {
	if host := c.facade(); host != nil {
		return host.DomainState(ctx, id)
	}
	return "", ErrNotSupported
}

func (c Control) Pause(ctx context.Context, id string) error {
	if host := c.facade(); host != nil {
		return host.Pause(ctx, id)
	}
	return ErrNotSupported
}

func (c Control) Resume(ctx context.Context, id string) error {
	if host := c.facade(); host != nil {
		return host.Resume(ctx, id)
	}
	return ErrNotSupported
}

func (c Control) SetVCPUCount(ctx context.Context, id string, count uint32) error {
	if host := c.facade(); host != nil {
		return host.SetVCPUCount(ctx, id, count)
	}
	return ErrNotSupported
}

func (c Control) SetMemory(ctx context.Context, id string, memMB uint32) error {
	if host := c.facade(); host != nil {
		return host.SetMemory(ctx, id, memMB)
	}
	return ErrNotSupported
}

func (c Control) SetMaxMemory(ctx context.Context, id string, memMB uint32) error {
	if host := c.facade(); host != nil {
		return host.SetMaxMemory(ctx, id, memMB)
	}
	return ErrNotSupported
}

func (c Control) SetCPUWeight(ctx context.Context, id string, weight uint32) error {
	if host := c.facade(); host != nil {
		return host.SetCPUWeight(ctx, id, weight)
	}
	return ErrNotSupported
}

func (c Control) SetCPUCapacity(ctx context.Context, id string, capacity uint32) error {
	if host := c.facade(); host != nil {
		return host.SetCPUCapacity(ctx, id, capacity)
	}
	return ErrNotSupported
}
