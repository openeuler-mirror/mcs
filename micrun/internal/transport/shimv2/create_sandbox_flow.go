package shim

import (
	"context"
	"fmt"

	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
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
	Delete(ctx context.Context) error
}

func reconcileExistingSandbox(ctx context.Context, runtime *shimService) error {
	sandbox, _ := runtime.currentSandbox()
	clearSandbox, err := reconcileSandbox(ctx, sandbox, runtime.runtimeDeps.hypervisor)
	if clearSandbox {
		runtime.clearSandbox()
	}
	return err
}

func reconcileSandbox(ctx context.Context, sandbox sandboxLifecycle, hypervisor ports.HypervisorControl) (bool, error) {
	if validation.IsNil(sandbox) {
		return false, nil
	}
	if validation.IsNil(hypervisor) {
		return false, fmt.Errorf("hypervisor control is required")
	}

	sandboxState := sandbox.GetState()
	log.Infof("[SHIM] Existing sandbox found: id=%s, state=%v", sandbox.SandboxID(), sandboxState)

	switch sandboxState {
	case cntr.StateRunning:
		xenState, err := hypervisor.DomainState(ctx, sandbox.SandboxID())
		guestRunning := err == nil && xenState == "running"
		log.Infof("[SHIM] XEN guest check: id=%s, xenState=%q, running=%v", sandbox.SandboxID(), xenState, guestRunning)

		if guestRunning {
			return false, fmt.Errorf("cannot create an existing sandbox: %s (state=%v, XEN guest is running)", sandbox.SandboxID(), sandboxState)
		}

		log.Warnf("[SHIM] Sandbox %s state=RUNNING but XEN guest is down (xenState=%q), cleaning up", sandbox.SandboxID(), xenState)
		if err := sandbox.Delete(ctx); err != nil {
			return false, fmt.Errorf("delete stale sandbox %s: %w", sandbox.SandboxID(), err)
		}
		return true, nil

	case cntr.StateStopped:
		log.Infof("[SHIM] Previous sandbox %s is STOPPED, cleaning up for restart", sandbox.SandboxID())
		if err := sandbox.Delete(ctx); err != nil {
			return false, fmt.Errorf("delete stopped sandbox %s: %w", sandbox.SandboxID(), err)
		}
		return true, nil

	default:
		return false, fmt.Errorf("cannot create an existing sandbox: %s (state=%v)", sandbox.SandboxID(), sandboxState)
	}
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
