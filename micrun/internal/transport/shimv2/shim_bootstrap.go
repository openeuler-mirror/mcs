package shim

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	micrunio "micrun/internal/adapters/io"
	appruntime "micrun/internal/application/runtime"
	cntr "micrun/internal/domain/container"
	ports "micrun/internal/ports"
	"micrun/internal/support/contextx"
	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

func New(ctx context.Context, id string, publisher shimv2.Publisher, shutdown func()) (shimv2.Shim, error) {
	bindings, err := detectRuntimeEnvironment(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to bootstrap platform bindings: %w", err)
	}

	containerDeps := buildContainerDependencies(bindings)
	if err := containerDeps.Validate(); err != nil {
		return nil, fmt.Errorf("invalid container dependencies: %w", err)
	}
	runtimeDeps := buildRuntimeDependencies(bindings, containerDeps)

	action := flag.Arg(0)
	if isOneShotAction(action) {
		return newOneShotShimService(id, runtimeDeps, action), nil
	}

	services, err := appruntime.NewServicesChecked(appruntime.Options{IOFactory: micrunio.NewFactory()})
	if err != nil {
		return nil, fmt.Errorf("invalid application services: %w", err)
	}
	service, err := newDaemonShimService(ctx, id, shutdown, services.Task(), runtimeDeps)
	if err != nil {
		return nil, err
	}
	if err := initializeDaemonMode(ctx, service, publisher, services.Recovery()); err != nil {
		return nil, err
	}

	return service, nil
}

func (s *shimService) RuntimeID() string {
	return s.id
}

func (s *shimService) makeRecoveredTask(spec ports.RecoveredTask) ports.Task {
	cType := recoveredContainerType(spec)
	return &shimContainer{
		s:           s,
		id:          spec.ID,
		cType:       cType,
		exitIOch:    make(chan struct{}),
		stdinCloser: make(chan struct{}),
	}
}

func recoveredContainerType(spec ports.RecoveredTask) cntr.ContainerType {
	switch {
	case spec.IsSandbox:
		return cntr.PodSandbox
	case !spec.CanSandbox:
		return cntr.PodContainer
	default:
		return cntr.SingleContainer
	}
}

func newCommand(ctx context.Context, opts shimv2.StartOpts, cwd string) (*exec.Cmd, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get current executable path: %w", err)
	}

	var args []string
	if opts.Debug {
		args = append(args, "-debug")
	}
	args = append(args, "-id", opts.ID)

	cmdCfg := &shimv2.CommandConfig{
		Runtime:      self,
		Address:      opts.Address,
		TTRPCAddress: opts.TTRPCAddress,
		Path:         cwd,
		SchedCore:    os.Getenv(contdShimEnvSchedCore) != "",
		Args:         args,
	}

	cmd, err := shimv2.Command(ctx, cmdCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create shim command: %w", err)
	}
	if logColor := os.Getenv("LOG_COLOR"); logColor != "" {
		cmd.Env = append(cmd.Env, "LOG_COLOR="+logColor)
	}
	return cmd, nil
}

func (s *shimService) StartShim(ctx context.Context, opts shimv2.StartOpts) (_ string, retErr error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
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
	if sockaddr != "" {
		if err := shimv2.WriteAddress("address", sockaddr); err != nil {
			return "", fmt.Errorf("failed to write socket address for pod container: %w", err)
		}
		return sockaddr, nil
	}

	log.Tracef("args: %v", os.Args)
	cmd, err := newCommand(ctx, opts, bundle)
	if err != nil {
		return "", err
	}

	sockAddr, socket, connected, err := ensureShimSocket(ctx, opts)
	if err != nil {
		return "", err
	}
	if connected {
		return sockAddr, nil
	}
	defer func() {
		if retErr != nil {
			socket.Close()
			if err := shimv2.RemoveSocket(sockAddr); err != nil {
				log.Tracef("failed to remove socket %s: %v", sockAddr, err)
			}
		}
	}()

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

	log.Tracef("starting daemon shim process...")
	if err := cmd.Start(); err != nil {
		runtime.UnlockOSThread()
		return "", fmt.Errorf("failed to start shim task service: %w", closeShimExtraFileAfterStartFailure(err, sock))
	}
	if err := closeShimExtraFile(sock); err != nil {
		log.Warnf("failed to close shim socket file after start: %v", err)
	}
	log.Tracef("daemon shim process started with PID: %d", cmd.Process.Pid)
	runtime.UnlockOSThread()

	defer func() {
		if retErr != nil {
			if err := killWithBackoff(cmd.Process); err != nil {
				log.Warnf("failed to kill shim process after retries: %v", err)
			}
		}
	}()

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Tracef("shim daemon process wait failed: %v", err)
		}
	}()

	if err = shimv2.WritePidFile("shim.pid", cmd.Process.Pid); err != nil {
		return "", fmt.Errorf("failed to write shim PID file: %w", err)
	}
	if err = shimv2.WriteAddress("address", sockAddr); err != nil {
		return "", err
	}
	if err = setupStateDir(defs.MicrunStateDir); err != nil {
		log.Warnf("failed to setup micrun state directory: %v", err)
	}

	return sockAddr, nil
}

func closeShimExtraFileAfterStartFailure(startErr error, file io.Closer) error {
	if closeErr := closeShimExtraFile(file); closeErr != nil {
		return errors.Join(startErr, closeErr)
	}
	return startErr
}

func closeShimExtraFile(file io.Closer) error {
	if file == nil {
		return nil
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close shim socket file: %w", err)
	}
	return nil
}

func ensureShimSocket(ctx context.Context, opts shimv2.StartOpts) (string, *net.UnixListener, bool, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return "", nil, false, err
	}
	sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, opts.ID)
	if err != nil {
		return "", nil, false, err
	}

	socket, err := shimv2.NewSocket(sockAddr)
	if err == nil {
		return sockAddr, socket, false, nil
	}
	if !shimv2.SocketEaddrinuse(err) {
		return "", nil, false, fmt.Errorf("create new shim socket: %w", err)
	}
	if shimv2.CanConnect(sockAddr) {
		if err := shimv2.WriteAddress("address", sockAddr); err != nil {
			return "", nil, false, fmt.Errorf("write existing socket for shim: %w", err)
		}
		return sockAddr, nil, true, nil
	}

	log.Tracef("removing stale socket and creating new one")
	if err := shimv2.RemoveSocket(sockAddr); err != nil {
		return "", nil, false, fmt.Errorf("remove pre-existing socket: %w", err)
	}

	socket, err = shimv2.NewSocket(sockAddr)
	if err != nil {
		return "", nil, false, fmt.Errorf("try create new shim socket 2x: %w", err)
	}
	return sockAddr, socket, false, nil
}

// killWithBackoff attempts to kill a process with exponential backoff retry.
func killWithBackoff(proc *os.Process) error {
	if proc == nil {
		return fmt.Errorf("process is nil")
	}
	return killWithBackoffFunc(proc.Kill, time.Sleep)
}

func killWithBackoffFunc(kill func() error, sleep func(time.Duration)) error {
	const maxAttempts = 5
	const baseWait = 100 * time.Millisecond
	const maxWait = time.Second

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := kill()
		if err == nil {
			if attempt > 0 {
				log.Tracef("kill succeeded on attempt %d", attempt+1)
			}
			return nil
		}
		lastErr = err
		if attempt == maxAttempts-1 {
			break
		}

		wait := baseWait * time.Duration(1<<uint(attempt))
		if wait > maxWait {
			wait = maxWait
		}

		log.Tracef("kill attempt %d failed, waiting %v before retry: %v", attempt+1, wait, err)
		sleep(wait)
	}

	return fmt.Errorf("kill failed after %d attempts: %w", maxAttempts, lastErr)
}
