package lifecycle

import (
	"context"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/validation"
)

type taskContext struct {
	Context context.Context
	Runtime ports.TaskLifecycleRuntime
	Task    ports.Task
}

func newTaskContext(ctx context.Context, runtime ports.TaskLifecycleRuntime, task ports.Task) *taskContext {
	return &taskContext{
		Context: contextx.OrBackground(ctx),
		Runtime: runtime,
		Task:    task,
	}
}

func lifecycleEventContext(fallback context.Context, runtime ports.TaskLifecycleRuntime) context.Context {
	if !validation.IsNil(runtime) {
		if backgroundCtx := runtime.BackgroundContext(); backgroundCtx != nil {
			return backgroundCtx
		}
	}
	return context.WithoutCancel(contextx.OrBackground(fallback))
}
