package shim

import (
	"context"
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	cntr "micrun/pkg/micantainer"
	oci "micrun/pkg/oci"
	"micrun/pkg/utils"

	"github.com/containerd/containerd/api/events"
	eventstypes "github.com/containerd/containerd/api/events"
	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"
	ptypes "github.com/containerd/containerd/protobuf/types"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
	"github.com/containerd/typeurl/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	channelSize = 128
	okCode      = 0
	exitCode    = 255
)

var (
	_       taskAPI.TaskService = (*shimService)(nil)
	shimPid                     = uint32(os.Getpid())
)

// TALK: why shimService maintains `mu`? why only one lock field?
// why not guard containers field by lock?
// 1. why shimService maintains a `mu` lock field?
// > parallel RPC to shim service (Create, Start, Kill... at the same time)
// > hence we need to ensure consistency and MT safe
// 2. why not guard `containers: map[string]*shimContainer` field by a lock?
// > due to mica rtos containerlization modle, no need to protect containers,
// map[string]*shimContainer is simple data struct, not about real process
// 3. why not guard event sender by a lock?
// the go channel is basically safe enough.
type shimService struct {
	id         string
	micadPid   int
	shimPid    int
	namespace  string
	config     *oci.RuntimeConfig
	containers map[string]*shimContainer
	sandbox    cntr.SandboxTraits
	ctx        context.Context
	events     chan any
	ec         chan exitEvent
	ss         func()
	monitor    chan error
	mu         sync.Mutex
}

func New(ctx context.Context, id string, publisher shimv2.Publisher, shutdown func()) (shimv2.Shim, error) {
	ns, found := namespaces.Namespace(ctx)
	if !found {
		return nil, fmt.Errorf("namespace is required")
	}

	micadPid, err := getMicadPid()
	if err != nil {
		log.Warnf("failed to get micad PID, setting to 0: %v", err)
		return nil, err
	}

	s := &shimService{
		id:        id,
		micadPid:  micadPid,
		shimPid:   os.Getpid(),
		namespace: ns,
		ctx:       ctx,
		events:    make(chan any, channelSize),
		ss:        shutdown,
		monitor:   make(chan error),
	}

	log.Debugf("starting service background goroutines exit listener")

	go s.listenAndReportExits()

	// Start events forwarder to publish events to containerd
	forwarder := s.newEventsForwarder(ctx, publisher)
	go forwarder.forward()

	log.Debugf("completed successfully, returning shimService")
	return s, nil
}

func newCommand(ctx context.Context, opts shimv2.StartOpts, cwd string) (*exec.Cmd, error) {

	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get current executable path: %w", err)
	}

	var args []string
	if opts.Debug {
		args = append(args, "-debug")
		args = append(args, "-id", opts.ID)
	}

	// TTRPC_ADDRESS the address of containerd's ttrpc API socket
	// GRPC_ADDRESS the address of containerd's grpc API socket (1.7+)
	// MAX_SHIM_VERSION the maximum shim version supported by the client, always 2 for shim v2 (1.7+)
	// SCHED_CORE enable core scheduling if available (1.6+)
	// NAMESPACE an optional namespace the shim is operating in or inheriting (1.7+)
	// LOG_COLOR controls colored output in the shim process
	cmdCfg := &shimv2.CommandConfig{
		Runtime:      self,
		Address:      opts.Address,
		TTRPCAddress: opts.TTRPCAddress,
		// resolved expanded path
		Path:      cwd,
		SchedCore: os.Getenv(contdShimEnvShedCore) != "",
		Args:      args,
	}

	// -namespace the namespace for the container
	// -address the address of the containerd's main grpc socket
	// -publish-binary the binary path to publish events back to containerd
	// -id the id of the container (containerID)
	// The start command, as well as all binary calls to the shim, has the bundle for the container set as the cwd.
	cmd, err := shimv2.Command(ctx, cmdCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create shim command: %w", err)
	}
	// Pass LOG_COLOR environment variable to child process
	if logColor := os.Getenv("LOG_COLOR"); logColor != "" {
		cmd.Env = append(cmd.Env, "LOG_COLOR="+logColor)
	}

	// Do not redirect child's stdout here. The parent `start` path is
	// responsible for emitting the address cleanly; child's logging(info,warn,error) is
	// routed to containerd via the shim FIFO logger setup.
	return cmd, nil
}

func (s *shimService) StartShim(ctx context.Context, opts shimv2.StartOpts) (_ string, retErr error) {
	// origLevel := log.Log.GetLevel()
	// log.Log.SetLevel(logrus.WarnLevel)
	// defer log.Log.SetLevel(origLevel)
	bundle, err := os.Getwd()
	if err != nil {
		return "", err
	}

	bundle, err = validBundle(opts.ID, bundle)
	if err != nil {
		return "", err
	}

	sockaddr, err := getContainerSocketAddr(ctx, bundle, opts)
	if err != nil {
		return "", err
	}

	// if podContainer/singleContainer: do not need a new shim binary, only write socket and then finished starting
	if sockaddr != "" {
		// write <socketaddr> into <bundle>/address socket
		if err := shimv2.WriteAddress("address", sockaddr); err != nil {
			return "", fmt.Errorf("failed to write socket address for pod container: %w", err)
		}
		return sockaddr, nil
	}

	log.Debugf("args: %s", os.Args)
	cmd, err := newCommand(ctx, opts, bundle)
	if err != nil {
		return "", err
	}

	// single container / sandbox
	sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, opts.ID)
	if err != nil {
		return "", err
	}

	socket, err := shimv2.NewSocket(sockAddr)

	if err != nil {
		// containerd:
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !shimv2.SocketEaddrinuse(err) {
			return "", fmt.Errorf("create new shim socket: %w", err)
		}
		if shimv2.CanConnect(sockAddr) {
			if err := shimv2.WriteAddress("address", sockAddr); err != nil {
				return "", fmt.Errorf("write existing socket for shim: %w", err)
			}
			return sockAddr, nil
		}
		log.Debugf("removing stale socket and creating new one")
		if err := shimv2.RemoveSocket(sockAddr); err != nil {
			return "", fmt.Errorf("remove pre-existing socket: %w", err)
		}
		if socket, err = shimv2.NewSocket(sockAddr); err != nil {
			return "", fmt.Errorf("try create new shim socket 2x: %w", err)
		}
	}

	defer func() {
		if retErr != nil {
			socket.Close()
			if err := shimv2.RemoveSocket(sockAddr); err != nil {
				log.Debugf("failed to remove socket %s: %v", sockAddr, err)
			}
		}
	}()

	// make sure that reexec shim-v2 binary use the value if need
	if err := shimv2.WriteAddress("address", sockAddr); err != nil {
		return "", err
	}

	sock, err := socket.File()
	if err != nil {
		return "", err
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, sock)

	runtime.LockOSThread()
	if os.Getenv("SCHED_CORE") != "" {
		tipSchedCore()
	}

	if err := cmd.Start(); err != nil {
		_ = sock.Close()
		return "", fmt.Errorf("failed to start shim task service: %w", err)
	}

	runtime.UnlockOSThread()

	// BUG: sometimes micrun failed to ensure the container socket is dropped after deleted
	// result in socket leak even if containerd managed to remove the socket
	defer func() {
		if retErr != nil {
			if err := cmd.Process.Kill(); err != nil {
				time.Sleep(2 * time.Second)
				log.Debugf("failed to kill shim process: %v, try again: %v", err, cmd.Process.Kill().Error())
			}
		}
	}()

	// Wait in background to avoid zombie if parent outlives child briefly.
	go cmd.Wait()

	if err = shimv2.WritePidFile("shim.pid", cmd.Process.Pid); err != nil {
		return "", fmt.Errorf("failed to write shim PID file: %w", err)
	}

	if err = shimv2.WriteAddress("address", sockAddr); err != nil {
		return "", err
	}

	// best effort
	err = setupStateDir()
	if err != nil {
		log.Warnf("failed to setup micrun state directory: %v", err)
	}

	return sockAddr, nil

}

// steps:
// delete forcely, cleanup containers in memory
// unmount recursively
// clean pidfile
// send event
// return Response
func (s *shimService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Tips from kata-container:
	// Since the binary cleanup will return the DeleteResponse from stdout to
	// containerd, thus we must make sure there is no any outputs in stdout except
	// the returned response, thus here redirect the log to stderr in case there's
	// any log output to stdout.
	logrus.SetOutput(os.Stderr)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if s.id == "" {
		return nil, fmt.Errorf("container ID is required")
	}

	ociSpec, err := oci.LoadSpec(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to load valid runtime config: %w", err)
	}

	ctype, err := oci.GetContainerType(&ociSpec)
	if err != nil {
		return nil, err
	}
	switch ctype {
	case cntr.PodSandbox, cntr.SingleContainer:
		err = cleanupContainer(ctx, s.id, s.id, cwd)
		if err != nil {
			return nil, err
		}
	case cntr.PodContainer:
		sandboxID, err := oci.GetSandboxID(&ociSpec)
		if err != nil {
			return nil, err
		}
		err = cleanupContainer(ctx, sandboxID, s.id, cwd)
		if err != nil {
			return nil, err
		}
	default:
		log.Debugf("unknown container type to be cleaned up: %s", ctype)
	}

	return &taskAPI.DeleteResponse{
		ExitedAt:   timestamppb.New(time.Now()),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

// ***************** taskAPI task entries ********************

var emptyResponse = &ptypes.Empty{}

func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, fmt.Errorf("container %s not found", r.ID)
	}

	return &taskAPI.StateResponse{
		ID:         c.id,
		Bundle:     c.bundle,
		Pid:        shimPid,
		Status:     c.status,
		Stdin:      c.stdin,
		Stdout:     c.stdout,
		Stderr:     c.stderr,
		Terminal:   c.terminal,
		ExitStatus: c.exit,
		ExitedAt:   timestamppb.New(c.exitTime),
		ExecID:     r.ExecID,
	}, nil
}

// does not send request to micad, create container in memory
func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	log.Debugf("creating task %s (bundle: %s, terminal: %v)", r.ID, r.Bundle, r.Terminal)
	if err := utils.ValidContainerID(r.ID); err != nil {
		return nil, er.InvalidCID
	}

	// create container sync
	container, err := create(ctx, s, r)
	if err != nil {
		return nil, err
	}
	// lock when updating shared state
	s.mu.Lock()
	container.status = task.Status_CREATED
	s.containers[r.ID] = container
	s.mu.Unlock()

	pid := container.pid
	if pid <= 0 {
		pid = shimPid
	}
	s.send(&events.TaskCreate{
		ContainerID: r.ID,
		Bundle:      r.Bundle,
		Rootfs:      r.Rootfs,
		IO: &eventstypes.TaskIO{
			Stdin:    r.Stdin,
			Stdout:   r.Stdout,
			Stderr:   r.Stderr,
			Terminal: r.Terminal,
		},
		Checkpoint: r.Checkpoint,
		Pid:        pid,
	})

	return &taskAPI.CreateTaskResponse{
		Pid: pid,
	}, nil
}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Debugf("starting container %s", r.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		log.Debugf("container %s not found in shim service storage", r.ID)
		return nil, er.ContainerNotFound
	}

	respPid := shimPid
	if r.ExecID != "" {
		log.Infof("container %s has no exec process", r.ID)
		s.send(&events.TaskExecStarted{
			ContainerID: c.id,
			ExecID:      r.ExecID,
			Pid:         respPid,
		})
	} else {
		log.Infof("starting container %s", c.id)
		err := startContainer(ctx, s, c)
		if err != nil {
			return nil, errdefs.ToGRPC(err)
		}
		if c.pid != 0 {
			respPid = c.pid
		}
		s.send(&events.TaskStart{
			ContainerID: c.id,
			Pid:         respPid,
		})
	}

	return &taskAPI.StartResponse{
		Pid: respPid,
	}, nil
}
func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()
	log.Debugf("deleting container %s", r.ID)

	c, found := s.containers[r.ID]
	if c == nil || !found {
		log.Debugf("container %s not found in shim service storage (idempotent delete)", r.ID)
		s.send(&events.TaskDelete{
			ContainerID: r.ID,
			ExitedAt:    timestamppb.Now(),
			Pid:         shimPid,
			ExitStatus:  okCode,
		})
		delete(s.containers, r.ID)
		return &taskAPI.DeleteResponse{
			ExitStatus: okCode,
			ExitedAt:   timestamppb.Now(),
			Pid:        shimPid,
		}, nil
	}

	if r.ExecID != "" {
		return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "exec processes are not supported for container %s", r.ID)
	}

	// delete single container or entire sandbox
	if c.cType.CanBeSandbox() {
		if s.sandbox == nil {
			log.Debugf("Sandbox already deleted in Delete method for container %s", c.id)
		} else {
			sandboxID := s.sandbox.SandboxID()

			if err := s.sandbox.Stop(ctx, true); err != nil {
				log.Debugf("Stop sandbox %s returned: %v", sandboxID, err)
			}
			if err := s.sandbox.Delete(ctx); err != nil {
				log.Debugf("Delete sandbox %s returned: %v", sandboxID, err)
			}
			s.sandbox = nil
		}
	}

	// Delete the container (handles pod containers, unmount, registry cleanup)
	if err := deleteContainer(ctx, s, c); err != nil {
		return nil, err
	}

	pid := c.pid
	if pid == 0 {
		pid = shimPid
	}

	s.send(&events.TaskDelete{
		ContainerID: r.ID,
		ExitedAt:    timestamppb.New(c.exitTime),
		Pid:         pid,
		ExitStatus:  c.exit,
	})

	return &taskAPI.DeleteResponse{
		ExitStatus: c.exit,
		ExitedAt:   timestamppb.New(c.exitTime),
		Pid:        pid,
	}, nil
}
func (s *shimService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info := task.ProcessInfo{
		Pid: shimPid,
	}
	proc := make([]*task.ProcessInfo, 1)
	proc[0] = &info
	return &taskAPI.PidsResponse{
		Processes: proc,
	}, nil
}

// Pause pauses a container by calling sandbox.PauseContainer.
func (s *shimService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if !found || c == nil {
		return nil, er.ContainerNotFound
	}

	c.status = task.Status_PAUSING
	if s.sandbox == nil {
		log.Debugf("Sandbox is nil, cannot pause container %s", r.ID)
		return nil, er.SandboxNotFound
	}

	err := s.sandbox.PauseContainer(ctx, r.ID)
	if err == nil {
		c.status = task.Status_PAUSED
		s.send(&events.TaskPaused{
			ContainerID: c.id,
		})
		return emptyResponse, nil
	}

	status, err := s.getContainerStatus(c.id)
	if err != nil {
		log.Debugf("container %s status query failed: %v", r.ID, err)
		c.status = task.Status_UNKNOWN
	} else {
		log.Debugf("container %s status: %s", r.ID, status)
		c.status = status
	}

	return emptyResponse, nil
}

// Resume resumes a paused container by calling sandbox.ResumeContainer.
func (s *shimService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ContainerNotFound
	}

	if s.sandbox == nil {
		log.Debugf("Sandbox is nil, cannot resume container %s", c.id)
		return nil, er.SandboxNotFound
	}

	err := s.sandbox.ResumeContainer(ctx, c.id)
	if err == nil {
		c.status = task.Status_RUNNING
		s.send(&events.TaskResumed{
			ContainerID: c.id,
		})
		return emptyResponse, nil
	}

	if status, err := s.getContainerStatus(c.id); err != nil {
		c.status = task.Status_UNKNOWN
	} else {
		c.status = status
	}

	return emptyResponse, nil
}
func (s *shimService) Checkpoint(context.Context, *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}

// Kill converts POSIX signals into sandbox operations and applies them to the task.
func (s *shimService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	signum := syscall.Signal(r.Signal)

	c, found := s.containers[r.ID]
	if !found {
		return nil, er.ContainerNotFound
	}

	if r.ExecID != "" {
		log.Debugf("exec processes are not supported for container %s, ignoring Kill request", r.ID)
		return emptyResponse, nil
	}

	switch signum {
	case syscall.SIGKILL, syscall.SIGTERM:
		if c.status == task.Status_STOPPED {
			log.Debugf("container %s already stopped", c.id)
			return emptyResponse, nil
		}
		if c.cType.CanBeSandbox() {
			if s.sandbox != nil {
				if err := s.sandbox.Stop(ctx, true); err != nil {
					log.Debugf("sandbox Stop returned: %v", err)
				}
				if err := s.sandbox.Delete(ctx); err != nil {
					log.Debugf("sandbox Delete returned: %v", err)
				}
				s.sandbox = nil
			} else {
				log.Debugf("Sandbox already deleted in Kill for container %s", c.id)
			}
			c.status = task.Status_STOPPED
			c.ioExit()
			return emptyResponse, nil
		}

		if s.sandbox == nil {
			log.Debugf("Sandbox is nil, cannot kill container %s", c.id)
			return nil, er.SandboxNotFound
		}

		killed, err := s.sandbox.KillContainer(ctx, c.id)
		if err != nil {
			st, err1 := s.getContainerStatus(c.id)
			log.Debugf("kill container %s failed: %v", c.id, err)
			if err1 != nil {
				c.status = task.Status_UNKNOWN
			} else {
				c.status = st
			}
			return nil, err
		}
		c.status = task.Status_UNKNOWN
		c.ioExit()
		log.Debugf("killed container %v", killed.Status())
		return emptyResponse, nil

	case syscall.SIGSTOP, syscall.SIGCONT:
		if c.status == task.Status_PAUSING || c.status == task.Status_STOPPED {
			log.Debugf("container %s pausing or stopped, can not task action", c.id)
			return emptyResponse, nil
		}
		if s.sandbox == nil {
			log.Debugf("Sandbox is nil, cannot pause container %s", c.id)
			return nil, er.SandboxNotFound
		}
		if err := s.sandbox.PauseContainer(ctx, c.id); err != nil {
			log.Debugf("sandbox pause container %s failed %v", c.id, err)
			st, err1 := s.getContainerStatus(c.id)
			if err1 != nil {
				c.status = task.Status_UNKNOWN
			} else {
				c.status = st
			}
			return nil, err
		}
	default:
		return emptyResponse, nil
	}
	return emptyResponse, nil
}
func (s *shimService) Exec(context.Context, *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}

// ResizePty resizes the PTY for a container by calling sandbox.WinResize.
func (s *shimService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Debugf("resizing PTY for container %s to %dx%d", r.ID, r.Width, r.Height)
	c, found := s.containers[r.ID]
	if !found || c == nil {
		return nil, er.ContainerNotFound
	}

	if s.sandbox == nil {
		log.Debugf("Sandbox is nil, cannot resize PTY for %s", r.ID)
		return nil, er.SandboxNotFound
	}

	if err := s.sandbox.WinResize(ctx, r.ID, r.Height, r.Width); err != nil {
		return nil, err
	}
	return emptyResponse, nil
}

// CloseIO closes the IO streams for a client OS.
func (s *shimService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ContainerNotFound
	}

	if r.ExecID != "" {
		return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "exec processes are not supported for container %s", r.ID)
	}

	if !r.Stdin {
		return emptyResponse, nil
	}

	stdinCloser := c.stdinCloser

	if c.ttyio != nil && c.ttyio.io != nil && c.ttyio.io.Stdin() != nil {
		if err := c.ttyio.io.Stdin().Close(); err != nil {
			log.Debugf("failed to drain containerd stdin reader for %s: %v", r.ID, err)
		}
	}

	<-stdinCloser

	if c.stdinPipe != nil {
		if err := c.stdinPipe.Close(); err != nil {
			log.Debugf("stdin pipe close for %s returned: %v", r.ID, err)
		}
	}

	return emptyResponse, nil
}

// Update updates container resources by calling sandbox.UpdateContainer.
func (s *shimService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if c == nil || !found {
		// Best-effort: container may already be gone; treat as success to avoid disrupting higher layers
		log.Debugf("Update ignored: container %s not found", r.ID)
		return emptyResponse, nil
	}

	// Decode resources if present; tolerate errors and proceed as no-op
	var res specs.LinuxResources
	if r.Resources != nil {
		if raw, err := typeurl.UnmarshalAny(r.Resources); err == nil {
			if lr, ok := raw.(*specs.LinuxResources); ok && lr != nil {
				res = *lr
			} else {
				log.Debugf("Update ignored: invalid resources type for %s", s.id)
			}
		} else {
			log.Debugf("Update ignored: unable to unmarshal resources for %s: %v", s.id, err)
		}
	}

	log.Debugf("Update task annotations: %v", r.Annotations)
	log.Debugf("Update task resource: %+v", res)

	if s.sandbox == nil {
		log.Debugf("Sandbox is nil, cannot update container %s", r.ID)
		return nil, er.SandboxNotFound
	}

	if err := s.sandbox.UpdateContainer(ctx, r.ID, res); err != nil {
		log.Debugf("UpdateContainer best-effort ignore error for %s: %v", r.ID, err)
	}

	return emptyResponse, nil
}
func (s *shimService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	s.mu.Lock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		s.mu.Unlock()
		return nil, er.ContainerNotFound
	}
	if r.ExecID != "" {
		s.mu.Unlock()
		return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "exec processes are not supported for container %s", r.ID)
	}

	// Capture current status and the exit channel, then release the lock while waiting
	exited := c.status == task.Status_STOPPED
	exitStatus := c.exit
	exitAt := c.exitTime
	exitIOch := c.exitIOch
	s.mu.Unlock()

	// If not already exited, wait for exit or context cancellation
	if !exited {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait canceled: %w", ctx.Err())
		case <-exitIOch:
		}
	}

	// Re-acquire lock to fetch final status/time
	s.mu.Lock()
	exitStatus = c.exit
	exitAt = c.exitTime
	s.mu.Unlock()

	return &taskAPI.WaitResponse{
		ExitStatus: exitStatus,
		ExitedAt:   timestamppb.New(exitAt),
	}, nil
}

// Stats returns container statistics by calling marshalMetrics.
func (s *shimService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return &taskAPI.StatsResponse{
			Stats: EmptyMetricsV1(),
		}, nil
	}

	data, err := marshalMetrics(ctx, s, r.ID)
	if err != nil {
		return &taskAPI.StatsResponse{
			Stats: EmptyMetricsV1(),
		}, nil
	}

	return &taskAPI.StatsResponse{
		Stats: data,
	}, nil
}
func (s *shimService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &taskAPI.ConnectResponse{
		ShimPid: shimPid,
		TaskPid: shimPid,
	}, nil
}
func (s *shimService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	if len(s.containers) != 0 {
		s.mu.Unlock()
		return emptyResponse, nil
	}

	s.mu.Unlock()
	s.ss()

	// Clean up the shim socket before exiting to prevent "address already in use" errors
	// when the shim is restarted. The socket address is stored in the "address" file.
	if sockAddr, err := shimv2.ReadAddress("address"); err == nil && sockAddr != "" {
		if err := shimv2.RemoveSocket(sockAddr); err != nil {
			log.Warnf("failed to remove shim socket %s: %v", sockAddr, err)
		}
	}

	// os.Exit() will terminate program immediately, the defer functions won't be executed,
	// so we add defer functions again before os.Exit().
	// Refer to https://pkg.go.dev/os#Exit
	os.Exit(0)

	// This will never be called, but this is only there to make sure the
	// program can compile.
	return emptyResponse, nil
}
