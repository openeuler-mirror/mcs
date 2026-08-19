package container

import (
	"context"
	"fmt"

	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/perf"
)

func (c *Container) ensureClientPresence() (StateString, error) {
	if c == nil {
		return StateDown, er.ContainerNotFound
	}
	return c.ensureClientPresenceWithContext(c.ctx)
}

func (c *Container) ensureClientPresenceWithContext(ctx context.Context) (StateString, error) {
	if c == nil {
		return StateDown, er.ContainerNotFound
	}
	ctx = queryContext(ctx)
	state, err := c.checkStateWithContext(ctx)
	if err != nil {
		return StateDown, err
	}
	log.Tracef("ensureClientPresence: container %s state=%s shouldPresent=%v", c.id, state, c.shouldPresent())
	if state != StateDown {
		return state, nil
	}

	if c.shouldPresent() {
		if err := c.requireGuestControl(); err != nil {
			return StateDown, err
		}
		// Hold registrationMu across the Remove+registerClient sequence so
		// two concurrent callers (e.g. kubelet retry + internal restart)
		// cannot each CreateGuest and leak a duplicate Xen domain.
		c.registrationMu.Lock()
		// Always Remove before re-register when state is Down: after micad
		// restart the control socket is gone (Exists=false) but the Xen
		// domain may still be alive — CreateGuest would collide. Remove
		// destroys a lingering domain (or is a no-op when nothing remains).
		if rmErr := c.sandbox.guestControl.Remove(ctx, c.id); rmErr != nil {
			c.registrationMu.Unlock()
			return StateDown, fmt.Errorf("failed to remove stale guest for %s before registration: %w", c.id, rmErr)
		}
		err = c.registerClient(ctx)
		c.registrationMu.Unlock()
		if err != nil {
			return StateDown, err
		}
	}

	state, err = c.checkStateWithContext(ctx)
	if err != nil {
		return StateDown, err
	}
	log.Tracef("ensureClientPresence: after registration, container %s state=%s", c.id, state)
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

func (c *Container) registerClient(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.guestExec == nil {
		return er.FactoryNotConfigured
	}
	conf, err := createMicaClientConf(c)
	if err != nil {
		return err
	}

	deps, err := c.sandbox.dependenciesChecked()
	if err != nil {
		return err
	}
	perfT := perf.Start(c.clock(), "register_client", c.id)
	if err := deps.CreateGuest(ctx, conf); err != nil {
		log.Errorf("registerClient: CreateGuest failed: %v", err)
		return err
	}
	perfT.Stage("create_guest")

	// Read mutable config fields under containersLock to avoid racing with a
	// concurrent UpdateContainer writing Resources.Memory.Limit.
	c.sandbox.containersLock.RLock()
	limit := c.config.memoryLimitMB()
	initialMemReservation := c.config.memoryReservationMB()
	c.sandbox.containersLock.RUnlock()
	initialMem := limit
	if initialMem == 0 {
		initialMem = initialMemReservation
	}
	if initialMem == 0 {
		initialMem = defs.DefaultMinMemMB
	}
	recordThreshold := limit
	if recordThreshold == 0 {
		recordThreshold = initialMem
	}
	c.guestExec.RecordMemoryState(initialMem, recordThreshold)

	perfT.Stage("initial_config")
	if err := c.setContainerState(ctx, StateReady); err != nil {
		// The Xen domain was already created; roll it back so a retry does not
		// find a stale domain that blocks re-registration. Detach from a
		// canceled Start/Create ctx so Remove can still complete.
		rollbackCtx := context.WithoutCancel(ctx)
		if rErr := c.sandbox.guestControl.Remove(rollbackCtx, c.id); rErr != nil {
			log.Warnf("failed to roll back guest %s after state persistence error: %v", c.id, rErr)
		}
		return err
	}
	return nil
}

func (c *Container) GetClientCPU() string {
	if c.cpuUnset() {
		return ""
	}
	return c.config.cpuMask()
}
