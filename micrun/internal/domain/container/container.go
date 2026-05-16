package container

import (
	"context"
	"errors"
	"fmt"
	"micrun/internal/ports"
	"micrun/internal/support/cpuset"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type Container struct {
	ctx            context.Context
	id             string
	guestExec      ports.GuestExecutor
	config         *ContainerConfig
	sandbox        *Sandbox
	mounts         []Mount
	rootfs         RootFs
	containerPath  string
	state          ContainerState
	infraCmd       *exec.Cmd
	exitNotifier   chan struct{}
	exitNotifierMu sync.Mutex
}

type ContainerConfig struct {
	ID             string
	Rootfs         RootFs
	Mount          []Mount
	ReadOnlyRootfs bool
	IsInfra        bool
	Pid            int
	Annotations    map[string]string
	Resources      *specs.LinuxResources

	ImageAbsPath string       `json:"elf_abs_path"`
	PedestalType PedestalType `json:"pedestal_type"`
	PedestalConf string       `json:"pedestal_conf"`
	CacheRoot    string       `json:"cache_root,omitempty"`
	OS           string       `json:"os"`

	VCPUNum    uint32 `json:"vcpu_num"`
	PCPUNum    int    `json:"ncpu"`
	MaxVcpuNum uint32 `json:"max_vcpu_num"`

	MemoryThresholdMB uint32 `json:"memory_threshold"`
	Cmdline           string `json:"cmdline"`

	ExclusiveDom0CPU bool `json:"exclusive_dom0_cpu"`
}

func newContainer(s *Sandbox, cc *ContainerConfig) (*Container, error) {
	return newContainerWithContext(context.Background(), s, cc)
}

func newContainerWithContext(ctx context.Context, s *Sandbox, cc *ContainerConfig) (*Container, error) {
	if cc == nil {
		return &Container{}, fmt.Errorf("container config is none")
	}

	if cc.ID == "" {
		log.Tracef("Empty container id.")
		return &Container{}, er.EmptyContainerID
	}
	deps, err := s.dependenciesChecked()
	if err != nil {
		return nil, err
	}

	c := &Container{
		id:            cc.ID,
		guestExec:     deps.GuestExecutorFactory(cc.ID),
		sandbox:       s,
		config:        cc,
		rootfs:        cc.Rootfs,
		containerPath: filepath.Join(s.id, cc.ID),
		mounts:        cc.Mount,
		state:         ContainerState{State: StateDown},
		ctx:           context.Background(),
	}

	if err := c.restoreState(ctx); err != nil && !errors.Is(err, er.ContainerNotFound) {
		log.Debugf("failed to restore container state: %v.", err)
		return nil, fmt.Errorf("failed to restore container state for %s: %w", c.id, err)
	}

	c.updateExitNotifier(c.checkState())

	return c, nil
}

func CleanupContainerWithDependencies(ctx context.Context, guestCtl ports.GuestControl, sandboxID string, containerID string, force bool, deps *Dependencies) error {
	log.Debugf("Cleaning up sandbox %s, container %s.", sandboxID, containerID)
	if sandboxID == "" {
		return er.EmptySandboxID
	}
	if containerID == "" {
		return er.EmptyContainerID
	}
	if guestCtl == nil {
		return fmt.Errorf("guest control is required")
	}

	sandbox, err := loadSandbox(ctx, sandboxID, guestCtl, deps)
	if err != nil {
		return cleanupOrphanedContainer(ctx, guestCtl, sandboxID, containerID, force, deps, err)
	}

	return cleanupContainerInSandbox(ctx, sandbox, containerID, force)
}

func cleanupOrphanedContainer(ctx context.Context, guestCtl ports.GuestControl, sandboxID, containerID string, force bool, deps *Dependencies, loadErr error) error {
	if !errors.Is(loadErr, er.SandboxNotFound) {
		return loadErr
	}
	exists, err := guestCtl.Exists(ctx, containerID)
	if err != nil {
		return err
	}
	if exists && !force {
		return fmt.Errorf("sandbox state missing while client %s still exists", containerID)
	}
	repo, err := stateRepositoryFromDependenciesChecked(deps)
	if err != nil {
		return err
	}
	if err := repo.DeleteContainer(ctx, containerID, filepath.Join(sandboxID, containerID)); err != nil {
		return fmt.Errorf("failed to delete orphaned container state for %s: %w", containerID, err)
	}
	log.Debugf("Sandbox %s already removed from disk, skipping container %s cleanup.", sandboxID, containerID)
	return nil
}

func cleanupContainerInSandbox(ctx context.Context, sandbox *Sandbox, containerID string, force bool) error {
	if _, err := sandbox.StopContainer(ctx, containerID, force); !tolerable(err, force) {
		return err
	}
	if _, err := sandbox.DeleteContainer(ctx, containerID); !tolerable(err, force) {
		return err
	}
	if len(sandbox.containers) > 0 {
		return nil
	}
	if err := sandbox.Stop(ctx, force); err != nil && !force {
		return err
	}
	return sandbox.Delete(ctx)
}

func tolerable(err error, force bool) bool {
	if err == nil {
		return true
	}
	if force || errors.Is(err, er.ContainerNotFound) {
		return true
	}
	return false
}

func (c *Container) ID() string {
	if c == nil {
		return ""
	}
	return c.id
}

func (c *Container) GetAnnotations() map[string]string {
	if c == nil || c.config == nil {
		return nil
	}
	return c.config.Annotations
}

func (c *Container) GetPid() int {
	if c == nil || c.config == nil {
		return 0
	}
	return c.config.Pid
}

func (c *Container) GetMemoryLimit() uint64 {
	if c == nil || c.config == nil {
		return 0
	}
	return uint64(c.config.memoryLimitMB())
}

func (c *Container) Sandbox() SandboxTraits {
	if c == nil {
		return nil
	}
	return c.sandbox
}

func (c *Container) Status() StateString {
	state, err := c.StateSnapshot()
	if err != nil {
		log.Warnf("failed to get container status snapshot: %v", err)
	}
	return state.State
}

func (c *Container) State() *ContainerState {
	if c == nil {
		return &ContainerState{State: StateDown}
	}
	if _, err := c.StateSnapshot(); err != nil {
		log.Warnf("failed to get container state snapshot for %s: %v", c.id, err)
	}
	return &c.state
}

func (c *Container) StateSnapshot() (ContainerState, error) {
	if c == nil {
		return ContainerState{State: StateDown}, nil
	}
	if _, err := c.checkStateWithError(); err != nil {
		return c.state, err
	}
	return c.state, nil
}

func (c *Container) setVcpuAffinity(ctx context.Context, cpuSet cpuset.CPUSet) error {
	if c == nil {
		return er.ContainerNotFound
	}
	if c.config == nil {
		return fmt.Errorf("container config is nil")
	}
	if c.guestExec == nil {
		return fmt.Errorf("guest executor is nil")
	}

	var result *multierror.Error
	cpulist := cpuSet.ToSlice()
	if err := c.guestExec.VCPUPin(ctx, cpulist); err != nil {
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

func (c *Container) getFirmware() string {
	if c == nil || c.config == nil {
		return ""
	}
	return c.config.ImageAbsPath
}

func (c *Container) getPedConf() string {
	if c == nil || c.config == nil {
		return ""
	}
	return c.config.PedestalConf
}

func (c *Container) os() string {
	if c == nil || c.config == nil {
		return ""
	}
	return c.config.OS
}

func (c *Container) cpuUnset() bool {
	if c == nil || c.config == nil {
		return true
	}
	return c.config.cpuMask() == ""
}

func (c *Container) notOperational() bool {
	operational, err := c.operational()
	if err != nil {
		log.Warnf("failed to check container %s operational state: %v", c.id, err)
		return true
	}
	return !operational
}

func (c *Container) operational() (bool, error) {
	return c.operationalWithContext(c.ctx)
}

func (c *Container) operationalWithContext(ctx context.Context) (bool, error) {
	currentState, err := c.checkStateWithContext(ctx)
	if err != nil {
		return false, err
	}
	return currentState == StateReady || currentState == StateRunning, nil
}

func (c *Container) stateRepositoryChecked() (stateRepository, error) {
	if c != nil && c.sandbox != nil {
		return c.sandbox.stateRepositoryChecked()
	}
	return stateRepository{}, fmt.Errorf("container: no state repository available; container has no sandbox reference")
}
