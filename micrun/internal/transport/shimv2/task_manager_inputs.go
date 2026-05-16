package shim

import (
	apptask "micrun/internal/application/task"
	"micrun/internal/ports"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
)

type taskIDTransportRequest interface {
	GetID() string
}

type execIDTransportRequest interface {
	GetExecID() string
}

type processTransportRequest interface {
	taskIDTransportRequest
	execIDTransportRequest
}

type processTransportIdentity struct {
	ID     string
	ExecID string
}

func taskIDFromTransport(r taskIDTransportRequest) string {
	if r == nil {
		return ""
	}
	return r.GetID()
}

func execIDFromTransport(r execIDTransportRequest) string {
	if r == nil {
		return ""
	}
	return r.GetExecID()
}

func processIdentityFromTransport(r processTransportRequest) processTransportIdentity {
	if r == nil {
		return processTransportIdentity{}
	}
	return processTransportIdentity{
		ID:     r.GetID(),
		ExecID: r.GetExecID(),
	}
}

func createInputFromTransport(r *taskAPI.CreateTaskRequest) apptask.CreateInput {
	return apptask.CreateInput{Request: createTaskRequestFromTransport(r)}
}

func createTaskRequestFromTransport(r *taskAPI.CreateTaskRequest) ports.TaskCreateRequest {
	if r == nil {
		return ports.TaskCreateRequest{}
	}
	return ports.TaskCreateRequest{
		ID:       r.ID,
		Bundle:   r.Bundle,
		Rootfs:   r.Rootfs,
		Stdin:    r.Stdin,
		Stdout:   r.Stdout,
		Stderr:   r.Stderr,
		Terminal: r.Terminal,
		Options:  r.Options,
	}
}

func startInputFromTransport(r *taskAPI.StartRequest) apptask.StartInput {
	identity := processIdentityFromTransport(r)
	return apptask.StartInput{ID: identity.ID, ExecID: identity.ExecID}
}

func deleteInputFromTransport(r *taskAPI.DeleteRequest) apptask.DeleteInput {
	identity := processIdentityFromTransport(r)
	return apptask.DeleteInput{ID: identity.ID, ExecID: identity.ExecID}
}

func signalInputFromTransport(r taskIDTransportRequest) apptask.SignalInput {
	return apptask.SignalInput{ID: taskIDFromTransport(r)}
}

func killInputFromTransport(r *taskAPI.KillRequest) apptask.KillInput {
	identity := processIdentityFromTransport(r)
	if r == nil {
		return apptask.KillInput{ID: identity.ID, ExecID: identity.ExecID}
	}
	return apptask.KillInput{ID: identity.ID, ExecID: identity.ExecID, Signal: r.Signal}
}

func resizePtyInputFromTransport(r *taskAPI.ResizePtyRequest) apptask.ResizePtyInput {
	identity := processIdentityFromTransport(r)
	if r == nil {
		return apptask.ResizePtyInput{ID: identity.ID, ExecID: identity.ExecID}
	}
	return apptask.ResizePtyInput{ID: identity.ID, ExecID: identity.ExecID, Height: r.Height, Width: r.Width}
}

func closeIOInputFromTransport(r *taskAPI.CloseIORequest) apptask.CloseIOInput {
	identity := processIdentityFromTransport(r)
	if r == nil {
		return apptask.CloseIOInput{ID: identity.ID, ExecID: identity.ExecID}
	}
	return apptask.CloseIOInput{ID: identity.ID, ExecID: identity.ExecID, CloseStdin: r.Stdin}
}

func stateInputFromTransport(r *taskAPI.StateRequest) apptask.StateInput {
	identity := processIdentityFromTransport(r)
	return apptask.StateInput{ID: identity.ID, ExecID: identity.ExecID}
}

func waitInputFromTransport(r *taskAPI.WaitRequest) apptask.WaitInput {
	identity := processIdentityFromTransport(r)
	return apptask.WaitInput{ID: identity.ID, ExecID: identity.ExecID}
}
