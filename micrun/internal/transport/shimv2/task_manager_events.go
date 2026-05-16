package shim

import (
	"micrun/internal/ports"

	"github.com/containerd/containerd/api/events"
	eventstypes "github.com/containerd/containerd/api/events"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (m *taskManager) emitTaskCreated(req ports.TaskCreateRequest, checkpoint string, pid uint32) {
	m.events.send(&events.TaskCreate{
		ContainerID: req.ID,
		Bundle:      req.Bundle,
		Rootfs:      req.Rootfs,
		IO: &eventstypes.TaskIO{
			Stdin:    req.Stdin,
			Stdout:   req.Stdout,
			Stderr:   req.Stderr,
			Terminal: req.Terminal,
		},
		Checkpoint: checkpoint,
		Pid:        pid,
	})
}

func (m *taskManager) emitTaskStarted(containerID, execID string, pid uint32) {
	if execID != "" {
		m.events.send(&events.TaskExecStarted{
			ContainerID: containerID,
			ExecID:      execID,
			Pid:         pid,
		})
		return
	}

	m.events.send(&events.TaskStart{
		ContainerID: containerID,
		Pid:         pid,
	})
}

func (m *taskManager) emitTaskDeleted(containerID string, exitStatus uint32, pid uint32, exitedAt *timestamppb.Timestamp) {
	m.events.send(&events.TaskDelete{
		ContainerID: containerID,
		ExitedAt:    exitedAt,
		Pid:         pid,
		ExitStatus:  exitStatus,
	})
}

func (m *taskManager) emitTaskPaused(containerID string) {
	m.events.send(&events.TaskPaused{ContainerID: containerID})
}

func (m *taskManager) emitTaskResumed(containerID string) {
	m.events.send(&events.TaskResumed{ContainerID: containerID})
}
