package shim

import (
	"context"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/errdefs"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (m *taskManager) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	if err := requireTransportRequest("create", r); err != nil {
		return nil, err
	}
	in := createInputFromTransport(r)
	out, err := m.service.Create(ctx, m.create, in)
	if err != nil {
		return nil, err
	}

	m.emitTaskCreated(in.Request, createTaskCheckpoint(r), out.Pid)
	return createTaskResponse(out), nil
}

func createTaskCheckpoint(r *taskAPI.CreateTaskRequest) string {
	if r == nil {
		return ""
	}
	return r.Checkpoint
}

func (m *taskManager) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	if err := requireTransportRequest("start", r); err != nil {
		return nil, err
	}
	in := startInputFromTransport(r)
	out, err := m.service.Start(ctx, m.start, in)
	if err != nil {
		return nil, grpcExecAwareRequestErrorWithFallback(r, err, errdefs.ToGRPC)
	}

	m.emitTaskStarted(out.ContainerID, out.ExecID, out.Pid)
	return startTaskResponse(out), nil
}

func (m *taskManager) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	if err := requireTransportRequest("delete", r); err != nil {
		return nil, err
	}
	in := deleteInputFromTransport(r)
	out, err := m.service.Delete(ctx, m.delete, in)
	if err != nil {
		return nil, grpcExecAwareRequestError(r, err)
	}

	exitedAt := timestamppb.New(out.ExitedAt)
	m.emitTaskDeleted(out.ContainerID, out.ExitStatus, out.Pid, exitedAt)

	return deleteTaskResponse(out, exitedAt), nil
}

func (m *taskManager) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("pause", r); err != nil {
		return nil, err
	}
	out, err := m.service.Pause(ctx, m.signal, signalInputFromTransport(r))
	if err != nil {
		return nil, err
	}
	if out.EmitEvent {
		m.emitTaskPaused(out.ContainerID)
	}
	return emptyResponse, nil
}

func (m *taskManager) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("resume", r); err != nil {
		return nil, err
	}
	out, err := m.service.Resume(ctx, m.signal, signalInputFromTransport(r))
	if err != nil {
		return nil, err
	}
	if out.EmitEvent {
		m.emitTaskResumed(out.ContainerID)
	}
	return emptyResponse, nil
}

func (m *taskManager) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("kill", r); err != nil {
		return nil, err
	}
	if err := m.service.Kill(ctx, m.signal, killInputFromTransport(r)); err != nil {
		return nil, grpcExecAwareRequestError(r, err)
	}
	return emptyResponse, nil
}

func (m *taskManager) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("resize pty", r); err != nil {
		return nil, err
	}
	if err := m.service.ResizePty(ctx, m.io, resizePtyInputFromTransport(r)); err != nil {
		return nil, grpcExecAwareRequestError(r, err)
	}
	return emptyResponse, nil
}

func (m *taskManager) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("close io", r); err != nil {
		return nil, err
	}
	if err := m.service.CloseIO(ctx, m.io, closeIOInputFromTransport(r)); err != nil {
		return nil, grpcExecAwareRequestError(r, err)
	}
	return emptyResponse, nil
}

func (m *taskManager) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("update", r); err != nil {
		return nil, err
	}
	in, err := updateInputFromTransport(r)
	if err != nil {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "invalid update resources: %v", err)
	}
	if err := m.service.Update(ctx, m.io, in); err != nil {
		return nil, err
	}
	return emptyResponse, nil
}

func (m *taskManager) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	if err := requireTransportRequest("wait", r); err != nil {
		return nil, err
	}
	out, err := m.service.Wait(ctx, m.wait, waitInputFromTransport(r))
	if err != nil {
		return nil, grpcExecAwareRequestError(r, err)
	}
	return waitTaskResponse(out), nil
}

func (m *taskManager) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("checkpoint", r); err != nil {
		return nil, err
	}
	return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "checkpoint is not supported for micrun task %s", r.ID)
}

func (m *taskManager) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("exec", r); err != nil {
		return nil, err
	}
	return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "exec is not supported for micrun task %s", r.ID)
}
