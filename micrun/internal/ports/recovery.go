package ports

import "context"

// RecoveredTask describes a task reconstructed from persisted sandbox/runtime state.
type RecoveredTask struct {
	ID         string
	CanSandbox bool
	IsSandbox  bool
	IsRunning  bool
}

// RecoveryRuntime is the runtime-facing surface needed by the recovery service.
type RecoveryRuntime interface {
	Namespace() string
	RuntimeID() string
	SaveTask(id string, task Task)
	SetSandbox(Sandbox)
}

// RecoveryBackend abstracts persisted sandbox/runtime recovery.
type RecoveryBackend interface {
	CleanupOrphans(ctx context.Context, namespace string) error
	Restore(ctx context.Context, id string) (Sandbox, []RecoveredTask, error)
}
