package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"micrun/internal/ports"
	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
)

type ContainerStorage struct {
	ID            string          `json:"id"`
	SandboxID     string          `json:"sandbox_id"`
	State         ContainerState  `json:"state"`
	Config        ContainerConfig `json:"config"`
	Mounts        []Mount         `json:"mounts"`
	ContainerPath string          `json:"container_path"`
}

func (c *Container) setContainerState(ctx context.Context, state StateString) error {
	if state == "" {
		return fmt.Errorf("state cannot be empty")
	}
	if !state.known() {
		return fmt.Errorf("unknown state: %s", state)
	}

	if err := c.requireSandbox(); err != nil {
		return err
	}

	// Mutate the in-memory state under stateMu, then persist without holding
	// the lock. StoreSandbox re-reads the state via snapshot helpers that
	// take stateMu for read, so holding it across persistence would
	// self-deadlock. The exit notifier is updated only AFTER persistence
	// succeeds: closing the terminal-state channel signals waiters that the
	// container has exited, and that signal cannot be taken back if we then
	// fail to persist and roll back.
	c.stateMu.Lock()
	oldState := c.state.State
	c.state.State = state
	c.stateMu.Unlock()

	// Single atomic write: the container's runtime state is embedded in the
	// sandbox document (SandboxStorage.Containers), so one StoreSandbox
	// captures container + sandbox consistently. The legacy two-file layout
	// dual-wrote container then sandbox and needed a rollback-re-persist
	// dance to keep the files from diverging when a crash or failure landed
	// between the writes.
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		log.Errorf("failed to persist container state: %v", err)
		c.rollbackState(state, oldState)
		return err
	}

	// Persistence succeeded: it is now safe to publish the state change (which
	// may close the exit notifier for terminal states) to waiters. Re-read the
	// current state under the lock: a concurrent setContainerState may have
	// overwritten `state` while this call was persisting, and publishing the
	// stale value (e.g. closing the notifier for Stopped while the live state
	// is Running again) would make WaitContainerExit report a spurious exit.
	c.stateMu.Lock()
	c.updateExitNotifier(c.state.State)
	c.stateMu.Unlock()
	return nil
}

// rollbackState restores the previous in-memory state, but only if no
// concurrent setContainerState has changed it in the meantime. The exit
// notifier is not touched here because the new state was never published to
// waiters (the notifier update happens only after successful persistence).
func (c *Container) rollbackState(state StateString, oldState StateString) {
	c.stateMu.Lock()
	// Only roll back if the current state is still the value this call
	// wrote. A concurrent setContainerState may have legitimately
	// transitioned the container to a different terminal state (e.g. Down)
	// and persisted it while our saveState/StoreSandbox was failing;
	// blindly restoring oldState would clobber that and diverge from disk.
	if c.state.State == state {
		c.state.State = oldState
	}
	c.stateMu.Unlock()
}

func (c *Container) updateExitNotifier(state StateString) {
	lockutil.WithLock(&c.exitNotifierMu, func() {
		c.applyExitNotifierState(state)
	})
}

func (c *Container) applyExitNotifierState(state StateString) {
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
	var notifier chan struct{}
	lockutil.WithLock(&c.exitNotifierMu, func() {
		c.applyExitNotifierState(state)
		notifier = c.exitNotifier
	})
	return notifier
}

func (c *Container) checkState() StateString {
	if c == nil {
		return StateDown
	}
	state, err := c.checkStateWithError()
	if err != nil {
		log.Warnf("failed to check container %s state: %v", c.id, err)
		return c.currentState()
	}
	return state
}

// currentState returns the container state under stateMu.
func (c *Container) currentState() StateString {
	return lockutil.WithReadLockValue(&c.stateMu, func() StateString {
		return c.state.State
	})
}

// snapshotState returns a copy of the full container state under stateMu.
func (c *Container) snapshotState() ContainerState {
	return lockutil.WithReadLockValue(&c.stateMu, func() ContainerState {
		return c.state
	})
}

func (c *Container) checkStateWithError() (StateString, error) {
	return c.checkStateWithContext(c.ctx)
}

func (c *Container) checkStateWithContext(ctx context.Context) (StateString, error) {
	if c == nil || c.id == "" {
		return StateDown, nil
	}

	if c.config != nil && c.config.IsInfra {
		return c.currentState(), nil
	}

	if c.sandbox == nil || c.sandbox.guestControl == nil {
		return c.currentState(), nil
	}

	ctx = queryContext(ctx)
	exists, err := c.sandbox.guestControl.Exists(ctx, c.id)
	if err != nil {
		return c.currentState(), err
	}
	if !exists {
		if c.currentState() != StateDown {
			if err := c.setContainerState(ctx, StateDown); err != nil {
				log.Warnf("failed to mark container %s as down: %v", c.id, err)
			}
		}
		return StateDown, nil
	}

	cur := c.currentState()
	// Socket presence alone is not liveness while we believe the guest is
	// active: micad may leave the control socket until unit cleanup even
	// after xl destroy / crash. Only probe Status for Running/Paused —
	// Ready/Created domains often report !Running until Start, which must
	// not be treated as Down.
	//
	// Critical: Suspended (paused) guests report Running=false. That is
	// healthy, not dead — treating !Running as Down would fire a false
	// guest-exit after Pause and kill Wait with fabricated 130.
	if cur == StateRunning || cur == StatePaused {
		status, statusErr := c.sandbox.guestControl.Status(ctx, c.id)
		if statusErr != nil {
			// Transient status RPC failure: keep the prior memory state.
			return cur, nil
		}
		if guestStatusAlivePaused(status) {
			if cur == StateRunning {
				// Crash/OOM between guest Pause and persisting StatePaused
				// leaves disk Running while Xen is Suspended. Converge so
				// recovery Resume (requires PAUSED) works instead of hanging
				// a "running" task with a suspended guest.
				if err := c.setContainerState(ctx, StatePaused); err != nil {
					log.Warnf("failed to mark container %s paused after suspended guest: %v", c.id, err)
					return cur, nil
				}
				return StatePaused, nil
			}
			return cur, nil
		}
		// On non-Xen pedestals Pause is implemented as a full mica stop (see
		// micaWireCommand: MPause->MStop): the guest is shut down ON PURPOSE
		// and micad then reports Stopped/Offline. For a container we believe
		// is Paused that is the expected shape, not a crash — keep Paused so a
		// later Resume (mica start) can boot the guest again. A container in
		// any other state reporting the same means the guest really died.
		if cur == StatePaused && guestStatusIntentionallyStopped(status) {
			return cur, nil
		}
		if status.Stopped || !status.Running {
			if cur != StateDown && cur != StateStopped {
				if err := c.setContainerState(ctx, StateDown); err != nil {
					log.Warnf("failed to mark container %s as down after guest status %q: %v", c.id, status.State, err)
				}
			}
			return StateDown, nil
		}
	}

	return cur, nil
}

// guestStatusAlivePaused reports whether Status describes a suspended/paused
// but still-present guest (not a crashed/destroyed domain).
func guestStatusAlivePaused(status ports.GuestStatus) bool {
	if status.Stopped {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(status.State))
	switch state {
	case "suspended", "paused":
		return true
	default:
		return false
	}
}

// guestStatusIntentionallyStopped reports whether Status describes a guest
// that was shut down on purpose and remains registered in micad: "Stopped"
// (explicit mica stop) or "Offline" (remoteproc shutdown on non-Xen
// pedestals). Only meaningful for a container already believed Paused — see
// the call site in checkStateWithContext. "Error" and other states are
// excluded: those indicate a crash, not an intentional stop.
func guestStatusIntentionallyStopped(status ports.GuestStatus) bool {
	if status.Stopped {
		return true
	}
	state := strings.ToLower(strings.TrimSpace(status.State))
	return state == "offline" || state == "stopped"
}

func (c *Container) SaveState() error {
	return c.saveState(c.ctx)
}

func (c *Container) saveState(ctx context.Context) error {
	// The container's runtime state lives in the combined sandbox document:
	// persisting a container means persisting the sandbox snapshot. This
	// removes the per-container write path entirely — a deleted container is
	// simply absent from the containers map, so a late persist cannot
	// resurrect its state by construction (StoreSandbox still guards the
	// sandbox-level storageRemoved race).
	if err := c.requireSandbox(); err != nil {
		return err
	}
	return c.sandbox.StoreSandbox(ctx)
}

func (c *Container) RestoreState() error {
	if c == nil {
		return er.ContainerNotFound
	}
	return c.restoreState(c.ctx)
}

func (c *Container) restoreState(ctx context.Context) error {
	if c == nil {
		return er.ContainerNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.normalizeContextFromSandbox()

	// Combined-format sandbox document: the runtime record was loaded with
	// the sandbox and is consumed one-shot here. The container's config is
	// already c.config (the live ContainerConfigs entry), so only the
	// runtime fields need applying.
	if c.sandbox != nil {
		if record, ok := c.sandbox.takeRestoredContainerRecord(c.id); ok {
			return c.applyRestoredContainerRecord(record)
		}
	}

	// Pre-combined-format fallback: per-container file (or its legacy
	// locations). The first StoreSandbox after rebuild migrates the state
	// into the combined document.
	repo, err := c.stateRepositoryChecked()
	if err != nil {
		return err
	}
	storage, err := repo.LoadContainer(ctx, c.id, c.containerPath, c.legacyBundleStatePath())
	if err != nil {
		return err
	}
	return c.applyRestoredContainerState(storage)
}

// applyRestoredContainerRecord applies a combined-document runtime record.
// Unlike the legacy path there is no config to overlay: c.config already IS
// the sandbox document's ContainerConfigs entry.
func (c *Container) applyRestoredContainerRecord(record ContainerRuntimeRecord) error {
	if !record.State.State.known() {
		return fmt.Errorf("container state invalid: %s", record.State.State)
	}
	lockutil.WithLock(&c.stateMu, func() {
		c.state = record.State
	})
	if len(record.Mounts) > 0 {
		c.mounts = record.Mounts
	}
	if record.ContainerPath != "" {
		c.containerPath = record.ContainerPath
	}
	if c.containerPath == "" && c.sandbox != nil {
		c.containerPath = filepath.Join(c.sandbox.id, c.id)
	}
	c.normalizeContextFromSandbox()
	c.normalizeGuestExecutorFromSandbox()
	c.updateExitNotifier(c.currentState())
	return nil
}

func (c *Container) applyRestoredContainerState(storage *ContainerStorage) error {
	if storage == nil {
		return fmt.Errorf("container state storage is nil")
	}
	if storage.ID == "" {
		storage.ID = c.id
	} else if storage.ID != c.id {
		return fmt.Errorf("container ID mismatch: %v != %v", storage.ID, c.id)
	}
	if c.sandbox != nil && storage.SandboxID != "" && storage.SandboxID != c.sandbox.id {
		return fmt.Errorf("container sandbox ID mismatch: %v != %v", storage.SandboxID, c.sandbox.id)
	}
	if !storage.State.State.known() {
		return fmt.Errorf("container state invalid: %s", storage.State.State)
	}
	if storage.Config.ID == "" {
		storage.Config.ID = storage.ID
	} else if storage.Config.ID != storage.ID {
		return fmt.Errorf("container config ID mismatch: %v != %v", storage.Config.ID, storage.ID)
	}

	lockutil.WithLock(&c.stateMu, func() {
		c.state = storage.State
	})
	c.mounts = storage.Mounts
	c.config = &storage.Config
	// Re-link the sandbox-wide ContainerConfigs entry to the same object:
	// updates write c.config while sandbox-wide readers (calculateSandboxVCPUs/
	// Memory, StoreSandbox, getSandboxCpusetStr) read the map entry. Without
	// this, a post-restore UpdateContainer is applied only to c.config and
	// silently lost on the next restart.
	if c.sandbox != nil && c.sandbox.config != nil {
		lockutil.WithLock(&c.sandbox.containersLock, func() {
			if c.sandbox.config.ContainerConfigs != nil {
				c.sandbox.config.ContainerConfigs[c.id] = c.config
			}
		})
	}
	c.containerPath = storage.ContainerPath
	if c.containerPath == "" && c.sandbox != nil {
		c.containerPath = filepath.Join(c.sandbox.id, c.id)
	}
	c.rootfs = c.config.Rootfs
	c.normalizeContextFromSandbox()
	c.normalizeGuestExecutorFromSandbox()
	c.updateExitNotifier(c.currentState())

	return nil
}

func (c *Container) normalizeContextFromSandbox() {
	if c != nil && c.ctx == nil && c.sandbox != nil {
		c.ctx = c.sandbox.ctx
	}
}

func (c *Container) normalizeGuestExecutorFromSandbox() {
	if c == nil || c.guestExec != nil || c.sandbox == nil {
		return
	}
	deps, err := c.sandbox.dependenciesChecked()
	if err != nil || deps.GuestExecutorFactory == nil {
		return
	}
	c.guestExec = deps.GuestExecutorFactory(c.id)
}

func (c *Container) DeleteState(ctx context.Context) error {
	repo, err := c.stateRepositoryChecked()
	if err != nil {
		return err
	}
	return repo.DeleteContainer(ctx, c.id, c.containerPath, c.legacyBundleStatePath())
}

func (c *Container) legacyBundleStatePath() string {
	if c == nil || c.containerPath == "" {
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Warnf("failed to get current working directory: %v", err)
		return ""
	}
	return filepath.Join(cwd, c.containerPath, defs.MicrunContainerStateFile)
}
