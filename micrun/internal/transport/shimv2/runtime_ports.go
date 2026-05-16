package shim

import (
	"context"
	"io"
	"time"

	cntr "micrun/internal/domain/container"
	ports "micrun/internal/ports"
	"micrun/internal/support/contextx"
	er "micrun/internal/support/errors"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
	ptypes "github.com/containerd/containerd/protobuf/types"
)

var _ ports.TaskRuntime = (*shimService)(nil)
var _ ports.TaskCreateRuntime = (*shimService)(nil)
var _ ports.TaskStartRuntime = (*shimService)(nil)
var _ ports.TaskDeleteRuntime = (*shimService)(nil)
var _ ports.TaskSignalRuntime = (*shimService)(nil)
var _ ports.TaskIORuntime = (*shimService)(nil)
var _ ports.TaskWaitRuntime = (*shimService)(nil)
var _ ports.TaskQueryRuntime = (*shimService)(nil)
var _ taskRuntimeEvents = (*shimService)(nil)
var _ taskRuntimeProcess = (*shimService)(nil)
var _ taskRuntimeMetrics = (*shimService)(nil)
var _ taskRuntimeTaskPresence = (*shimService)(nil)
var _ taskRuntimeShutdown = (*shimService)(nil)

func (s *shimService) Namespace() string {
	if s == nil {
		return ""
	}
	return s.namespace
}

func (s *shimService) BackgroundContext() context.Context {
	if s == nil || s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *shimService) ShimPID() uint32 {
	if s != nil && s.shimPid > 0 {
		return s.shimPid
	}
	if s != nil && s.processID != nil {
		return s.processID.PID()
	}
	return osProcessIDProvider{}.PID()
}

func (s *shimService) LookupTask(id string) (ports.Task, bool) {
	return s.lookupShimTask(id)
}

func (s *shimService) SaveTask(id string, taskHandle ports.Task) {
	s.saveShimTask(id, taskHandle)
}

func (s *shimService) DeleteTask(id string) {
	s.deleteShimTask(id)
}

func (s *shimService) CreateTask(ctx context.Context, r ports.TaskCreateRequest) (ports.Task, error) {
	return create(ctx, s, r)
}

func (s *shimService) CleanupTask(ctx context.Context, taskHandle ports.Task) error {
	c, ok := taskHandle.(*shimContainer)
	if !ok {
		return nil
	}
	return deleteContainer(ctx, s, c)
}

type runtimeSandbox struct {
	cntr.SandboxTraits
}

func (s runtimeSandbox) sandboxTraitsChecked() (cntr.SandboxTraits, error) {
	if validation.IsNil(s.SandboxTraits) {
		return nil, er.SandboxNotFound
	}
	return s.SandboxTraits, nil
}

func (s runtimeSandbox) KillContainer(ctx context.Context, id string) error {
	sandbox, err := s.sandboxTraitsChecked()
	if err != nil {
		return err
	}
	_, err = sandbox.KillContainer(ctx, id)
	return err
}

func (s runtimeSandbox) StartContainer(ctx context.Context, id string) error {
	sandbox, err := s.sandboxTraitsChecked()
	if err != nil {
		return err
	}
	_, err = sandbox.StartContainer(ctx, id)
	return err
}

func (s runtimeSandbox) StopContainer(ctx context.Context, id string, force bool) error {
	sandbox, err := s.sandboxTraitsChecked()
	if err != nil {
		return err
	}
	_, err = sandbox.StopContainer(ctx, id, force)
	return err
}

func (s runtimeSandbox) IOStream(ctx context.Context, containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	sandbox, err := s.sandboxTraitsChecked()
	if err != nil {
		return nil, nil, nil, err
	}
	return sandbox.IOStream(ctx, containerID, taskID)
}

func (s *shimService) Sandbox() ports.Sandbox {
	return s.sandboxRuntime()
}

func (s *shimService) SetSandbox(sandbox ports.Sandbox) {
	if validation.IsNil(sandbox) {
		s.clearSandbox()
		return
	}
	if sb, ok := sandbox.(runtimeSandbox); ok {
		s.setSandboxTraits(sb.SandboxTraits)
	}
}

func (s *shimService) QueryTaskStatus(ctx context.Context, id string) (task.Status, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return task.Status_UNKNOWN, err
	}
	return s.getContainerStatus(ctx, id)
}

func (s *shimService) MarkKilledByAPI() {
	if s == nil {
		return
	}
	s.killedByAPI = true
}

func (s *shimService) ReportTaskExit(task ports.Task, status int, exitedAt time.Time) {
	if s == nil || validation.IsNil(task) {
		return
	}
	event := exitEvent{
		ts:     exitedAt,
		cid:    task.ID(),
		execid: "",
		pid:    s.ShimPID(),
		status: status,
	}
	if s.ec == nil {
		s.reportExit(event)
		return
	}
	s.ec <- event
}

func (s *shimService) EmptyMetrics() *ptypes.Any {
	return emptyMetricsV1()
}
