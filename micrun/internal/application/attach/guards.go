package attach

import (
	"micrun/internal/ports"
	"micrun/internal/support/validation"
)

func requireRuntime(runtime ports.TaskAttachRuntime) error {
	return validation.RequireNotNil(runtime, "attach runtime is required")
}

func requireTask(taskHandle ports.Task) error {
	return validation.RequireNotNil(taskHandle, "attach task is required")
}
