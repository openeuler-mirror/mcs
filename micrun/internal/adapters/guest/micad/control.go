package micad

import (
	"context"

	libmica "micrun/internal/adapters/guest/libmica"
	"micrun/internal/ports"
)

// Control is the default guest backend adapter backed by micad/libmica.
type Control struct {
	Hypervisor ports.HypervisorControl
}

func NewControl(hypervisor ports.HypervisorControl) Control {
	return Control{Hypervisor: hypervisor}
}

func (Control) Start(ctx context.Context, id string) error {
	return libmica.StartContext(ctx, id)
}

func (Control) Stop(ctx context.Context, id string) error {
	return libmica.StopContext(ctx, id)
}

func (Control) Remove(ctx context.Context, id string) error {
	return libmica.RemoveContext(ctx, id)
}

func (c Control) Pause(ctx context.Context, id string) error {
	return libmica.PauseWithHypervisorContext(ctx, id, c.Hypervisor)
}

func (c Control) Resume(ctx context.Context, id string) error {
	return libmica.ResumeWithHypervisorContext(ctx, id, c.Hypervisor)
}

func (Control) Exists(ctx context.Context, id string) (bool, error) {
	return libmica.ClientExists(ctx, id)
}

func (c Control) Status(ctx context.Context, id string) (ports.GuestStatus, error) {
	status, err := libmica.StatusWithHypervisor(ctx, id, c.Hypervisor)
	if err != nil {
		return ports.GuestStatus{}, err
	}
	return ports.GuestStatus{
		State:   string(status.State),
		Raw:     status.Raw,
		Running: string(status.State) == "Running",
		Stopped: status.IsStopped(),
	}, nil
}
