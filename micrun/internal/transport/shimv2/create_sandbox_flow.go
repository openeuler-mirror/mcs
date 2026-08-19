package shim

import (
	"context"
	"fmt"

	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

type sandboxStorer interface {
	SandboxID() string
	StoreSandbox(ctx context.Context) error
}

type sandboxLifecycle interface {
	SandboxID() string
	GetState() cntr.StateString
	GetAllContainers() []cntr.ContainerTraits
	Stop(ctx context.Context, force bool) error
	Delete(ctx context.Context) error
}

func reconcileExistingSandbox(ctx context.Context, runtime *shimService) error {
	sandbox, _ := runtime.currentSandbox()
	clearSandbox, err := reconcileSandbox(ctx, sandbox, runtime.runtimeDeps.guestControl)
	if clearSandbox {
		runtime.clearSandbox()
	}
	return err
}

func reconcileSandbox(ctx context.Context, sandbox sandboxLifecycle, guestCtl ports.GuestControl) (bool, error) {
	if validation.IsNil(sandbox) {
		return false, nil
	}
	if validation.IsNil(guestCtl) {
		return false, fmt.Errorf("guest control is required")
	}

	sandboxState := sandbox.GetState()
	log.Infof("[SHIM] Existing sandbox found: id=%s, state=%v", sandbox.SandboxID(), sandboxState)

	switch sandboxState {
	case cntr.StateRunning:
		// Probe guest liveness through micad, not hypervisor.DomainState: a
		// CRI pod sandbox never registers a Xen domain under its own id (only
		// workload containers do) and non-Xen pedestals return ErrNotSupported,
		// so the old DomainState check reported "guest down" for a healthy
		// RUNNING sandbox and destroyed it. micad client presence is
		// pedestal-agnostic.
		live, err := sandboxGuestLive(ctx, sandbox, guestCtl)
		if err != nil {
			// Refuse to destroy on uncertainty: tearing down a possibly-live
			// sandbox is far worse than rejecting a duplicate Create. Surface as
			// Unavailable (not Unknown) so containerd/CRI treats it as transient.
			return false, er.Wrap(er.SandboxNotReady, fmt.Sprintf("cannot verify guest liveness for sandbox %s: %v", sandbox.SandboxID(), err))
		}
		if live {
			// A true duplicate: surface AlreadyExists (not Unknown) so CRI can
			// reconcile a Create retry that raced a slow first Create's deadline.
			return false, er.Wrapf(er.AlreadyExists, "cannot create an existing sandbox: %s (state=%v, guest is running)", sandbox.SandboxID(), sandboxState)
		}

		log.Warnf("[SHIM] Sandbox %s state=RUNNING but no live guest client, cleaning up", sandbox.SandboxID())
		// Sandbox.Delete only permits Ready/Paused/Stopped, so a stale
		// RUNNING sandbox must be stopped first (mirrors the one-shot
		// cleanup path which Stops before Deleting).
		// Detach from RPC cancellation: the decision to tear down was already
		// made from the liveness probe above, and the Stop/Delete sequence
		// issues guest RPCs (Exists/Status/Remove) that return ctx.Err()
		// immediately under a canceled ctx — leaving the stale micad clients
		// as orphans and the sandbox in an inconsistent state. Every other
		// teardown path (stopLifecycleTask, createSandboxFromConfig,
		// persistCreatedSandbox) detaches for the same reason.
		teardownCtx := context.WithoutCancel(ctx)
		if err := sandbox.Stop(teardownCtx, true); err != nil {
			return false, fmt.Errorf("stop stale sandbox %s: %w", sandbox.SandboxID(), err)
		}
		if err := sandbox.Delete(teardownCtx); err != nil {
			return false, fmt.Errorf("delete stale sandbox %s: %w", sandbox.SandboxID(), err)
		}
		return true, nil

	case cntr.StateStopped:
		log.Infof("[SHIM] Previous sandbox %s is STOPPED, cleaning up for restart", sandbox.SandboxID())
		// Detach from RPC cancellation: Delete iterates containers and issues
		// guestControl.Remove per container; a canceled ctx aborts mid-loop and
		// leaves live micad clients orphaned (same contract as the RUNNING
		// branch above and stopLifecycleTask).
		if err := sandbox.Delete(context.WithoutCancel(ctx)); err != nil {
			return false, fmt.Errorf("delete stopped sandbox %s: %w", sandbox.SandboxID(), err)
		}
		return true, nil

	default:
		return false, er.Wrapf(er.AlreadyExists, "cannot create an existing sandbox: %s (state=%v)", sandbox.SandboxID(), sandboxState)
	}
}

// sandboxGuestLive reports whether any RTOS client belonging to the sandbox is
// present in micad and not stopped. A client that exists but is stopped (mica
// stop removes the client; a crashed domain reports stopped) counts as not
// live. An error is returned when liveness cannot be determined (micad
// unreachable, status query failure) so the caller can refuse destructive
// action on uncertainty.
func sandboxGuestLive(ctx context.Context, sandbox sandboxLifecycle, guestCtl ports.GuestControl) (bool, error) {
	ids, infraOnly := sandboxClientIDs(sandbox)
	if infraOnly {
		// CRI InfraOnly pod: the only container is the infra (pause) container,
		// whose id IS the sandbox id and never registers a mica domain.
		// Probing micad would always report "not live", destroying a healthy
		// pod sandbox on every duplicate Create / kubelet retry. Treat as live
		// so the duplicate Create is rejected instead of destroying the sandbox.
		// (Mirrors the isRTOSClientStale InfraOnly guard in sandbox_loader.go.)
		log.Debugf("[SHIM] Sandbox %s is infra-only, guest liveness cannot be determined from micad; assuming live", sandbox.SandboxID())
		return true, nil
	}
	unknown := false
	paused := pausedContainerIDs(sandbox)
	for _, id := range ids {
		exists, err := guestCtl.Exists(ctx, id)
		if err != nil {
			unknown = true
			continue
		}
		if !exists {
			continue
		}
		status, err := guestCtl.Status(ctx, id)
		if err != nil {
			unknown = true
			continue
		}
		if !status.Stopped {
			return true, nil
		}
		// On non-Xen pedestals Pause is a full mica stop (MPause->MStop), so
		// a paused container's client reports Stopped while perfectly healthy.
		// Count it live when the container's persisted state is Paused —
		// otherwise a duplicate Create would destroy a merely-paused sandbox.
		if paused[id] {
			return true, nil
		}
	}
	if unknown {
		return false, fmt.Errorf("could not query all guest clients (probed %v)", ids)
	}
	return false, nil
}

// pausedContainerIDs returns the ids of containers whose persisted state is
// Paused. Used by sandboxGuestLive to tell an intentionally stopped guest
// (non-Xen Pause == mica stop) apart from a dead one.
func pausedContainerIDs(sandbox sandboxLifecycle) map[string]bool {
	paused := make(map[string]bool)
	for _, c := range sandbox.GetAllContainers() {
		if c == nil || c.ID() == "" {
			continue
		}
		if c.Status() == cntr.StatePaused {
			paused[c.ID()] = true
		}
	}
	return paused
}

// sandboxClientIDs returns the non-infra mica client ids belonging to the
// sandbox, plus a flag indicating whether the sandbox is an InfraOnly pod
// (all containers are infra, or there are none). The infra container id is the
// sandbox id for a CRI pod and never exists in micad, so it is excluded from
// the probe set. For a standalone sandbox (no containers at all) the sandbox id
// IS the client id and infraOnly is false.
func sandboxClientIDs(sandbox sandboxLifecycle) ([]string, bool) {
	containers := sandbox.GetAllContainers()
	ids := make([]string, 0, len(containers))
	hasInfra := false
	for _, c := range containers {
		if c == nil || c.ID() == "" {
			continue
		}
		if c.IsInfra() {
			hasInfra = true
			continue
		}
		ids = append(ids, c.ID())
	}
	if len(ids) > 0 {
		return ids, false
	}
	// No non-infra containers: standalone (hasInfra=false, sandbox id is
	// client id) or CRI InfraOnly pod (hasInfra=true, sandbox id is infra id).
	return []string{sandbox.SandboxID()}, hasInfra
}

func persistCreatedSandbox(ctx context.Context, sandbox sandboxStorer) error {
	if validation.IsNil(sandbox) {
		return fmt.Errorf("sandbox storer is required")
	}
	log.Debugf("storing sandbox state for %s", sandbox.SandboxID())
	if err := sandbox.StoreSandbox(ctx); err != nil {
		log.Warnf("failed to store sandbox state: %v", err)
		return fmt.Errorf("store sandbox state for %s: %w", sandbox.SandboxID(), err)
	}
	log.Debugf("sandbox state stored successfully")
	return nil
}
