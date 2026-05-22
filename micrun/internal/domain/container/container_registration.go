package container

import (
	"context"

	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
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
		exists, err := c.sandbox.guestControl.Exists(ctx, c.id)
		if err != nil {
			return StateDown, err
		}
		if !exists {
			log.Tracef("ensureClientPresence: registering client %s", c.id)
			if err := c.registerClient(ctx); err != nil {
				return StateDown, err
			}
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
	if err := deps.CreateGuest(ctx, conf); err != nil {
		log.Errorf("registerClient: CreateGuest failed: %v", err)
		return err
	}

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
	c.guestExec.RecordMemoryState(initialMem, recordThreshold)

	return c.setContainerState(ctx, StateReady)
}

func (c *Container) GetClientCPU() string {
	if c.cpuUnset() {
		return ""
	}
	return c.config.cpuMask()
}
