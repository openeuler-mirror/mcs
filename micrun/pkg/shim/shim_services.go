package shim

import (
	"context"
	"flag"
	"fmt"
	"io"
	er "micrun/errors"
	log "micrun/logger"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	cntr "micrun/pkg/micantainer"
	micrunio "micrun/pkg/io"
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
	shimPid    int
	namespace  string
	config     *oci.RuntimeConfig
	containers map[string]*shimContainer
	sandbox    cntr.SandboxTraits
	ctx        context.Context
	events     chan any
	ec         chan exitEvent
	ss         func()
	mu         sync.Mutex
	// killedByAPI indicates that the container was killed via Kill API
	// In this case, we should NOT trigger shim auto-exit
	killedByAPI bool
}

func New(ctx context.Context, id string, publisher shimv2.Publisher, shutdown func()) (shimv2.Shim, error) {
	// Detect if in one-shot command (start/delete) or daemon mode
	// - start/delete: one-shot commands that exit after completion
	// - empty string: daemon mode that runs TTRPC server
	action := flag.Arg(0)
	isOneShotCommand := action == "start" || action == "delete"

	var s *shimService

	if !isOneShotCommand {
		// Daemon mode: full initialization with all fields
		// These are needed for long-running TTRPC server
		// namespace is guaranteed to be set by shim.Run() before calling New()
		ns, _ := namespaces.Namespace(ctx)
		s = &shimService{
			id:         id,
			shimPid:    os.Getpid(),
			namespace:  ns,
			ctx:        ctx,
			ss:         shutdown,
			containers: make(map[string]*shimContainer),
		}
		s.events = make(chan any, channelSize)
		s.ec = make(chan exitEvent, channelSize)

		// Check micad is running (required for daemon mode)
		micadPid, err := getMicadPid()
		if err != nil {
			return nil, fmt.Errorf("micad is not running: %w", err)
		}
		log.Infof("[DAEMON] shimService initialized, micad PID: %d", micadPid)

		go s.listenAndReportExits()

		forwarder := s.newEventsForwarder(ctx, publisher)
		go forwarder.forward()

		// Try to restore sandbox and containers if they exist
		if err := s.restoreSandboxAndContainers(ctx); err != nil {
			log.Debugf("no existing sandbox to restore: %v", err)
			// This is expected for new containers, not an error
		}
	} else {
		// One-shot commands (start/delete): minimal initialization
		// Only the 'id' field is used by StartShim() and Cleanup()
		// All other fields remain nil/zero and are never accessed
		s = &shimService{
			id: id,
		}
		log.Infof("[ONESHOT] shimService initialized for '%s' command (one-shot, will exit after completion)", action)
	}

	return s, nil
}

// restoreSandboxAndContainers restores the sandbox and containers from disk
// if they exist from a previous shim invocation
func (s *shimService) restoreSandboxAndContainers(ctx context.Context) error {
	sandbox, err := cntr.LoadSandbox(ctx, s.id)
	if err != nil {
		return err
	}

	s.sandbox = sandbox

	// Check the actual sandbox state to determine container status
	sandboxState := sandbox.GetState()
	var initialStatus task.Status
	if sandboxState == cntr.StateRunning {
		// Sandbox is running, so containers should be marked as RUNNING
		initialStatus = task.Status_RUNNING
		log.Infof("[RESTORE] Sandbox %s is RUNNING, containers will be marked as RUNNING", s.id)
	} else {
		// Sandbox is not running yet, containers are CREATED
		initialStatus = task.Status_CREATED
		log.Infof("[RESTORE] Sandbox %s is %s, containers will be marked as CREATED", s.id, sandboxState)
	}

	// Restore containers from the sandbox
	containers := sandbox.GetAllContainers()
	for _, c := range containers {
		// Determine container type
		var cType cntr.ContainerType
		if c.GetAnnotations() != nil {
			if _, isSandbox := c.GetAnnotations()["io.kubernetes.cri.sandbox-id"]; isSandbox {
				cType = cntr.PodContainer
			} else if c.GetAnnotations()["io.kubernetes.cri.container-type"] == "sandbox" {
				cType = cntr.PodSandbox
			} else {
				cType = cntr.SingleContainer
			}
		} else {
			cType = cntr.SingleContainer
		}

		sc := &shimContainer{
			s:           s,
			id:          c.ID(),
			cType:       cType,
			status:      initialStatus,
			exitIOch:    make(chan struct{}),
			stdinCloser: make(chan struct{}),
		}
		s.containers[c.ID()] = sc
		log.Debugf("restored container %s (type: %v, status: %v)", c.ID(), cType, initialStatus)
	}

	return nil
}

func newCommand(ctx context.Context, opts shimv2.StartOpts, cwd string) (*exec.Cmd, error) {

	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get current executable path: %w", err)
	}

	var args []string
	if opts.Debug {
		args = append(args, "-debug")
	}
	// Always add -id parameter, not just in debug mode
	args = append(args, "-id", opts.ID)

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

	log.Debugf("starting daemon shim process...")
	if err := cmd.Start(); err != nil {
		_ = sock.Close()
		return "", fmt.Errorf("failed to start shim task service: %w", err)
	}
	log.Debugf("daemon shim process started with PID: %d", cmd.Process.Pid)

	runtime.UnlockOSThread()

	// BUG: sometimes micrun failed to ensure the container socket is dropped after deleted
	// result in socket leak even if containerd managed to remove the socket
	defer func() {
		if retErr != nil {
			if err := killWithBackoff(cmd.Process); err != nil {
				log.Warnf("failed to kill shim process after retries: %v", err)
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
	// update shared state (already holding lock from function entry)
	container.status = task.Status_CREATED
	s.containers[r.ID] = container

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

	log.Debugf("[CREATE] Container created (not started), returning PID=%d", pid)
	return &taskAPI.CreateTaskResponse{
		Pid: pid,
	}, nil
}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Debugf("[START METHOD] Start method called! Container ID: %s, Exec ID: %s", r.ID, r.ExecID)
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

		// Check for attach scenario: container already running
		log.Infof("[ATTACH] Checking attach scenario for %s: status=%v, attachInfo=%v, ioManager=%v",
			c.id, c.status, c.attachInfo != nil, c.ioManager != nil)
		if c.status == task.Status_RUNNING && c.attachInfo != nil {
			isRunning := false
			if c.ioManager != nil {
				isRunning = c.ioManager.IsRunning()
			}
			log.Infof("[ATTACH] IsRunning check for %s: ioManager=%v, isRunning=%v", c.id, c.ioManager != nil, isRunning)
			if c.ioManager == nil || !isRunning {
				// This is an attach scenario - restart the IO session
				log.Infof("[ATTACH] Restarting IO session for %s", c.id)

				// For attach, use current FIFO paths from the container (c.stdin, c.stdout, c.stderr)
				// These paths are set by containerd when the client calls Start API for attach
				log.Infof("[ATTACH] Current FIFO paths for %s: stdin=%q, stdout=%q, stderr=%q",
					c.id, c.stdin, c.stdout, c.stderr)
				log.Infof("[ATTACH] Saved attachInfo FIFO paths for %s: stdin=%q, stdout=%q, stderr=%q",
					c.id, c.attachInfo.Stdin, c.attachInfo.Stdout, c.attachInfo.Stderr)
				stdinFIFO := c.stdin
				stdoutFIFO := c.stdout
				stderrFIFO := c.stderr

				// If current FIFO paths are empty or invalid, try to use saved valid ones
				// Only use saved paths if they are valid (not binary:// URLs)
				if !micrunio.IsValidFIFOPath(stdinFIFO) && c.attachInfo.Stdin != "" && micrunio.IsValidFIFOPath(c.attachInfo.Stdin) {
					stdinFIFO = c.attachInfo.Stdin
				}
				if !micrunio.IsValidFIFOPath(stdoutFIFO) && c.attachInfo.Stdout != "" && micrunio.IsValidFIFOPath(c.attachInfo.Stdout) {
					stdoutFIFO = c.attachInfo.Stdout
				}
				if !micrunio.IsValidFIFOPath(stderrFIFO) && c.attachInfo.Stderr != "" && micrunio.IsValidFIFOPath(c.attachInfo.Stderr) {
					stderrFIFO = c.attachInfo.Stderr
				}

				// If still no valid FIFO paths, generate standard containerd FIFO paths
				// This handles the case where container was started in detached mode
				if !micrunio.IsValidFIFOPath(stdinFIFO) && !micrunio.IsValidFIFOPath(stdoutFIFO) && !micrunio.IsValidFIFOPath(stderrFIFO) {
					log.Infof("[ATTACH] No valid FIFO paths provided, generating standard paths for %s", c.id)
					// Generate standard paths: /run/containerd/io.containerd.runtime.v2.task/<ns>/<id>/<stream>
					if c.terminal {
						// For TTY mode, we need stdin and stdout
						stdinFIFO = micrunio.GenerateStandardFIFOPath(s.namespace, c.id, "stdin")
						stdoutFIFO = micrunio.GenerateStandardFIFOPath(s.namespace, c.id, "stdout")
					} else {
						// For non-TTY mode, we need stdout (and stderr if available)
						stdoutFIFO = micrunio.GenerateStandardFIFOPath(s.namespace, c.id, "stdout")
						if c.attachInfo != nil && c.attachInfo.TTYErr != nil {
							stderrFIFO = micrunio.GenerateStandardFIFOPath(s.namespace, c.id, "stderr")
						}
					}
					log.Infof("[ATTACH] Generated FIFO paths for %s: stdin=%q, stdout=%q, stderr=%q",
						c.id, stdinFIFO, stdoutFIFO, stderrFIFO)
				}

				log.Infof("[ATTACH] FIFO paths for %s: stdin=%q, stdout=%q, stderr=%q",
					c.id, stdinFIFO, stdoutFIFO, stderrFIFO)

				if c.ioManager == nil {
					// Create new IO session with FIFO paths and saved TTY handles
					log.Infof("[ATTACH] Creating new IO session wrapper for %s", c.id)
					config := micrunio.Config{
						ContainerID: c.id,
						StdinFIFO:   stdinFIFO,
						StdoutFIFO:  stdoutFIFO,
						StderrFIFO:  stderrFIFO,
						TTYIn:       c.attachInfo.TTYIn,
						TTYOut:      c.attachInfo.TTYOut,
						TTYErr:      c.attachInfo.TTYErr,
						Terminal:    c.attachInfo.Terminal,
						FilterNUL:   true,
					}
					session, err := micrunio.NewSession(config)
					if err != nil {
						log.Warnf("[ATTACH] Failed to create new session for %s: %v", c.id, err)
						return nil, errdefs.ToGRPCf(errdefs.ErrFailedPrecondition, "failed to create session: %v", err)
					}
					wrapper := &ioSessionWrapper{
						session:  session,
						svc:      s,
						container: c,
					}
					c.ioManager = wrapper
				}
				// Restart IO session
				if err := c.ioManager.Restart(); err != nil {
					log.Warnf("[ATTACH] Failed to restart IO session for %s: %v", c.id, err)
					return nil, errdefs.ToGRPCf(errdefs.ErrFailedPrecondition, "failed to restart IO: %v", err)
				}
				log.Infof("[ATTACH] Successfully restarted IO session for %s", c.id)
			}
			// Container already running, just return success
			if c.pid != 0 {
				respPid = c.pid
			}
		} else {
			// Normal start - start the container and IO session
			err := startContainer(ctx, s, c)
			if err != nil {
				return nil, errdefs.ToGRPC(err)
			}
			if c.pid != 0 {
				respPid = c.pid
			}
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
	log.Infof("[DELETE] Deleting container %s (current status: %s)", r.ID, func() task.Status {
		if c, ok := s.containers[r.ID]; ok {
			return c.status
		}
		return task.Status_UNKNOWN
	}())

	c, found := s.containers[r.ID]
	if c == nil || !found {
		log.Debugf("[DELETE] Container %s not found (idempotent delete)", r.ID)
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

	// If container is still RUNNING or CREATED, it means ctr closed stdin (e.g., user pressed Ctrl+C)
	// but shim hasn't updated status yet due to race condition. Set status to STOPPED here
	// to allow Delete() to succeed. This handles the "ctr task start + Ctrl+C" scenario.
	if c.status == task.Status_RUNNING || c.status == task.Status_CREATED {
		log.Infof("[DELETE] Container %s is %s, setting to STOPPED for deletion", r.ID, c.status)
		c.setStatus(task.Status_STOPPED)
		c.exit = 130 // 128 + SIGINT (standard exit code for Ctrl+C)
		c.exitTime = time.Now()
		// Trigger cleanup if not already triggered
		select {
		case <-c.exitIOch:
			// Already triggered
		default:
			close(c.exitIOch)
		}
	}

	if r.ExecID != "" {
		return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "exec processes are not supported for container %s", r.ID)
	}

	// delete single container or entire sandbox
	if c.cType.CanBeSandbox() {
		if s.sandbox == nil {
			log.Debugf("[DELETE] Sandbox already deleted for container %s", c.id)
		} else {
			sandboxID := s.sandbox.SandboxID()
			log.Infof("[DELETE] Stopping sandbox %s for container %s", sandboxID, c.id)

			if err := s.sandbox.Stop(ctx, true); err != nil {
				log.Debugf("[DELETE] Stop sandbox %s returned: %v", sandboxID, err)
			}
			log.Infof("[DELETE] Deleting sandbox %s for container %s", sandboxID, c.id)
			if err := s.sandbox.Delete(ctx); err != nil {
				log.Debugf("[DELETE] Delete sandbox %s returned: %v", sandboxID, err)
			}
			s.sandbox = nil
			log.Infof("[DELETE] Sandbox %s deleted successfully for container %s", sandboxID, c.id)
		}
	}

	// Delete the container (handles pod containers, unmount, registry cleanup)
	log.Infof("[DELETE] Calling deleteContainer for %s", c.id)
	if err := deleteContainer(ctx, s, c); err != nil {
		log.Errorf("[DELETE] Failed to delete container %s: %v", c.id, err)
		return nil, err
	}

	pid := c.pid
	if pid == 0 {
		pid = shimPid
	}

	log.Infof("[DELETE] Container %s deleted successfully (exit status: %d)", c.id, c.exit)
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

	c.setStatus(task.Status_PAUSING)
	if s.sandbox == nil {
		log.Debugf("Sandbox is nil, cannot pause container %s", r.ID)
		return nil, er.SandboxNotFound
	}

	err := s.sandbox.PauseContainer(ctx, r.ID)
	if err == nil {
		c.setStatus(task.Status_PAUSED)
		s.send(&events.TaskPaused{
			ContainerID: c.id,
		})
		return emptyResponse, nil
	}

	status, err := s.getContainerStatus(c.id)
	if err != nil {
		log.Debugf("container %s status query failed: %v", r.ID, err)
		c.setStatus(task.Status_UNKNOWN)
	} else {
		log.Debugf("container %s status: %s", r.ID, status)
		c.setStatus(status)
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
		c.setStatus(task.Status_RUNNING)
		s.send(&events.TaskResumed{
			ContainerID: c.id,
		})
		return emptyResponse, nil
	}

	if status, err := s.getContainerStatus(c.id); err != nil {
		c.setStatus(task.Status_UNKNOWN)
	} else {
		c.setStatus(status)
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

		// NOTE: Attach disconnect detection removed because it interfered with
		// explicit kill commands (ctr task kill). Kill API should always stop
		// the container when explicitly requested by the user.

		if c.cType.CanBeSandbox() {
			if s.sandbox != nil {
				log.Infof("[KILL] Stopping sandbox for container %s", c.id)
				if err := s.sandbox.Stop(ctx, true); err != nil {
					log.Debugf("sandbox Stop returned: %v", err)
				}
				log.Infof("[KILL] Deleting sandbox for container %s", c.id)
				if err := s.sandbox.Delete(ctx); err != nil {
					log.Debugf("sandbox Delete returned: %v", err)
				}
				s.sandbox = nil
			} else {
				log.Debugf("[KILL] Sandbox already deleted for container %s", c.id)
			}
			c.setStatus(task.Status_STOPPED)
			// Mark as killed by API to prevent shim auto-exit
			// Only natural exits (timeout, exit command) should trigger shim exit
			s.killedByAPI = true
			c.ioExit()
			// Brief pause to allow state to propagate
			// This helps prevent "task must be stopped before deletion" errors
			// when Delete is called immediately after Kill
			time.Sleep(10 * time.Millisecond)
			log.Infof("[KILL] Container %s stopped successfully", c.id)
			return emptyResponse, nil
		}

		if s.sandbox == nil {
			log.Warnf("[KILL] Sandbox is nil for container %s (may have been cleaned up)", c.id)
			return nil, er.SandboxNotFound
		}

		log.Infof("[KILL] Killing pod container %s", c.id)
		_, err := s.sandbox.KillContainer(ctx, c.id)
		if err != nil {
			st, err1 := s.getContainerStatus(c.id)
			log.Warnf("[KILL] Failed to kill pod container %s: %v", c.id, err)
			if err1 != nil {
				c.setStatus(task.Status_UNKNOWN)
			} else {
				c.setStatus(st)
			}
			return nil, err
		}
		c.setStatus(task.Status_STOPPED)
		// Mark as killed by API to prevent shim auto-exit
		// Only natural exits (timeout, exit command) should trigger shim exit
		s.killedByAPI = true
		c.ioExit()
		log.Infof("[KILL] Pod container %s killed successfully", c.id)
		return emptyResponse, nil

	case syscall.SIGSTOP:
		if c.status == task.Status_PAUSING || c.status == task.Status_STOPPED {
			log.Debugf("container %s pausing or stopped, cannot pause again", c.id)
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
				c.setStatus(task.Status_UNKNOWN)
			} else {
				c.setStatus(st)
			}
			return nil, err
		}
		log.Debugf("container %s paused successfully", c.id)

	case syscall.SIGCONT:
		if c.status == task.Status_RUNNING {
			log.Debugf("container %s already running, ignoring SIGCONT", c.id)
			return emptyResponse, nil
		}
		if s.sandbox == nil {
			log.Debugf("Sandbox is nil, cannot resume container %s", c.id)
			return nil, er.SandboxNotFound
		}
		if err := s.sandbox.ResumeContainer(ctx, c.id); err != nil {
			log.Debugf("sandbox resume container %s failed %v", c.id, err)
			st, err1 := s.getContainerStatus(c.id)
			if err1 != nil {
				c.setStatus(task.Status_UNKNOWN)
			} else {
				c.setStatus(st)
			}
			return nil, err
		}
		log.Debugf("container %s resumed successfully via SIGCONT", c.id)
	default:
		return emptyResponse, nil
	}
	return emptyResponse, nil
}
func (s *shimService) Exec(context.Context, *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}

// ResizePty resizes the PTY for a container by calling sandbox.WinResize.
// This is called in two scenarios:
// 1. During initial container start (nerdctl run -it) to set up PTY size
// 2. During attach (nerdctl attach, ctr task attach) to set up PTY size for reconnection
//
// NOTE: ResizePty is a PTY size adjustment operation, NOT an attach operation.
// It should be allowed regardless of the current attach state.
func (s *shimService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Debugf("[ATTACH] ResizePty called for container %s to %dx%d (mode: %s)", r.ID, r.Width, r.Height, func() string {
		if c, ok := s.containers[r.ID]; ok {
			return c.ioMode.String()
		}
		return "unknown"
	}())
	c, found := s.containers[r.ID]
	if !found || c == nil {
		return nil, er.ContainerNotFound
	}

	if s.sandbox == nil {
		log.Debugf("[ATTACH] Sandbox is nil for %s, cannot resize PTY", r.ID)
		return nil, er.SandboxNotFound
	}

	// Check if IO session needs to be restarted (for attach scenario)
	//
	// Attach scenarios:
	// 1. User detaches (Ctrl+P Ctrl+Q or "back" command) and then reattaches
	// 2. User starts container in background (-d flag) and then attaches
	// 3. IO session has stopped but container is still running
	//
	// Detection: Non-zero width/height in ResizePty indicates real attach
	// (initial calls during container start have width=0, height=0)
	isRealAttach := (r.Width > 0 || r.Height > 0)
	needsRestart := false

	if c.status == task.Status_RUNNING && c.attachInfo != nil {
		isRunning := false
		if c.ioManager != nil {
			isRunning = c.ioManager.IsRunning()
		}
		log.Infof("[ATTACH] IsRunning check (ResizePty) for %s: ioManager=%v, isRunning=%v, isRealAttach=%v",
			r.ID, c.ioManager != nil, isRunning, isRealAttach)

		// Restart if:
		// 1. IO session is not running
		// 2. This is a real attach (non-zero dimensions) and container supports attach
		needsRestart = (c.ioManager == nil || !isRunning)

		// For non-TTY background mode, always restart on attach
		// This ensures stdin FIFO is properly reopened for the attach client
		if !needsRestart && isRealAttach && !c.ioMode.IsTTY && !c.ioMode.IsForeground {
			log.Infof("[ATTACH] Non-TTY background attach detected for %s, restarting IO session", r.ID)
			needsRestart = true
		}

		if needsRestart {
			log.Infof("[ATTACH] IO session not running for %s, restarting for attach", r.ID)

			// IMPORTANT: Open fresh TTY handles BEFORE creating the session
			// This ensures the copier starts with valid TTY file descriptors
			// instead of using stale closed ones from attachInfo.
			ttyIn, ttyOut, ttyErr := s.sandbox.OpenTTYs(ctx, r.ID)
			if ttyErr != nil {
				log.Warnf("[ATTACH] Failed to open fresh TTY for %s: %v", r.ID, ttyErr)
				// Continue anyway - will try to use old handles from attachInfo
				ttyIn = nil
				ttyOut = nil
			} else {
				log.Infof("[ATTACH] Opened fresh TTY handles for %s before session creation", r.ID)
			}

			// Create new IO session if needed
			if c.ioManager == nil {
				log.Infof("[ATTACH] Creating new IO session for %s", r.ID)
				// Use freshly opened TTY if successful, otherwise fall back to attachInfo
				var configTTYIn io.WriteCloser
				var configTTYOut io.Reader
				if ttyIn != nil {
					configTTYIn = ttyIn
					configTTYOut = ttyOut
				} else {
					configTTYIn = c.attachInfo.TTYIn
					configTTYOut = c.attachInfo.TTYOut
				}
				config := micrunio.Config{
					ContainerID: c.id,
					StdinFIFO:   c.attachInfo.Stdin,
					StdoutFIFO:  c.attachInfo.Stdout,
					StderrFIFO:  c.attachInfo.Stderr,
					TTYIn:       configTTYIn,
					TTYOut:      configTTYOut,
					TTYErr:      configTTYOut, // TTYErr is same as TTYOut for RPMSG TTY
					Terminal:    c.attachInfo.Terminal,
					FilterNUL:   true,
				}
				session, err := micrunio.NewSession(config)
				if err != nil {
					log.Warnf("[ATTACH] Failed to create session for %s: %v", r.ID, err)
					// Continue anyway - TTY resize might still work
				} else {
					// Start the session (open FIFOs, start copier)
					if err := session.Start(); err != nil {
						log.Warnf("[ATTACH] Failed to start session for %s: %v", r.ID, err)
					} else {
						wrapper := &ioSessionWrapper{
							session:   session,
							svc:       s,
							container: c,
						}
						c.ioManager = wrapper
						log.Infof("[ATTACH] IO session created and started for %s with fresh TTY", r.ID)
					}
				}
			} else {
				// Restart existing IO manager with fresh TTY handles
				// This ensures the copier goroutines use the correct file descriptors
				if wrapper, ok := c.ioManager.(*ioSessionWrapper); ok {
					if err := wrapper.RestartWithTTYs(ttyIn, ttyOut); err != nil {
						log.Warnf("[ATTACH] Failed to restart IO manager for %s: %v", r.ID, err)
					} else {
						log.Infof("[ATTACH] IO manager restarted for %s with fresh TTY handles", r.ID)
					}
				} else {
					// Fallback: try regular restart (will use old TTY handles from config)
					if err := c.ioManager.Restart(); err != nil {
						log.Warnf("[ATTACH] Failed to restart IO manager for %s: %v", r.ID, err)
					} else {
						log.Infof("[ATTACH] IO manager restarted for %s", r.ID)
					}
				}
			}
		}
	}

	// Mark as attached - this allows subsequent CloseIO to clear the state
	c.isAttached = true
	log.Infof("[ATTACH] Container %s marked as attached", r.ID)

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

	// Clear attached state to allow subsequent attach
	c.attachLock.Lock()
	wasAttached := c.isAttached
	c.isAttached = false
	c.attachLock.Unlock()
	if wasAttached {
		log.Infof("[ATTACH] Container %s detached, isAttached cleared", r.ID)
	}

	if !r.Stdin {
		return emptyResponse, nil
	}

	stdinCloser := c.stdinCloser

	// Close stdin pipe to signal IO copier
	if c.stdinPipe != nil {
		if err := c.stdinPipe.Close(); err != nil {
			log.Debugf("stdin pipe close for %s returned: %v", r.ID, err)
		}
	}

	<-stdinCloser

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
			Stats: emptyMetricsV1(),
		}, nil
	}

	data, err := marshalMetrics(ctx, s, r.ID)
	if err != nil {
		return &taskAPI.StatsResponse{
			Stats: emptyMetricsV1(),
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

// killWithBackoff attempts to kill a process with exponential backoff retry.
// Wait times: 100ms, 200ms, 400ms, 800ms, 1000ms (capped)
// Total max wait: ~2.5 seconds
func killWithBackoff(proc *os.Process) error {
	const maxAttempts = 5
	const baseWait = 100 * time.Millisecond
	const maxWait = time.Second

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := proc.Kill()
		if err == nil {
			if attempt > 0 {
				log.Debugf("kill succeeded on attempt %d", attempt+1)
			}
			return nil
		}
		lastErr = err

		// Calculate wait time with exponential backoff
		wait := baseWait * time.Duration(1<<uint(attempt))
		if wait > maxWait {
			wait = maxWait
		}

		log.Debugf("kill attempt %d failed, waiting %v before retry: %v", attempt+1, wait, err)
		time.Sleep(wait)
	}

	return fmt.Errorf("kill failed after %d attempts: %w", maxAttempts, lastErr)
}
