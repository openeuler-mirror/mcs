package container

import (
	"context"
	"errors"
	"fmt"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"

	"golang.org/x/sys/unix"
)

func loadSandbox(ctx context.Context, id string, guestCtl ports.GuestControl, deps *Dependencies) (sandbox *Sandbox, err error) {
	if deps == nil {
		return nil, fmt.Errorf("loadSandbox requires non-nil dependencies")
	}
	if guestCtl == nil {
		return nil, fmt.Errorf("loadSandbox requires non-nil guest control")
	}
	if id == "" {
		return nil, er.EmptySandboxID
	}

	repo, err := stateRepositoryFromDependenciesChecked(deps)
	if err != nil {
		return nil, err
	}
	ss, err := repo.LoadSandbox(ctx, id)
	if err != nil {
		log.Debugf("failed to restore sandbox from disk: %v.", err)
		return nil, err
	}

	if err := validateOrCleanup(ctx, id, ss, guestCtl, repo); err != nil {
		return nil, err
	}

	return rebuildSandbox(ctx, ss, guestCtl, deps, repo)
}

func validateOrCleanup(ctx context.Context, id string, ss *SandboxStorage, guestCtl ports.GuestControl, repo stateRepository) error {
	result, err := validateSandboxState(ctx, id, ss, guestCtl)
	if err != nil {
		return fmt.Errorf("sandbox state validation failed: %w", err)
	}
	if result.Cleanup {
		log.Infof("Cleaning up stale sandbox state for %s", id)
		if removeErr := repo.DeleteSandbox(ctx, id); removeErr != nil {
			log.Errorf("failed to remove persisted sandbox state for %s: %v", id, removeErr)
		}
		return er.SandboxNotFound
	}
	if !result.Valid {
		return fmt.Errorf("sandbox state is not valid for restoration")
	}
	return nil
}

func rebuildSandbox(ctx context.Context, ss *SandboxStorage, guestCtl ports.GuestControl, deps *Dependencies, repo stateRepository) (*Sandbox, error) {
	c := ss.Config
	c.StateStore = repo.store
	c.Dependencies = deps
	if c.GuestControl == nil {
		c.GuestControl = guestCtl
	}
	if c.HypervisorControl == nil {
		c.HypervisorControl = deps.DefaultHypervisorControl()
	}

	sandbox, err := createSandbox(ctx, &c)
	if err != nil {
		log.Errorf("failed to create sandbox: %v.", err)
		return nil, err
	}
	if err := sandbox.loadContainersToSandbox(ctx); err != nil {
		return nil, err
	}
	return sandbox, nil
}

func LoadSandboxWithDependencies(ctx context.Context, id string, guestCtl ports.GuestControl, deps *Dependencies) (sandbox *Sandbox, err error) {
	return loadSandbox(ctx, id, guestCtl, deps)
}

func processExists(pid int) bool {
	err := unix.Kill(pid, 0)
	return processExistsAfterSignalCheck(pid, err)
}

func processExistsAfterSignalCheck(pid int, err error) bool {
	if pid <= 0 {
		return false
	}
	return err == nil || errors.Is(err, unix.EPERM)
}

type sandboxStateValidation struct {
	Valid   bool
	Cleanup bool
}

func validateSandboxState(ctx context.Context, id string, storage *SandboxStorage, guestCtl ports.GuestControl) (sandboxStateValidation, error) {
	if storage == nil {
		return sandboxStateValidation{}, fmt.Errorf("nil storage")
	}
	if guestCtl == nil {
		return sandboxStateValidation{}, fmt.Errorf("guest control is required")
	}
	if !storage.State.Valid() {
		return sandboxStateValidation{}, fmt.Errorf("sandbox state invalid: %s", storage.State.State)
	}

	if err := checkShimCollision(id, storage.ShimPID); err != nil {
		return sandboxStateValidation{}, err
	}

	stale, err := isRTOSClientStale(ctx, id, guestCtl)
	if err != nil {
		return sandboxStateValidation{}, err
	}
	if stale {
		return sandboxStateValidation{Cleanup: true}, nil
	}

	compareStoredAndLiveState(ctx, id, storage.State.State, guestCtl)
	return sandboxStateValidation{Valid: true}, nil
}

func checkShimCollision(id string, shimPID int) error {
	if shimPID <= 0 {
		return nil
	}
	if processExists(shimPID) {
		log.Debugf("Sandbox %s: shim PID %d still running, another instance may be active", id, shimPID)
		return fmt.Errorf("another shim instance (PID %d) is already running", shimPID)
	}
	log.Infof("Sandbox %s: shim PID %d is dead, validating RTOS state", id, shimPID)
	return nil
}

func isRTOSClientStale(ctx context.Context, id string, guestCtl ports.GuestControl) (bool, error) {
	exists, err := guestCtl.Exists(ctx, id)
	if err != nil {
		return false, err
	}
	if !exists {
		log.Infof("Sandbox %s: RTOS client not found in micad, persisted state is stale", id)
		return true, nil
	}
	return false, nil
}

func compareStoredAndLiveState(ctx context.Context, id string, storageState StateString, guestCtl ports.GuestControl) {
	status, err := guestCtl.Status(ctx, id)
	if err != nil {
		log.Warnf("Sandbox %s: failed to query RTOS status: %v", id, err)
		return
	}
	if storageState == StateRunning && status.Stopped {
		log.Warnf("Sandbox %s: state mismatch, file=running, actual=%s", id, status.State)
	} else if storageState == StateStopped && status.Running {
		log.Warnf("Sandbox %s: state mismatch, file=stopped, actual=%s", id, status.State)
	}
}
