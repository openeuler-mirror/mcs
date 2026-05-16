package task

import "micrun/internal/ports"

func stateOutput(runtime ports.TaskIdentity, taskHandle ports.Task, execID string) *StateOutput {
	return &StateOutput{
		ID:         taskHandle.ID(),
		Bundle:     taskHandle.Bundle(),
		Pid:        taskPIDOrShim(runtime, taskHandle),
		Status:     taskHandle.Status(),
		Stdin:      taskHandle.StdinPath(),
		Stdout:     taskHandle.StdoutPath(),
		Stderr:     taskHandle.StderrPath(),
		Terminal:   taskHandle.Terminal(),
		ExitStatus: taskHandle.ExitStatus(),
		ExitedAt:   taskHandle.ExitTime(),
		ExecID:     execID,
	}
}

func waitOutput(taskHandle ports.Task) *WaitOutput {
	return &WaitOutput{
		ExitStatus: taskHandle.ExitStatus(),
		ExitedAt:   taskHandle.ExitTime(),
	}
}
