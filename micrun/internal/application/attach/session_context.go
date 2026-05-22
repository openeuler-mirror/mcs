package attach

import (
	"context"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/validation"
)

func attachSessionContext(ctx context.Context, runtime ports.TaskAttachRuntime) context.Context {
	if backgroundCtx := attachRuntimeBackgroundContext(runtime); backgroundCtx != nil {
		return backgroundCtx
	}
	return context.WithoutCancel(contextx.OrBackground(ctx))
}

func attachRuntimeBackgroundContext(runtime ports.TaskAttachRuntime) context.Context {
	if !validation.IsNil(runtime) {
		if backgroundCtx := runtime.BackgroundContext(); backgroundCtx != nil {
			return backgroundCtx
		}
	}
	return nil
}
