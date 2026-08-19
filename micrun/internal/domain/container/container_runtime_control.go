package container

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"micrun/internal/adapters/hypervisor/pedestal"
	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
	"micrun/internal/support/fs"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
)

func (c *Container) start(ctx context.Context) error {
	// Serialize the whole start sequence (ensureClientPresence + guest
	// start): without it, two concurrent Starts both observe state==Down in
	// ensureClientPresence, and the orphan-domain cleanup there can destroy
	// the healthy domain the winner just registered. startGuest runs under
	// the same lock (see below).
	c.startMu.Lock()
	defer c.startMu.Unlock()

	currentState, err := c.ensureClientPresenceWithContext(ctx)
	if err != nil {
		return err
	}

	if c.isInfra() {
		return c.startInfra(ctx, currentState)
	}
	return c.startGuest(ctx, currentState)
}

func (c *Container) startInfra(ctx context.Context, currentState StateString) error {
	if currentState == StateRunning {
		return nil
	}
	if !canStartFrom(currentState) {
		return er.ContainerNotReady
	}
	if err := c.transitionState(currentState, StateRunning); err != nil {
		return err
	}
	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) startGuest(ctx context.Context, currentState StateString) error {
	// Serialized by start()'s startMu: two concurrent Starts cannot both
	// pass the state check, double-start the guest, or have the loser's
	// failure rollback destroy the winner's domain. Inside the lock the
	// rollback on failure is unconditional — there can be no concurrent
	// winner.

	if currentState == StateRunning {
		return fmt.Errorf("container %s is already running", c.id)
	}
	if !canStartFrom(currentState) {
		return er.ContainerNotReady
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}
	if err := c.transitionState(currentState, StateRunning); err != nil {
		return err
	}
	if err := startClient(ctx, c); err != nil {
		log.Warnf("failed to start container: %v, stopping it", err)
		// Start RPC may already have canceled ctx; rollback must still tear
		// down a partially started domain (same as pin/persist rollback).
		rollbackCtx := context.WithoutCancel(ctx)
		if stopErr := c.stopUnlocked(rollbackCtx, true); stopErr != nil {
			log.Warn("failed to stop the container after start failed.")
			return errors.Join(err, fmt.Errorf("stop container after start failure: %w", stopErr))
		}
		return err
	}
	if err := c.setContainerState(ctx, StateRunning); err != nil {
		// The guest is up but the state could not be persisted: roll the
		// domain back so the in-memory/disc/hypervisor states stay
		// consistent (a retry would otherwise double-start the running
		// domain).
		log.Warnf("failed to persist running state for %s, stopping it: %v", c.id, err)
		rollbackCtx := context.WithoutCancel(ctx)
		if stopErr := c.stopUnlocked(rollbackCtx, true); stopErr != nil {
			return errors.Join(err, fmt.Errorf("stop container after state persistence failure: %w", stopErr))
		}
		return err
	}
	return nil
}

func canStartFrom(state StateString) bool {
	return state == StateReady || state == StateStopped
}

// transitionState validates a container state transition under stateMu.
// The underlying Transition reads c.state.State; without the lock it races
// with setContainerState writers (a string is a non-atomic pointer+length).
func (c *Container) transitionState(old, new StateString) error {
	return lockutil.WithReadLockValue(&c.stateMu, func() error {
		return c.state.Transition(old, new)
	})
}

func (c *Container) create(ctx context.Context) error {
	if c.isInfra() {
		return c.setContainerState(ctx, StateReady)
	}

	if _, err := c.ensureClientPresenceWithContext(ctx); err != nil {
		return err
	}

	return c.setContainerState(ctx, StateReady)
}

func (c *Container) doStop(ctx context.Context, force bool) error {
	if c.isInfra() {
		// Infra (pause) container has no guest domain to stop.
		return nil
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}

	currentState, err := c.checkStateWithContext(ctx)
	if err != nil {
		return err
	}
	// StateDown only means the micad control SOCKET is gone (Exists is
	// socket-only, invariant 3): the Xen domain may still be alive after a
	// micad crash/restart. Do NOT short-circuit — guestControl.Stop routes
	// to destroyLingeringDomain and is a no-op when the domain is truly
	// gone. Skipping it here is what orphaned domains.
	// Skip the state transition when already Stopped: the state machine
	// forbids Stopped→Stopped, but the domain may still exist and needs
	// guestControl.Stop. Only transition from non-terminal states.
	if currentState != StateStopped {
		if err := c.transitionState(currentState, StateStopped); err != nil && !force {
			return err
		}
	}
	return c.sandbox.guestControl.Stop(ctx, c.ID())
}

// stop tears the guest down. It shares startMu with Start so a Kill/Stop
// cannot destroy a domain that Start is still building (and Start's second
// presence check cannot re-register a new domain after a Kill returned) —
// the same serialization two concurrent Starts already get (scan 2.2).
func (c *Container) stop(ctx context.Context, force bool) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	return c.stopUnlocked(ctx, force)
}

// stopUnlocked is stop() for callers already holding startMu (the Start
// failure rollbacks in startGuest run inside the start critical section).
func (c *Container) stopUnlocked(ctx context.Context, force bool) error {
	// doStop uses checkStateWithContext (not ensureClientPresenceWithContext):
	// if the guest domain is already gone, ensureClientPresence would
	// re-create it just so we can destroy it again — an unnecessary domain
	// create/destroy cycle that can fail and wedge sandbox teardown. When
	// the guest socket is gone (StateDown), doStop still tears down any
	// lingering Xen domain via the adapter (destroyLingeringDomain); Down
	// must not short-circuit to Stopped or the domain is orphaned.
	if err := c.doStop(ctx, force); err != nil {
		if force && isBenignForcedStopError(err) {
			log.Debugf("container %s was already exiting during forced stop: %v", c.id, err)
			return c.setContainerState(ctx, StateStopped)
		}
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	if err := c.setContainerState(ctx, StateStopped); err != nil {
		// The domain is already destroyed but the state could not be
		// persisted: force convergence via checkStateWithContext. If the
		// domain is truly gone it marks the container Down (closing the
		// exit notifier so waiters unblock); if it still exists (unusual),
		// the in-memory Stopped state is kept and a later start re-checks
		// the real state. Without this, the container stays Running in
		// memory while its domain is gone.
		if _, convErr := c.checkStateWithContext(ctx); convErr != nil {
			return errors.Join(err, fmt.Errorf("converge state after stop persistence failure: %w", convErr))
		}
		return err
	}
	return nil
}

func isBenignForcedStopError(err error) bool {
	return errors.Is(err, syscall.ECONNRESET)
}

// kill shares startMu with Start for the same reason stop does: a Kill in
// Start's window must wait instead of racing the domain build (scan 2.2).
// Kill deliberately stays OUT of claimLifecycle — a claim failure returns
// Unavailable, which kubelet would retry rather than tear down.
func (c *Container) kill(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	return c.killUnlocked(ctx)
}

func (c *Container) killUnlocked(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.notOperational() {
		return er.SandboxNotReady
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}

	currentState, err := c.checkStateWithContext(ctx)
	if err != nil {
		return err
	}
	log.Debugf("Container state is %s.", currentState)
	// StateDown (socket gone) must still flow into doStop: the Xen domain
	// may be alive after a micad crash and needs teardown via the adapter's
	// destroyLingeringDomain. The redundant Exists probe is gone — doStop's
	// guestControl.Stop handles both socket-present and socket-absent.
	if err := c.doStop(ctx, true); err != nil {
		if isBenignForcedStopError(err) {
			log.Debugf("container %s was already exiting during kill: %v", c.id, err)
			return c.setContainerState(ctx, StateStopped)
		}
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	if err := c.setContainerState(ctx, StateStopped); err != nil {
		// Mirror the stop path: the domain is already destroyed but the
		// Stopped state could not be persisted. Force convergence via
		// checkStateWithContext so the container is marked Down (closing the
		// exit notifier) instead of staying Running in memory while its
		// domain is gone.
		if _, convErr := c.checkStateWithContext(ctx); convErr != nil {
			return errors.Join(err, fmt.Errorf("converge state after kill persistence failure: %w", convErr))
		}
		return err
	}
	return nil
}

func (c *Container) delete(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	// Use checkStateWithContext (not ensureClientPresenceWithContext): a
	// missing guest domain must not be re-created just so we can delete it.
	currentState, err := c.checkStateWithContext(ctx)
	if err != nil {
		return err
	}
	if !canDeleteFrom(currentState) {
		return er.ContainerNotReady
	}

	// StateDown means only the micad socket is gone; the Xen domain may
	// still be alive. Remove is attempted regardless: the adapter routes a
	// socket-absent Remove to destroyLingeringDomain (no-op when the domain
	// is truly gone), so Down must not skip it or the domain is orphaned.
	if !c.isInfra() {
		if err := c.requireGuestControl(); err != nil {
			return err
		}
		if err := c.removeGuestClientForDelete(ctx, currentState); err != nil {
			log.Debugf("failed to remove container %s.", err)
			return err
		}
	}
	return c.cleanupAfterDelete(ctx)
}

func (c *Container) removeGuestClientForDelete(ctx context.Context, currentState StateString) error {
	// Do not clamp Stopped removes to a short timeout: MRemove uses
	// MicaSocketLongTimeout (30s). A 2s budget commonly timed out after a
	// force-stop that only saw ECONNRESET, swallowing the error and leaving
	// the Xen domain behind while containerd reported Delete success.
	if err := c.sandbox.guestControl.Remove(ctx, c.id); err != nil {
		if currentState == StateStopped {
			exists, existsErr := c.sandbox.guestControl.Exists(ctx, c.id)
			if existsErr == nil && !exists {
				// The socket vanishing together with a failed remove (micad
				// died mid-MRemove) is exactly when the domain's fate is
				// unknown: a socket-only probe cannot see a domain left
				// behind by the failed destroy. Cross-check at the
				// hypervisor layer before granting the exemption.
				return c.confirmDomainGoneForDeleteExemption(ctx, "stopped", err)
			}
			return fmt.Errorf("remove stopped container %s: %w", c.id, err)
		}
		if currentState == StateDown {
			// Down means the micad socket is already gone, so a socket-only
			// Exists probe would "confirm" absence unconditionally and mask
			// a real destroyLingeringDomain failure (leaking a live Xen
			// domain while reporting delete success). Cross-check at the
			// hypervisor layer instead: only a definitive not-found there
			// earns the exemption.
			return c.confirmDomainGoneForDeleteExemption(ctx, "down", err)
		}
		return err
	}
	return nil
}

// confirmDomainGoneForDeleteExemption grants the failed-remove exemption
// only when the hypervisor layer confirms the domain is gone. A live
// domain (probe succeeds) or an inconclusive probe keeps the error so the
// caller retries instead of orphaning the domain behind a successful
// delete.
func (c *Container) confirmDomainGoneForDeleteExemption(ctx context.Context, stateName string, removeErr error) error {
	hyp := c.sandbox.hypervisorControl
	if hyp == nil {
		return fmt.Errorf("remove %s container %s: %w", stateName, c.id, removeErr)
	}
	if _, stateErr := hyp.DomainState(ctx, c.id); stateErr == nil {
		return fmt.Errorf("remove %s container %s: domain still alive after remove failure: %w", stateName, c.id, removeErr)
	} else if !isMissingDomainError(stateErr) && !errors.Is(stateErr, pedestal.ErrNotSupported) {
		return fmt.Errorf("remove %s container %s (domain state probe failed): %w", stateName, c.id, removeErr)
	}
	return nil
}

func (c *Container) cleanupAfterDelete(ctx context.Context) error {
	if err := c.sandbox.removeContainer(c.id); err != nil {
		return err
	}
	// Remove the container config BEFORE StoreSandbox so the persisted
	// snapshot never references a container whose per-container state is
	// about to be deleted. Without this, a later persistSandboxState failure
	// would leave the on-disk snapshot containing the deleted container's
	// config while its state file is gone — on restart the sandbox would
	// re-add a phantom container that can't be created.
	c.sandbox.removeContainerConfig(c.id)
	var cleanupErr error
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		// Roll back the in-memory removal so a retry can find the container
		// again; otherwise it would be orphaned in neither the map nor disk.
		// Restore BOTH the containers map and ContainerConfigs so the two
		// stay in sync. Do NOT proceed to delete per-container state files:
		// the on-disk sandbox snapshot still references this container (the
		// StoreSandbox write failed), so removing the container state would
		// create a memory/disk inconsistency on the next restore.
		if rbErr := c.sandbox.addContainer(c); rbErr != nil {
			log.Warnf("failed to roll back in-memory container %s after StoreSandbox failure: %v", c.id, rbErr)
		}
		c.sandbox.restoreContainerConfig(c.id, c.config)
		return errors.Join(cleanupErr, fmt.Errorf("failed to store sandbox after deleting container %s: %w", c.id, err))
	}
	// Container state lives in the combined sandbox document, and this
	// container is already out of the containers map: a late persist from an
	// in-flight Kill/State probe writes a document WITHOUT this container's
	// record, so resurrection is impossible by construction. DeleteState only
	// removes the legacy per-container files (pre-combined-format leftovers).
	if err := c.DeleteState(ctx); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failed to delete container state: %w", err))
	}
	if err := fs.RemoveContainerCacheDirAt(c.cacheRoot(), c.id); err != nil {
		log.Warnf("failed to remove cache directory for container %s: %v", c.id, err)
	}
	return cleanupErr
}

func (c *Container) cacheRoot() string {
	if c != nil && c.config != nil && c.config.CacheRoot != "" {
		return c.config.CacheRoot
	}
	return defs.DefaultMicaContainersRoot
}

// canDeleteFrom reports whether a container may be deleted from state.
// StateDown is included: the guest domain is already gone and delete must
// only clean up persisted state (crash-recovery always deletes via Down).
func canDeleteFrom(state StateString) bool {
	return state == StateDown || state == StateReady || state == StatePaused || state == StateStopped
}

func (c *Container) isInfra() bool {
	return c.config != nil && c.config.IsInfra
}

func (c *Container) pause(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.notOperational() {
		return er.SandboxNotReady
	}
	// checkStateWithContext: a missing guest must not be re-created by a
	// pause; the operation should fail with ContainerNotRunning instead.
	currentState, err := c.checkStateWithContext(ctx)
	if err != nil {
		return err
	}
	if currentState != StateRunning {
		return er.ContainerNotRunning
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StatePaused)
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}
	if err := c.sandbox.guestControl.Pause(ctx, c.id); err != nil {
		return fmt.Errorf("pause container %s: %w", c.id, err)
	}
	if err := c.setContainerState(ctx, StatePaused); err != nil {
		// Guest is paused but state could not be persisted: roll the guest
		// back so memory/disk do not diverge permanently (Pause would then
		// see not-running, Resume would see not-paused).
		rollbackCtx := context.WithoutCancel(ctx)
		if resumeErr := c.sandbox.guestControl.Resume(rollbackCtx, c.id); resumeErr != nil {
			return errors.Join(err, fmt.Errorf("resume container %s after state persistence failure: %w", c.id, resumeErr))
		}
		return err
	}
	return nil
}

func (c *Container) resume(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.notOperational() {
		return er.SandboxNotReady
	}

	// checkStateWithContext: a missing guest must not be re-created by a
	// resume; the operation should fail with ContainerNotPaused instead.
	// Note: resuming from StateStopped (restarting a destroyed domain) is
	// intentionally not supported — stop tears the domain down, and a
	// restart is expressed by start, not resume.
	currentState, err := c.checkStateWithContext(ctx)
	if err != nil {
		return err
	}
	if currentState != StatePaused {
		return er.ContainerNotPaused
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StateRunning)
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}

	log.Debugf("resuming container %s (restarting RTOS)", c.id)
	if err := c.sandbox.guestControl.Resume(ctx, c.id); err != nil {
		return fmt.Errorf("resume container %s: %w", c.id, err)
	}
	if err := c.setContainerState(ctx, StateRunning); err != nil {
		// Symmetric with pause: guest is running again but state persist
		// failed — re-pause so subsequent Resume is still valid.
		rollbackCtx := context.WithoutCancel(ctx)
		if pauseErr := c.sandbox.guestControl.Pause(rollbackCtx, c.id); pauseErr != nil {
			return errors.Join(err, fmt.Errorf("pause container %s after state persistence failure: %w", c.id, pauseErr))
		}
		return err
	}
	return nil
}

// Signal is a ContainerTraits interface method. RTOS guests have no POSIX
// signal mechanism; the Kill RPC routes through a dedicated classification
// path (service_signals.go), not here. Return NotSupported rather than a
// misleading nil so any caller knows the signal was not delivered.
func (c *Container) Signal(_ context.Context, _ syscall.Signal) error {
	return er.NotSupported
}

func (c *Container) requireGuestControl() error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.guestControl == nil {
		return fmt.Errorf("guest control is nil")
	}
	return nil
}
