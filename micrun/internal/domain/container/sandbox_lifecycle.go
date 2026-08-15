package container

import (
	"context"
	"errors"
	"fmt"

	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"

	"github.com/hashicorp/go-multierror"
)

func (s *Sandbox) Start(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()

	cur := s.GetState()
	log.Debugf("current sandbox state=%s", cur)

	if cur == StateCreating {
		if err := s.setSandboxState(StateReady); err != nil {
			return err
		}
		cur = s.GetState()
	}

	if cur == StateRunning {
		return s.restartContainersIfStopped(ctx)
	}
	return s.transitionToRunning(ctx, cur)
}

func (s *Sandbox) restartContainersIfStopped(ctx context.Context) (retErr error) {
	log.Debugf("sandbox %s already running, checking containers", s.id)
	containers, err := s.lifecycleContainers()
	if err != nil {
		return err
	}
	// Track successfully (re)started containers so that a failure mid-loop
	// stops them — mirroring transitionToRunning's rollback. Without this, a
	// start failure on container N would leave containers 0..N-1 with live Xen
	// domains in a sandbox the caller believes failed to start, and
	// StoreSandbox would be skipped (disk never updated).
	started := make([]*Container, 0, len(containers))
	defer func() {
		if retErr == nil {
			return
		}
		for _, c := range started {
			// Use WithoutCancel so a Start RPC timeout does not prevent
			// rolling back the domains we just brought up — mirrors
			// stopLifecycleTask's context detachment.
			if err := c.stop(context.WithoutCancel(ctx), true); err != nil {
				log.Warnf("failed to rollback started container %s: %v", c.id, err)
			}
		}
	}()
	for _, c := range containers {
		state, err := c.checkStateWithContext(ctx)
		if err != nil {
			return err
		}
		if state != StateRunning && state != StatePaused {
			if err := c.start(ctx); err != nil {
				return err
			}
			started = append(started, c)
		}
	}
	return s.StoreSandbox(ctx)
}

func (s *Sandbox) transitionToRunning(ctx context.Context, cur StateString) (retErr error) {
	containers, err := s.lifecycleContainers()
	if err != nil {
		return err
	}
	if err := s.transitionState(cur, StateRunning); err != nil {
		log.Debugf("transition error: from=%s to=%s", cur, StateRunning)
		return err
	}

	oldState := cur
	if err := s.setSandboxState(StateRunning); err != nil {
		return fmt.Errorf("set Sandbox state error: %w", err)
	}
	log.Debugf("sandbox state: %s -> %s", oldState, s.GetState())

	started := make([]*Container, 0, len(containers))
	defer func() {
		if retErr == nil {
			return
		}
		// Roll back the sandbox state and stop any containers that were
		// successfully started before the failure, so their Xen domains are
		// not left running in an orphaned state. This also covers the final
		// StoreSandbox failure (e.g. empty InfraOnly sandbox where it is the
		// sole persistence point): without this, the in-memory state would be
		// Running while disk retains oldState.
		if rollbackErr := s.setSandboxState(oldState); rollbackErr != nil {
			log.Warnf("failed to rollback sandbox state to %s: %v", oldState, rollbackErr)
		}
		for _, c := range started {
			// Use WithoutCancel so a Start RPC timeout does not prevent
			// rolling back the domains we just brought up — mirrors
			// stopLifecycleTask's context detachment.
			if err := c.stop(context.WithoutCancel(ctx), true); err != nil {
				log.Warnf("failed to rollback started container %s: %v", c.id, err)
			}
		}
	}()

	for _, c := range containers {
		if retErr = c.start(ctx); retErr != nil {
			return retErr
		}
		started = append(started, c)
	}
	retErr = s.StoreSandbox(ctx)
	return retErr
}

func (s *Sandbox) Stop(ctx context.Context, force bool) error {
	if s == nil {
		return er.SandboxNotFound
	}
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()

	if s.GetState() == StateStopped && s.allContainersTerminal() {
		return nil
	}

	originalState := s.GetState()

	// If the sandbox is already Stopped (a previous Stop pushed the state but
	// left some containers non-terminal due to a partial failure), skip the
	// transition guard — validTransitions has no Stopped→Stopped self-loop,
	// so transitionState would reject the reentry and permanently wedge Delete.
	// Continue to stopContainers to finish any remaining containers.
	if originalState != StateStopped {
		if err := s.transitionState(originalState, StateStopped); err != nil {
			return err
		}
	}

	var stopErr error
	if err := s.stopContainers(ctx, force); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("stop containers: %w", err))
	}

	log.Debug("stop monitor and console")

	// Once containers are stopped their Xen domains are destroyed and cannot
	// be recovered, so the sandbox is effectively stopped regardless of whether
	// network cleanup succeeds. Treat removeNetwork failure as best-effort
	// (networkCleaned already makes it idempotent for the next Delete) instead
	// of rolling the state back to a misleading "running" state.
	if err := s.removeNetwork(); err != nil && !force {
		log.Warnf("failed to remove network for sandbox %s (best-effort): %v", s.id, err)
	}

	if err := s.setSandboxState(StateStopped); err != nil {
		return err
	}
	log.Debugf("sandbox state: %s -> %s", originalState, StateStopped)

	if err := s.StoreSandbox(ctx); err != nil {
		return errors.Join(fmt.Errorf("save sandbox state during Stop: %w", err), stopErr)
	}

	return stopErr
}

func (s *Sandbox) Delete(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()

	switch cur := s.GetState(); cur {
	case StateReady, StatePaused, StateStopped:
		// allowed to delete
	default:
		return er.SandboxNotReady
	}

	var result *multierror.Error
	containers, err := s.lifecycleContainers()
	if err != nil {
		// Defensive: with an unreadable container list, cleaning the sandbox
		// storage would orphan any live guest domain that the failed listing
		// hid. Treat it like a container-delete failure and keep the state.
		result = multierror.Append(result, err)
		return result.ErrorOrNil()
	}
	containerDeleteFailed := false
	for _, c := range containers {
		if err := c.delete(ctx); err != nil {
			if errors.Is(err, er.ContainerNotFound) {
				// A pod container's own Delete RPC raced this sandbox-level
				// sweep and fully removed it first (snapshot-vs-map race):
				// already-gone is success for a teardown loop, not a failure
				// that would abort sandbox cleanup and leak the persisted
				// state plus the netns holder.
				log.Debugf("container %s already removed by a concurrent delete", c.id)
				continue
			}
			log.Errorf("failed to delete container %s: %v", c.id, err)
			result = multierror.Append(result, fmt.Errorf("delete container %s: %w", c.id, err))
			containerDeleteFailed = true
		}
	}

	// If any container delete failed (e.g. still Running after a partial
	// Stop), keep persisted sandbox state so a later retry / recovery can
	// still find the snapshot and tear down orphan Xen domains. Cleaning
	// storage here would leave a live domain with no metadata.
	if containerDeleteFailed {
		return result.ErrorOrNil()
	}

	if err := s.removeNetwork(); err != nil {
		log.Warnf("failed to remove network for sandbox %s: %v", s.id, err)
		result = multierror.Append(result, fmt.Errorf("remove sandbox network: %w", err))
	}

	if err := s.cleanSandboxStorage(ctx); err != nil {
		result = multierror.Append(result, err)
	}
	return result.ErrorOrNil()
}

func (s *Sandbox) removeNetwork() error {
	if s == nil {
		return er.SandboxNotFound
	}
	if s.config == nil {
		return nil
	}

	// Idempotent: Stop() and Delete() can both call removeNetwork().
	if s.networkCleaned {
		return nil
	}

	log.Infof("remove network for sandbox %s", s.id)
	// NetworkCleanup mutates shared NetworkConfig fields; hold containersLock
	// so snapshot readers (networkStorageFromSandbox under RLock) never see
	// a torn/half-cleared network config. The netns.Cleanup inside may take
	// up to its shutdown timeout, acceptable for the rare teardown path.
	s.containersLock.Lock()
	err := s.config.NetworkConfig.NetworkCleanup(s.id)
	s.containersLock.Unlock()
	if err != nil {
		return err
	}
	s.networkCleaned = true
	return nil
}

func (s *Sandbox) stopContainers(ctx context.Context, force bool) error {
	if s == nil {
		return er.SandboxNotFound
	}
	log.Infof("stopping client os in sandbox %s", s.id)
	containers, err := s.lifecycleContainers()
	if err != nil {
		return err
	}
	// Best-effort: try to stop every container even if an earlier one fails.
	// A stopped container's Xen domain is destroyed and cannot be recovered,
	// so returning early would leave remaining containers running while the
	// caller believes the stop succeeded. Collect all errors and aggregate.
	var result *multierror.Error
	for _, c := range containers {
		if err := c.stop(ctx, force); err != nil {
			log.Errorf("failed to stop container %s: %v", c.id, err)
			result = multierror.Append(result, fmt.Errorf("stop container %s: %w", c.id, err))
		}
	}
	return result.ErrorOrNil()
}

func (s *Sandbox) lifecycleContainers() ([]*Container, error) {
	entries, err := s.containerEntries()
	if err != nil {
		return nil, err
	}

	containers := make([]*Container, 0, len(entries))
	for _, entry := range entries {
		containers = append(containers, entry.container)
	}
	return containers, nil
}

// allContainersTerminal reports whether every live container is in a
// terminal state (Stopped/Down). Used to decide whether a re-entry into
// Stop can short-circuit: a previous Stop that partially failed leaves
// containers running, so returning early would strand their domains and
// wedge the later Delete (c.delete refuses non-terminal states).
func (s *Sandbox) allContainersTerminal() bool {
	if s == nil {
		return true
	}
	s.containersLock.RLock()
	defer s.containersLock.RUnlock()
	for _, c := range s.containers {
		if c == nil {
			continue
		}
		st := lockutil.WithReadLockValue(&c.stateMu, func() StateString { return c.state.State })
		if st != StateStopped && st != StateDown {
			return false
		}
	}
	return true
}
