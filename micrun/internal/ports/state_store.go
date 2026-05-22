package ports

import "context"

// RuntimeSnapshot is a generic persisted runtime snapshot payload.
// Concrete callers can marshal their own typed content into Data.
type RuntimeSnapshot struct {
	Namespace string
	TaskID    string
	Data      []byte
}

// StateStore abstracts persistence for runtime instance state.
type StateStore interface {
	Load(ctx context.Context, namespace, taskID string) (*RuntimeSnapshot, error)
	Save(ctx context.Context, snapshot *RuntimeSnapshot) error
	Delete(ctx context.Context, namespace, taskID string) error
}
