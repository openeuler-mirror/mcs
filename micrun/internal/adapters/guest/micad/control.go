package micad

import (
	"context"
	"errors"
	"strings"

	libmica "micrun/internal/adapters/guest/libmica"
	pedestal "micrun/internal/adapters/hypervisor/pedestal"
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

func (c Control) Stop(ctx context.Context, id string) error {
	exists, err := libmica.ClientExists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return libmica.StopContext(ctx, id)
	}
	// Socket wiped (micad restart) but Xen domain may still be alive:
	// StopContext would no-op and leave the domain for Delete/Start to leak.
	return c.destroyLingeringDomain(ctx, id)
}

func (c Control) Remove(ctx context.Context, id string) error {
	exists, err := libmica.ClientExists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return libmica.RemoveContext(ctx, id)
	}
	return c.destroyLingeringDomain(ctx, id)
}

// destroyLingeringDomain tears down a Xen domain that outlived its micad
// control socket. No-op when the domain is already gone or the pedestal
// cannot destroy domains.
func (c Control) destroyLingeringDomain(ctx context.Context, id string) error {
	if c.Hypervisor == nil {
		return nil
	}
	_, err := c.Hypervisor.DomainState(ctx, id)
	if err != nil {
		if errors.Is(err, pedestal.ErrNotSupported) || isMissingDomainError(err) {
			return nil
		}
		return err
	}
	if err := c.Hypervisor.Destroy(ctx, id); err != nil {
		if errors.Is(err, pedestal.ErrNotSupported) || isMissingDomainError(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c Control) Pause(ctx context.Context, id string) error {
	return libmica.PauseWithHypervisorContext(ctx, id, c.Hypervisor)
}

func (c Control) Resume(ctx context.Context, id string) error {
	return libmica.ResumeWithHypervisorContext(ctx, id, c.Hypervisor)
}

func (Control) Exists(ctx context.Context, id string) (bool, error) {
	// Socket-only: checkState treats !Exists as Down so a micad restart that
	// wiped /run/mica can converge Ready→Down and re-register. Lingering Xen
	// domains are cleared by Stop/Remove (xl destroy) and by always Remove
	// before registerClient when state is Down — not by Exists.
	return libmica.ClientExists(ctx, id)
}

func isMissingDomainError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
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
