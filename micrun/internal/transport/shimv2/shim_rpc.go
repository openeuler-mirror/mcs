package shim

import (
	"context"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	ptypes "github.com/containerd/containerd/protobuf/types"
)

var emptyResponse = &ptypes.Empty{}

func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.StateResponse, error) {
		return manager.State(ctx, r)
	})
}

func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.CreateTaskResponse, error) {
		return manager.Create(ctx, r)
	})
}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.StartResponse, error) {
		return manager.Start(ctx, r)
	})
}

func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.DeleteResponse, error) {
		return manager.Delete(ctx, r)
	})
}

func (s *shimService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.PidsResponse, error) {
		return manager.Pids(ctx, r)
	})
}

func (s *shimService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.Pause(ctx, r)
	})
}

func (s *shimService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.Resume(ctx, r)
	})
}

func (s *shimService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.Checkpoint(ctx, r)
	})
}

func (s *shimService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.Kill(ctx, r)
	})
}

func (s *shimService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.Exec(ctx, r)
	})
}

func (s *shimService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.ResizePty(ctx, r)
	})
}

func (s *shimService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.CloseIO(ctx, r)
	})
}

func (s *shimService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.Update(ctx, r)
	})
}

func (s *shimService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.WaitResponse, error) {
		return manager.Wait(ctx, r)
	})
}

func (s *shimService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.StatsResponse, error) {
		return manager.Stats(ctx, r)
	})
}

func (s *shimService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	return callTaskManager(s, func(manager *taskManager) (*taskAPI.ConnectResponse, error) {
		return manager.Connect(ctx, r)
	})
}

func (s *shimService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	return callTaskManager(s, func(manager *taskManager) (*ptypes.Empty, error) {
		return manager.Shutdown(ctx, r)
	})
}
