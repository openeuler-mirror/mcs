package micantainer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	defs "micrun/definitions"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/cpuset"
	"micrun/pkg/libmica"
	"micrun/pkg/netns"
	ped "micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

// Container represents a single container instance, encapsulating its configuration,
// state, and relationship with a sandbox.
type Container struct {
	ctx            context.Context
	id             string
	me             libmica.MicaExecutor
	config         *ContainerConfig
	sandbox        *Sandbox
	mounts         []Mount
	rootfs         RootFs
	containerPath  string // The path relative to the root bundle: <bundleRoot>/<sandboxID>/<containerID>.
	state          ContainerState
	infraCmd       *exec.Cmd
	infraExitCh    chan helperCh
	exitNotifier   chan struct{}
	exitNotifierMu sync.Mutex
}

type ContainerConfig struct {
	ID             string
	Rootfs         RootFs
	Mount          []Mount
	ReadOnlyRootfs bool
	IsInfra        bool
	Pid            int // Pid is typically the shim pid.
	Annotations    map[string]string
	Resources      *specs.LinuxResources

	// ImageAbsPath is the absolute path of the <RTOS> image in the host required by mica
	ImageAbsPath string      `json:"elf_abs_path"`
	PedestalType ped.PedType `json:"pedestal_type"`
	PedestalConf string      `json:"pedestal_conf"`
	OS           string      `json:"os"`

	// VCPUNum is the number of virtual CPUs. Matches the configured CPU capacity when not pinning; otherwise, equals the size of the cpuset.
	VCPUNum uint32 `json:"vcpu_num"`
	// PCPUNum is the number of allocated physical CPUs.
	// TODO: Implement for openAMP and Jailhouse cases.
	PCPUNum int `json:"ncpu"`
	// MaxVcpuNum is the pedestal max virtual CPUs configured for this container.
	MaxVcpuNum uint32 `json:"max_vcpu_num"`

	// MemoryThresholdMB is the pedestal maximum allocable memory in MiB.
	MemoryThresholdMB uint32 `json:"memory_threshold"`

	// Cmdline is the boot command line for the guest.
	// TODO: consider passing the cmdline as a parameter to the pty, acting as if we "execute" command
	Cmdline string `json:"cmdline"`
}

// Noop writer/reader are used for infra container which never has PTY or IO.
type noopWriteCloser struct{}

func (noopWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (noopWriteCloser) Close() error {
	return nil
}

type helperCh struct {
	code int
	err  error
}

// newContainer creates a new container struct instance.
// It assumes that the container config is already parsed.
func newContainer(ctx context.Context, s *Sandbox, cc *ContainerConfig) (*Container, error) {
	if cc == nil {
		return &Container{}, fmt.Errorf("container config is none")
	}

	if cc.ID == "" {
		log.Debugf("Empty container id.")
		return &Container{}, er.EmptyContainerID
	}

	c := &Container{
		id:            cc.ID,
		me:            libmica.MicaExecutor{Id: cc.ID},
		sandbox:       s,
		config:        cc,
		rootfs:        cc.Rootfs,
		containerPath: filepath.Join(s.id, cc.ID),
		mounts:        cc.Mount,
		state:         ContainerState{State: StateDown},
		ctx:           s.ctx,
	}

	if err := c.RestoreState(); err != nil {
		log.Warnf("Failed to restore container state: %v.", err)
	}

	c.updateExitNotifier(c.checkState())

	return c, nil
}

// CleanupContainer stops and deletes a container and its associated sandbox if it's the last one.
// NOTICE: This function is designed for exclusive cleanup operations.
func CleanupContainer(ctx context.Context, sandboxID string, containerID string, force bool) error {
	log.Debugf("Cleaning up sandbox %s, container %s.", sandboxID, containerID)
	if sandboxID == "" {
		return er.EmptySandboxID
	}

	if containerID == "" {
		return er.EmptyContainerID
	}

	sandbox, err := loadSandbox(ctx, sandboxID)
	if err != nil {
		if err == er.SandboxNotFound {
			if !libmica.ClientNotExist(containerID) && !force {
				return fmt.Errorf("sandbox state missing while client %s still exists", containerID)
			}
			log.Debugf("Sandbox %s already removed from disk, skipping container %s cleanup.", sandboxID, containerID)
			return nil
		}
		return err
	}

	if _, err = sandbox.StopContainer(ctx, containerID, force); err != nil {
		if err != er.ContainerNotFound && !force {
			return err
		}
		log.Debugf("Container %s already stopped or absent in sandbox %s: %v.", containerID, sandboxID, err)
	}

	if _, err = sandbox.DeleteContainer(ctx, containerID); err != nil {
		if err != er.ContainerNotFound && !force {
			return err
		}
		log.Debugf("Container %s already deleted from sandbox %s: %v.", containerID, sandboxID, err)
	}

	if len(sandbox.containers) > 0 {
		return nil
	}

	if err = sandbox.Stop(ctx, force); err != nil && !force {
		return err
	}

	if err = sandbox.Delete(ctx); err != nil {
		return err
	}

	return nil
}

// start begins the execution of the container.
func (c *Container) start(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}

	if c.config != nil && c.config.IsInfra {
		if currentState == StateRunning {
			return nil
		}
		if currentState != StateReady && currentState != StateStopped {
			return fmt.Errorf("container is not ready or stopped, cannot start")
		}
		if err := c.state.Transition(currentState, StateRunning); err != nil {
			return err
		}
		return c.setContainerState(ctx, StateRunning)
	}

	if currentState == StateRunning {
		return fmt.Errorf("container %s is already running", c.id)
	}

	if currentState != StateReady && currentState != StateStopped {
		return fmt.Errorf("container is not ready or stopped, cannot start")
	}

	if err := c.state.Transition(currentState, StateRunning); err != nil {
		return err
	}

	if err := startClient(ctx, c.sandbox, c); err != nil {
		log.Warnf("Failed to start container: %v, stopping it", err)
		if err := c.stop(ctx, true); err != nil {
			log.Warn("Failed to stop the container after start failed.")
		}
	}

	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) startInfraProcess(ctx context.Context) error {
	if c.infraCmd != nil {
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get cwd: %v", err)
	}

	bundle, err := utils.ValidBundle(c.id, cwd)
	if err != nil {
		return err
	}

	spec, err := loadSpecFromBundle(bundle)
	if err != nil {
		return fmt.Errorf("failed to load sandbox spec: %w", err)
	}

	nsenterPath, err := exec.LookPath("nsenter")
	if err != nil {
		return fmt.Errorf("nsenter not found, unable to join netnamespace: %w", err)
	}

	rootfs := filepath.Join(bundle, "rootfs")
	if c.config.Rootfs.Target != "" {
		rootfs = c.config.Rootfs.Target
	}

	netPath := ""
	if c.sandbox != nil && c.sandbox.config != nil {
		netPath = c.sandbox.config.NetworkConfig.NetworkID
	}

	args, err := genNsenterArgs(spec, rootfs, netPath)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, nsenterPath, args...)

	env := assembleHelperEnv(spec)
	cmd.Env = env

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:    true,
		Pdeathsig: syscall.SIGKILL,
	}

	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open /dev/null: %w", err)
	}
	defer devNull.Close()

	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start sandbox pause helper: %w", err)
	}

	c.infraCmd = cmd
	c.infraExitCh = make(chan helperCh, 1)

	go c.monitorInfraExit(cmd)

	c.config.Pid = cmd.Process.Pid
	if c.sandbox != nil && c.sandbox.config != nil {
		prev := c.sandbox.config.NetworkConfig.HolderPid
		c.sandbox.config.NetworkConfig.HolderPid = cmd.Process.Pid
		if c.sandbox.config.NetworkConfig.NetworkCreated && prev > 0 && prev != cmd.Process.Pid {
			if err := netns.Cleanup(c.sandbox.id, prev); err != nil && !errors.Is(err, os.ErrProcessDone) {
				log.Warnf("failed to cleanup previous netns holder %d: %v", prev, err)
			}
		}
	}

	return nil
}

func (c *Container) monitorInfraExit(cmd *exec.Cmd) {
	err := cmd.Wait()
	exitCode := extractExitCode(err)
	if c.infraExitCh != nil {
		c.infraExitCh <- helperCh{code: exitCode, err: nil}
		close(c.infraExitCh)
	}
	c.infraCmd = nil
	c.infraExitCh = nil
	if c.config != nil {
		c.config.Pid = 0
	}
	if c.sandbox != nil && c.sandbox.config != nil {
		c.sandbox.config.NetworkConfig.HolderPid = 0
	}
}

func (c *Container) ioStream(taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	_ = taskID
	if c.config != nil && c.config.IsInfra {
		return noopWriteCloser{}, bytes.NewReader(nil), bytes.NewReader(nil), nil
	}

	stdin, stdout, _, err := dialTTY(c.ctx, c.id)
	if err != nil {
		return nil, nil, nil, err
	}

	// dialTTY now returns the SAME file descriptor for both stdin and stdout
	// This prevents PTY buffer corruption from dual-fd contention
	// stdin and stdout both point to the same os.File object
	// Wrap stdout and stderr with noCloseFile to prevent double-close issues
	// since stdin and stdout share the same underlying fd
	// stdout/stderr are permanently merged for mica clients.
	return stdin, &noCloseFile{stdout}, &noCloseFile{stdout}, nil
}

// noCloseFile wraps an os.File to prevent actual closure
// When Close() is called, it does nothing, keeping the underlying file open
type noCloseFile struct {
	*os.File
}

func (f *noCloseFile) Close() error {
	// Don't close the underlying TTY file descriptor
	// This prevents the stdout copy goroutine from closing stdin's fd
	return nil
}

func extractExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}

	return 255
}

func genNsenterArgs(spec specs.Spec, rootfs, fallbackNetPath string) ([]string, error) {
	if spec.Process == nil || len(spec.Process.Args) == 0 {
		return nil, fmt.Errorf("invalid sandbox process definition")
	}

	args := make([]string, 0)
	nsSeen := make(map[specs.LinuxNamespaceType]struct{})
	if spec.Linux != nil {
		for _, ns := range spec.Linux.Namespaces {
			if ns.Path == "" {
				continue
			}
			switch ns.Type {
			case specs.NetworkNamespace:
				args = append(args, "--net="+ns.Path)
				nsSeen[specs.NetworkNamespace] = struct{}{}
			case specs.IPCNamespace:
				args = append(args, "--ipc="+ns.Path)
			case specs.UTSNamespace:
				args = append(args, "--uts="+ns.Path)
			case specs.PIDNamespace:
				args = append(args, "--pid="+ns.Path)
			case specs.UserNamespace:
				args = append(args, "--user="+ns.Path)
			case specs.MountNamespace:
				args = append(args, "--mount="+ns.Path)
			}
		}
	}

	if fallbackNetPath != "" {
		if _, ok := nsSeen[specs.NetworkNamespace]; !ok {
			args = append(args, "--net="+fallbackNetPath)
		}
	}

	if rootfs != "" {
		args = append(args, "--root="+rootfs)
	}
	if spec.Process.Cwd != "" {
		args = append(args, "--wd="+spec.Process.Cwd)
	}

	args = append(args, "--")
	args = append(args, spec.Process.Args...)
	return args, nil
}

func assembleHelperEnv(spec specs.Spec) []string {
	env := append([]string{}, os.Environ()...)

	if spec.Process != nil && len(spec.Process.Env) > 0 {
		env = mergeEnv(env, spec.Process.Env)
	}

	if !envHasKey(env, "PATH") {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return env
}

func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func mergeEnv(base, override []string) []string {
	result := append([]string{}, base...)
	index := make(map[string]int, len(result))
	for i, kv := range result {
		if pos := strings.Index(kv, "="); pos >= 0 {
			index[kv[:pos]] = i
		}
	}

	for _, kv := range override {
		if pos := strings.Index(kv, "="); pos >= 0 {
			key := kv[:pos]
			if idx, ok := index[key]; ok {
				result[idx] = kv
			} else {
				index[key] = len(result)
				result = append(result, kv)
			}
		}
	}
	return result
}

func loadSpecFromBundle(bundle string) (specs.Spec, error) {
	configPath := filepath.Join(bundle, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return specs.Spec{}, fmt.Errorf("failed to read %s: %w", configPath, err)
	}
	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return specs.Spec{}, fmt.Errorf("failed to unmarshal %s: %w", configPath, err)
	}
	return spec, nil
}

// create prepares the container to be started.
func (c *Container) create(ctx context.Context) error {
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StateReady)
	}

	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	if err := c.setContainerState(ctx, StateReady); err != nil {
		return err
	}
	return nil
}

// doStop performs the actual stop operation on the client.
func (c *Container) doStop(force bool) error {
	if c.config != nil && c.config.IsInfra {
		if c.infraCmd == nil || c.infraCmd.Process == nil {
			return nil
		}
		if err := c.infraCmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	currentState := c.checkState()
	if currentState == StateStopped {
		log.Debugf("Container %s is already stopped.", c.id)
		return nil
	}

	if err := c.state.Transition(currentState, StateStopped); err != nil && !force {
		return err
	}

	if err := libmica.Stop(c.ID()); err != nil {
		return err
	}
	return nil
}

// stop stops the container.
// for semantic continuation, register client at micad even if client is not here
func (c *Container) stop(ctx context.Context, force bool) error {
	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	var err error
	if err = c.doStop(force); err != nil {
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	if err = c.setContainerState(ctx, StateStopped); err != nil {
		return err
	}

	return nil
}

// kill forcibly stops the container.
// Due to the 1:1:1 relationship of Container:ClientOS:Task in mica, kill() is essentially stop().
func (c *Container) kill() error {

	if c.sandbox == nil {
		return fmt.Errorf("container sandbox is nil")
	}
	if c.sandbox.state.State != StateReady && c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running or ready, can not signal container")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	log.Debugf("Container state is %s.", currentState)

	if libmica.ClientNotExist(c.id) {
		return c.setContainerState(c.ctx, StateStopped)
	} else if err := c.doStop(true); err != nil {
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	if err := c.setContainerState(c.ctx, StateStopped); err != nil {
		return err
	}
	return nil
}

// delete removes the container.
// remove mica client first and update state, then remove container instance from sandbox container list
// clean cached data finally
func (c *Container) delete(ctx context.Context) error {
	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateReady &&
		currentState != StatePaused &&
		currentState != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete container")
	}

	if c.config == nil || !c.config.IsInfra {
		if err := libmica.Remove(c.id); err != nil {
			log.Debugf("Failed to remove container %s.", err)
			return err
		}
	}
	if err := c.sandbox.removeContainer(c.id); err != nil {
		return err
	}
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		return fmt.Errorf("failed to store sandbox")
	}
	if err := utils.RemoveContainerCacheDir(c.id); err != nil {
		log.Warnf("failed to remove cache directory for container %s: %v", c.id, err)
	}
	return nil
}

// pause pauses the container's execution.
func (c *Container) pause(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateRunning {
		return fmt.Errorf("container is not running, cannot pause container")
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StatePaused)
	}
	if err := libmica.Pause(c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StatePaused)
}

// resume resumes a paused container.
func (c *Container) resume(ctx context.Context) error {
	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StatePaused && c.sandbox.state.State != StateStopped {
		return fmt.Errorf("container is not paused, cannot resume container")
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StateRunning)
	}
	log.Debugf("resuming container %s (restarting RTOS)", c.id)
	if err := libmica.Start(c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) update(ctx context.Context, resources specs.LinuxResources) error {
	if c.config != nil && c.config.IsInfra {
		return nil
	}
	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}
	if c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running, cannot update container")
	}
	if c.notOperational() {
		return fmt.Errorf("container not ready or running, cannot update")
	}
	if err := c.validateUpdate(); err != nil {
		return err
	}

	pedRes, hasUpdates := c.extractChanges(resources)
	if !hasUpdates {
		return nil
	}

	return c.applyChanges(ctx, pedRes, resources)
}

func (c *Container) validateUpdate() error {

	if c.config == nil {
		return fmt.Errorf("container config is nil")
	}
	if c.config.Resources == nil {
		c.config.Resources = &specs.LinuxResources{}
	}
	if c.config.Resources.CPU == nil {
		c.config.Resources.CPU = &specs.LinuxCPU{}
	}
	if c.config.Resources.Memory == nil {
		c.config.Resources.Memory = &specs.LinuxMemory{}
	}
	return nil
}

func (c *Container) extractChanges(resources specs.LinuxResources) (*ped.EssentialResource, bool) {
	pedRes := ped.InitResource()
	pedRes.MemoryMaxMB = nil
	pedRes.CPUWeight = nil
	hasUpdates := false

	if cpu := resources.CPU; cpu != nil {
		if cpu.Period != nil && *cpu.Period != 0 {
			hasUpdates = true
		}
		if cpu.Quota != nil && *cpu.Quota != 0 {
			hasUpdates = true
		}
		if cpu.Cpus != "" {
			pedRes.ClientCpuSet = cpu.Cpus
			hasUpdates = true
		}
		if cpu.Shares != nil {
			weight := ped.ShareToWeight(*cpu.Shares)
			weightCopy := weight
			pedRes.CPUWeight = &weightCopy
			hasUpdates = true
		}
	}

	if mem := resources.Memory; mem != nil && mem.Limit != nil {
		limitMiB := uint32(*mem.Limit >> 20)
		pedRes.MemoryMinMB = limitMiB
		pedRes.MemoryMaxMB = copyUint32(limitMiB)
		hasUpdates = true
	}

	return pedRes, hasUpdates
}

func (c *Container) applyChanges(ctx context.Context, pedRes *ped.EssentialResource, resources specs.LinuxResources) error {
	if err := updateContainerResource(c, pedRes); err != nil {
		return err
	}

	res := c.config.Resources

	if cpu := resources.CPU; cpu != nil {
		if cpu.Period != nil && *cpu.Period != 0 {
			res.CPU.Period = cpu.Period
		}
		if cpu.Quota != nil && *cpu.Quota != 0 {
			res.CPU.Quota = cpu.Quota
		}
		if cpu.Cpus != "" {
			res.CPU.Cpus = cpu.Cpus
		}
		if cpu.Shares != nil {
			sharesCopy := *cpu.Shares
			res.CPU.Shares = &sharesCopy
		}
	}

	if mem := resources.Memory; mem != nil && mem.Limit != nil {
		res.Memory.Limit = mem.Limit
	}

	if err := c.sandbox.updateResources(ctx); err != nil {
		log.Debugf("Update best-effort: ignore sandbox.updateResources error for %s: %v", c.id, err)
	}

	return nil
}

// Traits:
func (c *Container) ID() string {
	return c.id
}

func (c *Container) GetAnnotations() map[string]string {
	return c.config.Annotations
}

func (c *Container) GetPid() int {
	return c.config.Pid
}

func (c *Container) GetMemoryLimit() uint64 {
	return uint64(c.config.memoryLimitMB())
}

func (c *Container) Sandbox() SandboxTraits {
	return c.sandbox
}

func (c *Container) Status() StateString {
	return c.checkState()
}

func (c *Container) State() *ContainerState {
	c.checkState()
	return &c.state
}

// Signal sends a signal to the container.
// TODO: Implement a POSIX signals hub.
func (c *Container) Signal(ctx context.Context, signal syscall.Signal) error {
	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}
	if c.sandbox.notOperational() {
		return fmt.Errorf("sandbox is not running or ready, can not signal container")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateRunning && currentState != StateReady && currentState != StatePaused {
		return fmt.Errorf("client os is not running, ready or paused, can not signal container")
	}

	return nil
}

// validOS checks if the OS is in the list of preserved OSes.
// TODO: RTOS validation list should be coordinates with mica
func validOS(os string) bool {
	ret := utils.InList(defs.TrustyOS[:], os)
	return ret
}

// validComponent checks if a component file is a regular file.
func validComponent(component string) bool {
	if !utils.IsRegular(component) {
		log.Debugf("validComponent: %s is not a regular file", component)
		return false
	}

	hostArch := runtime.GOARCH
	log.Debugf("validComponent: checking %s on host arch %s", component, hostArch)

	if match, _ := utils.IsELFForHost(component); match {
		log.Debugf("validComponent: %s is a valid ELF for host", component)
		return true
	}

	// check for arm64 xen client image
	if hostArch == "arm64" {
		if isArm64XenImg(component) {
			return true
		}
	}

	return true
}

func isArm64XenImg(firmware string) bool {
	if runtime.GOARCH != "arm64" {
		return false
	}

	fh, err := os.Open(firmware)
	if err == nil {
		// check magic number
		buf := make([]byte, 0x40)
		if n, _ := fh.Read(buf); n > 0x3C {
			if bytes.Contains(buf, []byte("ARMd")) {
				fh.Close()
				return true
			}
		}
	}

	fh.Close()
	return false
}

// validFirmware checks if the firmware file is valid.
func validFirmware(firmware string) bool {
	return validComponent(firmware)
}

// validBinfile checks if the binary file is valid.
// For Xen, this is typically image.bin.
func validBinfile(binpath string) bool {
	return validComponent(binpath)
}

// validMicaContainer checks if the container configuration is valid for mica.
// NOTICE: Xen is the only supported pedestal for now.
func (c *Container) validMicaContainer() bool {
	if c.config != nil && c.config.IsInfra {
		return true
	}

	os := c.os()
	firmware := c.getFirmware()
	log.Debugf("validMicaContainer: os=%q, firmware=%q", os, firmware)

	osValid := validOS(os)
	fwValid := validFirmware(firmware)
	if HostPedType == ped.Xen {
		binFile := validBinfile(c.getPedConf())
		log.Debugf("validMicaContainer: pedConf=%q, binFile valid=%v", c.getPedConf(), binFile)
		fwValid = binFile && fwValid
	}
	judge := osValid && fwValid
	log.Debugf("container validation: os=%v, firmware=%v, valid=%v", osValid, fwValid, judge)

	return judge
}

// setContainerState updates the container's state and persists it.
func (c *Container) setContainerState(ctx context.Context, state StateString) error {
	if state == "" {
		return fmt.Errorf("state cannot be empty")
	}

	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}

	c.state.State = state
	c.updateExitNotifier(state)
	if err := c.SaveState(); err != nil {
		log.Errorf("failed to save container state: %v", err)
		return err
	}
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		log.Errorf("failed to save sandbox state: %v", err)
		return err
	}
	return nil
}

func (c *Container) updateExitNotifier(state StateString) {
	c.exitNotifierMu.Lock()
	defer c.exitNotifierMu.Unlock()

	switch state {
	case StateStopped, StateDown:
		if c.exitNotifier != nil {
			close(c.exitNotifier)
			c.exitNotifier = nil
		}
	default:
		if c.exitNotifier == nil {
			c.exitNotifier = make(chan struct{})
		}
	}
}

func (c *Container) exitNotifierForState(state StateString) chan struct{} {
	c.updateExitNotifier(state)
	c.exitNotifierMu.Lock()
	defer c.exitNotifierMu.Unlock()
	return c.exitNotifier
}

func (c *Container) checkState() StateString {
	if c == nil || c.id == "" {
		return StateDown
	}

	if c.config != nil && c.config.IsInfra {
		return c.state.State
	}

	if libmica.ClientNotExist(c.id) {
		if c.state.State != StateDown {
			if err := c.setContainerState(c.ctx, StateDown); err != nil {
				log.Warnf("failed to mark container %s as down: %v", c.id, err)
			}
		}
		return StateDown
	}

	return c.state.State
}

// register client when container is missing and the container is not a infra container
func (c *Container) ensureClientPresence() (StateString, error) {
	state := c.checkState()
	log.Debugf("ensureClientPresence: container %s state=%s shouldPresent=%v", c.id, state, c.shouldPresent())
	if state != StateDown {
		return state, nil
	}

	if c.shouldPresent() && libmica.ClientNotExist(c.id) {
		log.Debugf("ensureClientPresence: registering client %s", c.id)
		if err := c.registerClient(); err != nil {
			return StateDown, err
		}
	}

	state = c.checkState()
	log.Debugf("ensureClientPresence: after registration, container %s state=%s", c.id, state)
	if state == StateDown {
		return StateDown, er.ContainerNotFound
	}

	return state, nil
}

func (c *Container) shouldPresent() bool {
	if c == nil || c.config == nil || c.config.IsInfra {
		return false
	}
	return true
}

func (c *Container) registerClient() error {
	log.Debugf("registerClient: creating mica client conf for %s", c.id)
	conf, err := createMicaClientConf(c)
	if err != nil {
		return err
	}

	log.Debugf("registerClient: calling libmica.Create for %s", c.id)
	if err := libmica.Create(conf); err != nil {
		log.Errorf("registerClient: libmica.Create failed: %v", err)
		return err
	}
	log.Debugf("registerClient: libmica.Create succeeded for %s", c.id)

	limit := c.config.memoryLimitMB()
	initialMem := limit
	if initialMem == 0 {
		initialMem = c.config.memoryReservationMB()
	}
	if initialMem == 0 {
		initialMem = defs.DefaultMinMemMB
	}
	recordThreshold := limit
	if recordThreshold == 0 {
		recordThreshold = initialMem
	}
	c.me.RecordMemoryState(initialMem, recordThreshold)

	return c.setContainerState(c.ctx, StateReady)
}

func (c *Container) setupMemory() error {
	if c == nil || c.config == nil || c.config.IsInfra {
		return nil
	}

	if HostPedType != ped.Xen {
		return nil
	}

	limit := c.config.memoryLimitMB()
	if limit == 0 {
		return nil
	}

	if c.me.CurrentMaxMem() == limit && c.me.MemoryThresholdMB() >= limit {
		return nil
	}

	target := int(limit)
	log.Debugf("setting mem threshold to %d MB", target)
	if err := c.me.UpdateMemoryThreshold(limit); err != nil {
		return fmt.Errorf("failed to set new memory threshold to %d MB for %s: %w", limit, c.id, err)
	}
	if err := c.me.UpdateMemory(limit); err != nil {
		return fmt.Errorf("failed to set memory to %d MB for %s: %w", limit, c.id, err)
	}

	c.me.RecordMemoryState(limit, limit)
	return nil
}

func (c *Container) GetClientCPU() string {
	if c.cpuUnset() {
		return ""
	}
	return c.config.cpuMask()
}

// SaveState persists the container's state to disk at two locations for redundancy.
func (c *Container) SaveState() error {
	serializable := struct {
		ID            string          `json:"id"`
		SandboxID     string          `json:"sandbox_id"`
		State         ContainerState  `json:"state"`
		Config        ContainerConfig `json:"config"`
		Mounts        []Mount         `json:"mounts"`
		ContainerPath string          `json:"container_path"`
	}{
		ID:            c.id,
		SandboxID:     c.sandbox.SandboxID(),
		State:         c.state,
		Config:        *c.config,
		Mounts:        c.mounts,
		ContainerPath: c.containerPath,
	}

	failed, failed1 := false, false
	var err error
	var err1 error

	cwd, err := os.Getwd()
	if err != nil {
		log.Warnf("Failed to get current working directory: %v", err)
		cwd = "."
	}
	stateInBundle := filepath.Join(cwd, c.containerPath, defs.MicrunContainerStateFile)
	stateInMicrunDir := filepath.Join(defs.MicrunStateDir, c.containerPath, defs.MicrunContainerStateFile)
	log.Infof("stateInBundle: %s", stateInBundle)

	bundleDir := filepath.Dir(stateInBundle)
	if err := utils.EnsureDir(bundleDir, defs.DirMode); err != nil {
		log.Warnf("Failed to ensure bundle directory: %v.", err)
	}
	if err := utils.EnsureDir(filepath.Dir(stateInMicrunDir), defs.DirMode); err != nil {
		log.Warnf("Failed to ensure micrun state directory: %v.", err)
	}

	if err = utils.SaveStructToJSON(stateInBundle, serializable); err != nil {
		failed = true
		err = fmt.Errorf("failed to save state to <%s>: %w", stateInBundle, err)
	}

	if err1 = utils.SaveStructToJSON(stateInMicrunDir, serializable); err1 != nil {
		failed1 = true
		err1 = fmt.Errorf("failed to save state to <%s>: %w", stateInMicrunDir, err1)
	}

	if failed1 && failed {
		return fmt.Errorf("failed to save container state to both locations: %w, %w", err, err1)
	}
	return nil
}

// RestoreState loads the container's state from disk, trying the primary and fallback locations.
func (c *Container) RestoreState() error {
	type ContainerStorage struct {
		ID            string          `json:"id"`
		SandboxID     string          `json:"sandbox_id"`
		State         ContainerState  `json:"state"`
		Config        ContainerConfig `json:"config"`
		Mounts        []Mount         `json:"mounts"`
		ContainerPath string          `json:"container_path"`
	}

	var storage ContainerStorage

	stateInMicrunDir := filepath.Join(defs.MicrunStateDir, c.id, defs.MicrunContainerStateFile)
	raw, err := utils.RestoreStructFromJSON(stateInMicrunDir)

	if err != nil {
		cwd, err := os.Getwd()
		if err != nil {
			log.Warnf("Failed to get current working directory: %v", err)
			cwd = "."
		}
		stateInBundle := filepath.Join(cwd, c.containerPath, defs.MicrunContainerStateFile)
		raw, err = utils.RestoreStructFromJSON(stateInBundle)
		if err != nil {
			return fmt.Errorf("failed to restore container state from both locations: %w", err)
		}
	}

	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("failed to marshal raw data: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, &storage); err != nil {
		return fmt.Errorf("failed to unmarshal container storage: %w", err)
	}

	c.state = storage.State
	c.mounts = storage.Mounts
	c.containerPath = storage.ContainerPath
	c.updateExitNotifier(c.state.State)

	return nil
}

// stats returns the container statistics.
// For Xen-based guests, we derive CPU usage from xl vcpu-list time(s) and memory from libmica.
func (c *Container) stats() (*ContainerStats, error) {
	if c.sandbox == nil {
		return nil, fmt.Errorf("container sandbox reference is nil")
	}
	if c.sandbox.state.State != StateRunning {
		return nil, fmt.Errorf("sandbox is not running, cannot stats container")
	}

	// CPU: sum per-vCPU consumed time(s) -> microseconds
	var totalUsec uint64
	if vcpuInfo, err := ped.XlVcpuList(); err == nil && vcpuInfo != nil {
		var entries []ped.VCPUEntry

		if v, ok := vcpuInfo.DomainVCPUMap[c.id]; ok {
			entries = v
		}
		for _, e := range entries {
			if e.TimeSeconds > 0 {
				totalUsec += uint64(e.TimeSeconds * 1_000_000.0)
			}
		}
	}

	curMB := c.me.CurrentMaxMem()
	thrMB := c.me.MemoryThresholdMB()
	if thrMB == 0 {
		thrMB = c.config.memoryLimitMB()
	}
	usageBytes := uint64(curMB) << 20
	limitBytes := uint64(thrMB) << 20

	st := &ContainerStats{
		ResourceStats: &ResourceStats{
			CPUStats: CPUStats{
				TotalUsage: totalUsec,
				NrPeriods:  0, // no cgroup-like period semantics in Xen; leave zero.
			},
			MemoryStats: MemoryStats{
				Cache: 0,
				Usage: MemoryEntry{
					Failcnt: 0,
					Limit:   limitBytes,
					MaxEver: limitBytes, // Conservative default until HWM tracking exists.
					Usage:   usageBytes,
				},
				Stats: map[string]uint64{}, // Reserved for future detailed stats.
			},
		},
		NetworkStats: nil,
	}
	return st, nil
}

// setVcpuAffinity sets the VCPU affinity for the container.
func (c *Container) setVcpuAffinity(cpuSet cpuset.CPUSet) error {
	var result *multierror.Error
	cpulist := cpuSet.ToSlice()
	if err := c.me.VcpuPin(cpulist); err != nil {
		result = multierror.Append(result, err)
	}

	ret := result.ErrorOrNil()
	if ret == nil {
		c.config.VCPUNum = uint32(cpuSet.Size())
		if cpu := c.config.ensureCPU(); cpu != nil {
			cpu.Cpus = cpuSet.String()
		}
		c.config.PCPUNum = int(c.config.VCPUNum)
	}
	return ret
}

// winresize resizes the container's PTY.
func (c *Container) winresize(height, width uint32) error {
	if c.notOperational() {
		return fmt.Errorf("container not ready or running, impossible to resize the container pty")
	}
	log.Debugf("resizing PTY for container %s to [%dx%d]", c.id, width, height)
	stdin, stdout, p, err := dialTTY(c.ctx, c.id)
	if err != nil {
		return err
	}
	_ = stdin.Close()
	defer stdout.Close()
	log.Debugf("resizing rpmsg tty at %s", p)

	ws := &unix.Winsize{Row: uint16(height), Col: uint16(width)}
	if err := unix.IoctlSetWinsize(int(stdout.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("set winsize: %w", err)
	}
	return nil
}

// firmware is the elf file of rtos
func (c *Container) getFirmware() string {
	return c.config.ImageAbsPath
}

func (c *Container) getPedConf() string {
	return c.config.PedestalConf
}

func (c *Container) os() string {
	return c.config.OS
}

func (c *Container) cpuUnset() bool {
	return c.config.cpuMask() == ""
}

// notOperational checks if the container is not in a state to be operated on.
func (c *Container) notOperational() bool {
	currentState := c.checkState()

	return currentState != StateReady && currentState != StateRunning
}
