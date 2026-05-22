package shim

import (
	apptask "micrun/internal/application/task"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func createTaskResponse(out *apptask.CreateOutput) *taskAPI.CreateTaskResponse {
	return &taskAPI.CreateTaskResponse{Pid: out.Pid}
}

func startTaskResponse(out *apptask.StartOutput) *taskAPI.StartResponse {
	return &taskAPI.StartResponse{Pid: out.Pid}
}

func deleteTaskResponse(out *apptask.DeleteOutput, exitedAt *timestamppb.Timestamp) *taskAPI.DeleteResponse {
	return deleteResponse(out.ExitStatus, exitedAt, out.Pid)
}

func deleteResponse(exitStatus uint32, exitedAt *timestamppb.Timestamp, pid uint32) *taskAPI.DeleteResponse {
	return &taskAPI.DeleteResponse{ExitStatus: exitStatus, ExitedAt: exitedAt, Pid: pid}
}

func waitTaskResponse(out *apptask.WaitOutput) *taskAPI.WaitResponse {
	return &taskAPI.WaitResponse{
		ExitStatus: out.ExitStatus,
		ExitedAt:   timestamppb.New(out.ExitedAt),
	}
}

func stateTaskResponse(out *apptask.StateOutput) *taskAPI.StateResponse {
	return &taskAPI.StateResponse{
		ID:         out.ID,
		Bundle:     out.Bundle,
		Pid:        out.Pid,
		Status:     out.Status,
		Stdin:      out.Stdin,
		Stdout:     out.Stdout,
		Stderr:     out.Stderr,
		Terminal:   out.Terminal,
		ExitStatus: out.ExitStatus,
		ExitedAt:   timestamppb.New(out.ExitedAt),
		ExecID:     out.ExecID,
	}
}

func pidsResponse(pid uint32) *taskAPI.PidsResponse {
	info := task.ProcessInfo{Pid: pid}
	return &taskAPI.PidsResponse{Processes: []*task.ProcessInfo{&info}}
}

func statsResponse(stats *ptypes.Any) *taskAPI.StatsResponse {
	return &taskAPI.StatsResponse{Stats: stats}
}

func connectResponse(pid uint32) *taskAPI.ConnectResponse {
	return &taskAPI.ConnectResponse{
		ShimPid: pid,
		TaskPid: pid,
	}
}
