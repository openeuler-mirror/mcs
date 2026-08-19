package container

import (
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/runtime-spec/specs-go"
	"micrun/internal/ports"
	"micrun/internal/support/cpuset"
	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
	"path/filepath"
	"sync"
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
	stateMu        sync.RWMutex // protects state field
	exitNotifier   chan struct{}
	exitNotifierMu sync.Mutex
	registrationMu sync.Mutex
	// startMu serializes Start invocations: two concurrent Starts would
	// both pass the state check, double-start the guest, and the loser's
	// failure rollback would destroy the winner's domain. Holding it also
	// makes the failure rollback safe (no concurrent winner inside).
	startMu sync.Mutex
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
	// The sandbox metadata is gone but the Xen domain may still be running.
	// Remove it (best-effort under force) before deleting the local state so
	// the guest side does not leak a domain that can never be reached again.
	if exists {
		if rErr := guestCtl.Remove(ctx, containerID); rErr != nil && !force {
			return fmt.Errorf("failed to remove orphaned guest %s: %w", containerID, rErr)
		}
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
	if sandbox.containerCount() > 0 {
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

// IsInfra reports whether this container is the sandbox infra (pause) container.
// Exported on ContainerTraits so the transport layer can distinguish a CRI
// InfraOnly pod (sandbox id is the infra id, never registered in micad) from a
// standalone sandbox when reconciling a duplicate Create.
func (c *Container) IsInfra() bool {
	return c.isInfra()
}

func (c *Container) GetMemoryLimit() uint64 {
	if c == nil || c.config == nil {
		return 0
	}
	if c.sandbox == nil {
		return uint64(c.config.memoryLimitMB())
	}
	// Resources.Memory.Limit is mutated by UpdateContainer under
	// containersLock; read it under the same lock to avoid a torn read of
	// the *int64 (setupMemory locks the same field for the same reason).
	return lockutil.WithReadLockValue(&c.sandbox.containersLock, func() uint64 {
		return uint64(c.config.memoryLimitMB())
	})
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
	snapshot, err := c.StateSnapshot()
	if err != nil {
		log.Warnf("failed to get container state snapshot for %s: %v", c.id, err)
	}
	// Return a copy so callers cannot mutate the internal state without the lock.
	state := snapshot
	return &state
}

func (c *Container) StateSnapshot() (ContainerState, error) {
	if c == nil {
		return ContainerState{State: StateDown}, nil
	}
	if _, err := c.checkStateWithError(); err != nil {
		return c.snapshotState(), err
	}
	return c.snapshotState(), nil
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
		// ContainerConfig fields are shared with sandbox-wide readers
		// (getSandboxCpusetStr, calculateSandboxVCPUs, json.Marshal in
		// StoreSandbox). Protect the write under containersLock so those
		// readers don't race on the string/uint32 assignment.
		//
		// SharedCPUPool only pins affinity onto the shared host cpuset; it
		// must not rewrite VCPUNum/PCPUNum to the pool size (that would make
		// every 1-vCPU guest look like N-vCPU and inflate sandbox totals).
		// Exclusive mode still treats the pin set as the 1:1 vCPU footprint.
		c.sandbox.containersLock.Lock()
		sharedPool := c.sandbox.config != nil && c.sandbox.config.SharedCPUPool
		if cpu := c.config.ensureCPU(); cpu != nil {
			cpu.Cpus = cpuSet.String()
		}
		if !sharedPool {
			c.config.VCPUNum = uint32(cpuSet.Size())
			c.config.PCPUNum = int(c.config.VCPUNum)
		}
		c.sandbox.containersLock.Unlock()
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
