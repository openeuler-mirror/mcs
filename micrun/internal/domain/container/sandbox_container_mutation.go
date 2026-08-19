package container

import (
	"context"
	"errors"
	"fmt"

	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
)

func (s *Sandbox) CreateContainer(ctx context.Context, config ContainerConfig) (_ ContainerTraits, err error) {
	id := config.ID
	if s == nil {
		return nil, er.SandboxNotFound
	}
	if id == "" {
		return nil, er.EmptyContainerID
	}
	if s.config == nil {
		return nil, fmt.Errorf("sandbox config is nil")
	}
	// Serialize with Sandbox Stop/Delete (same lifecycleLock). CreateGuest is
	// slow; without this lock an in-flight create is invisible to
	// lifecycleContainers() (config claim only, not yet in s.containers), so
	// Delete finishes storage teardown and then CreateGuest leaves an
	// untracked Xen/micad domain. Holding the lock also lets a create that
	// starts after Stop see notOperational and fail before CreateGuest.
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()
	if s.notOperational() {
		return nil, er.SandboxNotReady
	}

	s.containersLock.Lock()
	if s.containers == nil {
		s.containers = make(map[string]*Container)
	}
	if _, ok := s.containers[id]; ok {
		s.containersLock.Unlock()
		log.Errorf("container %s already exists", id)
		return nil, er.AlreadyExists
	}
	if s.config.ContainerConfigs == nil {
		s.config.ContainerConfigs = make(map[string]*ContainerConfig)
	}
	// The speculative config entry doubles as the create claim: a concurrent
	// same-ID Create passes the containers check above while the first create
	// is still in its slow guest call, and must bail out HERE instead of
	// overwriting the in-flight entry. An overwrite either split the pair of
	// maps (winner's container registered, loser's config persisted) or let
	// the loser's rollback drop the entry entirely — both lost the container
	// from the StoreSandbox snapshot and leaked its guest domain.
	if _, ok := s.config.ContainerConfigs[id]; ok {
		s.containersLock.Unlock()
		log.Errorf("container %s create already in flight", id)
		return nil, er.AlreadyExists
	}
	s.config.ContainerConfigs[id] = &config
	origInfraOnly := s.config.InfraOnly
	if s.config.InfraOnly && !config.IsInfra {
		s.config.InfraOnly = false
	}
	newc := s.config.ContainerConfigs[id]
	s.containersLock.Unlock()

	defer func() {
		if err == nil || s.config == nil {
			return
		}
		s.rollbackFailedContainerConfig(id, newc, origInfraOnly)
	}()

	c, err := newContainerWithContext(ctx, s, newc)
	if err != nil {
		return nil, err
	}
	// restoreState (inside newContainerWithContext) may have replaced the
	// ContainerConfigs[id] entry with the loaded &storage.Config when it hit
	// the legacy per-container-file fallback, so the rollback token captured
	// above no longer matches the live entry.
	// Re-capture the current entry so the deferred rollback's pointer-identity
	// guard (which protects a concurrent same-ID Create's entry from being
	// dropped) still works: without this, a validateMicaContainer failure
	// after a successful restore would leave a phantom config entry that the
	// next StoreSandbox persists and a later restart revives.
	s.containersLock.Lock()
	newc = s.config.ContainerConfigs[id]
	s.containersLock.Unlock()
	if err := c.validateMicaContainer(); err != nil {
		return nil, fmt.Errorf("invalid mica container %s: %w", c.ID(), err)
	}

	defer func() {
		if err == nil {
			return
		}
		log.Errorf("failed to create container %s: %v", id, err)
		// Restore InfraOnly BEFORE cleanupFailedContainerCreate persists the
		// sandbox snapshot: defers are LIFO, so this runs before the earlier
		// defer A. Without this, StoreSandbox inside cleanup would persist
		// InfraOnly=false (set at line 38), and the later defer A restore to
		// memory-only true would leave disk and memory diverged.
		s.containersLock.Lock()
		s.config.InfraOnly = origInfraOnly
		s.containersLock.Unlock()
		// This defer is registered BEFORE c.create: registerClient may have
		// created the guest domain and persisted state already, so a c.create
		// failure (or an addContainer failure) must tear the guest down too,
		// not just roll back the config entry — otherwise the live domain
		// leaks as an orphan nothing tracks.
		if cleanupErr := s.cleanupFailedContainerCreate(ctx, c); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	if err = c.create(ctx); err != nil {
		return nil, err
	}
	if err = s.addContainer(c); err != nil {
		return nil, err
	}

	if err = s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}
	if err = s.persistSandboxState(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// rollbackFailedContainerConfig undoes the speculative ContainerConfigs write
// a failed CreateContainer made. It must NOT run when a concurrent same-ID
// Create won: the winner's container is registered in s.containers and the
// ContainerConfigs entry belongs to it — deleting that entry (the previous
// unconditional behavior) dropped the winner from the next StoreSandbox
// snapshot, so after a shim restart the container was never recovered and its
// guest domain leaked. The entry is only deleted while it is still the one
// this call wrote (pointer identity): a later same-ID Create may have
// overwritten it, in which case that call's own rollback governs.
func (s *Sandbox) rollbackFailedContainerConfig(id string, newc *ContainerConfig, origInfraOnly bool) {
	s.containersLock.Lock()
	defer s.containersLock.Unlock()
	if _, registered := s.containers[id]; registered {
		return
	}
	if s.config.ContainerConfigs[id] == newc {
		delete(s.config.ContainerConfigs, id)
	}
	s.config.InfraOnly = origInfraOnly
}

func (s *Sandbox) cleanupFailedContainerCreate(ctx context.Context, c *Container) error {
	if c == nil {
		return nil
	}

	// Create RPC may have already canceled ctx (timeout); cleanup must still
	// tear down a partially registered domain. Mirror Start rollback.
	ctx = context.WithoutCancel(ctx)

	var errs []error
	if errStop := c.stop(ctx, true); errStop != nil {
		log.Errorf("failed to stop container %s after creation failure: %v", c.id, errStop)
		errs = append(errs, fmt.Errorf("stop container %s after creation failure: %w", c.id, errStop))
	}
	log.Debug("remove stopped container from sandbox")
	// The container may never have been registered (c.create failed before
	// addContainer): a missing map entry is expected there, not an error.
	if errRemove := s.removeContainer(c.id); errRemove != nil && !errors.Is(errRemove, er.ContainerNotFound) {
		log.Errorf("failed to remove container %s after creation failure: %v", c.id, errRemove)
		errs = append(errs, fmt.Errorf("remove container %s after creation failure: %w", c.id, errRemove))
	}
	// Clean up the per-container state file so it doesn't linger on disk
	// after a failed create. registerClient → setContainerState → saveState
	// already wrote it before the failure.
	if errDel := c.DeleteState(ctx); errDel != nil {
		log.Warnf("failed to delete container state for %s after creation failure: %v", c.id, errDel)
	}
	// Drop the ContainerConfigs entry BEFORE rewriting the snapshot: the
	// caller's deferred config cleanup runs after this function (defer LIFO),
	// so without this the snapshot would still reference the failed
	// container and a shim restart would rebuild a phantom container whose
	// state file is gone. Best-effort: the create failure is already
	// reported.
	s.removeContainerConfig(c.id)
	if errPersist := s.StoreSandbox(ctx); errPersist != nil {
		log.Warnf("failed to persist sandbox snapshot after failed create of %s: %v", c.id, errPersist)
	}
	return errors.Join(errs...)
}

func (s *Sandbox) DeleteContainer(ctx context.Context, containerID string) (ContainerTraits, error) {
	log.Debugf("delete container %s from sandbox", containerID)
	c, err := s.containerByID(containerID)
	if err != nil {
		return nil, err
	}
	if err := c.delete(ctx); err != nil {
		return nil, err
	}

	s.removeContainerResources(containerID)
	if err := s.updateResources(ctx); err != nil {
		log.Debugf("ignore updateResources error after delete %s: %v", containerID, err)
	}
	// Pin/persist are post-delete maintenance. The container is already gone
	// from the map and disk; treating pin/persist failure as delete failure
	// makes retries return ContainerNotFound while the API reported error.
	if err := s.checkVCPUsPinning(ctx); err != nil {
		log.Warnf("checkVCPUsPinning after delete %s: %v", containerID, err)
	}
	if err := s.persistSandboxState(ctx); err != nil {
		log.Warnf("persistSandboxState after delete %s: %v", containerID, err)
	}
	return c, nil
}
