package ports

import "context"

// GuestStatus is a runtime-agnostic projection of guest state.
type GuestStatus struct {
	State   string
	Raw     string
	Running bool
	Stopped bool
}

// GuestControl abstracts lifecycle and status operations against the guest
// backend, such as micad.
type GuestControl interface {
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	Exists(ctx context.Context, id string) (bool, error)
	Status(ctx context.Context, id string) (GuestStatus, error)
}
