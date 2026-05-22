package recovery

import (
	"context"
	"fmt"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

type recoveryOperation struct {
	ctx         context.Context
	runtime     ports.RecoveryRuntime
	backend     ports.RecoveryBackend
	taskFactory func(spec ports.RecoveredTask) ports.Task
}

func newRecoveryOperation(ctx context.Context, runtime ports.RecoveryRuntime, backend ports.RecoveryBackend, taskFactory func(spec ports.RecoveredTask) ports.Task) (recoveryOperation, bool, error) {
	if err := validation.RequireNotNil(runtime, "recovery runtime is required"); err != nil {
		return recoveryOperation{}, false, err
	}
	if validation.IsNil(backend) {
		return recoveryOperation{}, false, nil
	}
	if taskFactory == nil {
		return recoveryOperation{}, false, fmt.Errorf("recovery task factory is required")
	}
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return recoveryOperation{}, false, err
	}
	return recoveryOperation{
		ctx:         ctx,
		runtime:     runtime,
		backend:     backend,
		taskFactory: taskFactory,
	}, true, nil
}

func (op recoveryOperation) run() error {
	op.cleanupOrphans()

	sandbox, restoredTasks, err := op.backend.Restore(op.ctx, op.runtime.RuntimeID())
	if err != nil {
		return err
	}
	op.runtime.SetSandbox(sandbox)

	return restoreRecoveredTasks(op.ctx, op.runtime, restoredTasks, op.taskFactory)
}

func (op recoveryOperation) cleanupOrphans() {
	if err := op.backend.CleanupOrphans(op.ctx, op.runtime.Namespace()); err != nil {
		log.Debugf("recovery cleanup skipped: %v", err)
	}
}
