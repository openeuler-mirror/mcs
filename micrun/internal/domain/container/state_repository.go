package container

import (
	"context"
	"fmt"
	"os"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/timex"
	"micrun/internal/support/validation"
)

type stateRepository struct {
	store     ports.StateStore
	legacy    legacyStateRepository
	now       timex.Clock
	processID func() int
}

func stateRepositoryFromStore(store ports.StateStore) stateRepository {
	return stateRepository{
		store:  store,
		legacy: defaultLegacyStateRepository(),
	}
}

// stateRepositoryWithLegacyRoots creates a stateRepository with custom legacy
// root directories, useful for tests that need to avoid writing to /run/micrun.
func stateRepositoryWithLegacyRoots(store ports.StateStore, sandboxRoot, containerRoot string) stateRepository {
	return stateRepository{
		store:  store,
		legacy: legacyStateRepositoryWithRoots(sandboxRoot, containerRoot),
	}
}

func stateRepositoryFromDependenciesChecked(deps *Dependencies) (stateRepository, error) {
	if deps == nil {
		return stateRepository{}, fmt.Errorf("container: state repository dependencies are required")
	}
	if err := deps.Validate(); err != nil {
		return stateRepository{}, err
	}
	store := deps.StateStoreFactory()
	if validation.IsNil(store) {
		return stateRepository{}, fmt.Errorf("container: dependencies require non-nil StateStore")
	}
	repo := stateRepositoryFromStore(store)
	repo.now = deps.Now
	return repo, nil
}

func (r stateRepository) SaveSandbox(ctx context.Context, sandbox *Sandbox) error {
	if err := r.validateSandboxForSave(sandbox); err != nil {
		return err
	}

	serializable := sandboxStorageFromSandbox(sandbox, timex.Now(r.now).Unix(), r.currentProcessID())
	return saveStateSnapshot(ctx, r.store, runtimeStateNamespaceSandbox, sandboxSnapshotID(sandbox.id), serializable)
}

func (r stateRepository) currentProcessID() int {
	if r.processID != nil {
		return r.processID()
	}
	return os.Getpid()
}

func (r stateRepository) LoadSandbox(ctx context.Context, id string) (*SandboxStorage, error) {
	if id == "" {
		return nil, er.EmptySandboxID
	}

	storage, err := r.loadSandboxRuntimeSnapshot(ctx, sandboxSnapshotID(id))
	if err == nil {
		return storage, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load sandbox state from runtime store: %w", err)
	}

	return r.loadLegacySandboxState(ctx, id)
}

func (r stateRepository) DeleteSandbox(ctx context.Context, id string) error {
	if id == "" {
		return er.EmptySandboxID
	}
	if err := r.deleteSnapshot(ctx, runtimeStateNamespaceSandbox, sandboxSnapshotID(id)); err != nil {
		return err
	}
	return r.legacy.removeSandboxState(id)
}

func (r stateRepository) SaveContainer(ctx context.Context, container *Container) error {
	if err := r.validateContainerForSave(container); err != nil {
		return err
	}

	return saveStateSnapshot(
		ctx,
		r.store,
		runtimeStateNamespaceContainer,
		containerSnapshotID(container.containerPath, container.id),
		containerStorageFromContainer(container),
	)
}

func (r stateRepository) LoadContainer(ctx context.Context, id, containerPath string, extraLegacyPaths ...string) (*ContainerStorage, error) {
	if id == "" {
		return nil, er.EmptyContainerID
	}

	snapshotID := containerSnapshotID(containerPath, id)
	storage, err := r.loadContainerRuntimeSnapshot(ctx, snapshotID)
	if err == nil {
		return storage, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to restore container state from runtime store: %w", err)
	}

	return r.loadLegacyContainerState(ctx, id, snapshotID, r.legacy.containerStatePaths(containerPath, id, extraLegacyPaths))
}

func (r stateRepository) loadContainerRuntimeSnapshot(ctx context.Context, snapshotID string) (*ContainerStorage, error) {
	return loadStateSnapshot[ContainerStorage](ctx, r.store, runtimeStateNamespaceContainer, snapshotID)
}

func (r stateRepository) loadSandboxRuntimeSnapshot(ctx context.Context, snapshotID string) (*SandboxStorage, error) {
	return loadStateSnapshot[SandboxStorage](ctx, r.store, runtimeStateNamespaceSandbox, snapshotID)
}

func (r stateRepository) loadLegacyContainerState(ctx context.Context, id, fallbackSnapshotID string, legacyPaths []string) (*ContainerStorage, error) {
	legacyStorage, _, err := r.legacy.loadContainerState(legacyPaths)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, er.ContainerNotFound
		}
		return nil, err
	}

	r.migrateLegacyContainerState(ctx, id, fallbackSnapshotID, legacyStorage)
	return legacyStorage, nil
}

func (r stateRepository) loadLegacySandboxState(ctx context.Context, id string) (*SandboxStorage, error) {
	legacyStorage, legacyPath, err := r.legacy.loadSandboxState(id)
	if err != nil {
		if os.IsNotExist(err) {
			log.Tracef("not found sandbox state file: %s, sandbox may have been already cleaned up", legacyPath)
			return nil, er.SandboxNotFound
		}
		return nil, fmt.Errorf("failed to load sandbox state from %s: %w", legacyPath, err)
	}

	r.migrateLegacySandboxState(ctx, id, legacyStorage)
	return legacyStorage, nil
}

func (r stateRepository) DeleteContainer(ctx context.Context, id, containerPath string, extraLegacyPaths ...string) error {
	if id == "" {
		return er.EmptyContainerID
	}
	if err := r.deleteSnapshot(ctx, runtimeStateNamespaceContainer, containerSnapshotID(containerPath, id)); err != nil {
		return err
	}

	r.legacy.removeContainerStateFiles(r.legacy.containerStatePaths(containerPath, id, extraLegacyPaths))
	return nil
}

func (r stateRepository) migrateLegacyContainerState(ctx context.Context, id, fallbackSnapshotID string, storage *ContainerStorage) {
	if storage == nil {
		return
	}
	migrateID := containerSnapshotID(storage.ContainerPath, id)
	if migrateID == "" {
		migrateID = fallbackSnapshotID
	}
	migrateLegacySnapshot(ctx, r.store, "container", id, runtimeStateNamespaceContainer, migrateID, *storage)
}

func (r stateRepository) migrateLegacySandboxState(ctx context.Context, id string, storage *SandboxStorage) {
	if storage == nil {
		return
	}
	migrateLegacySnapshot(ctx, r.store, "sandbox", id, runtimeStateNamespaceSandbox, sandboxSnapshotID(id), *storage)
}

func migrateLegacySnapshot[T any](ctx context.Context, store ports.StateStore, kind, id, namespace, snapshotID string, storage T) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		log.Debugf("skipping legacy %s state migration for %s: %v", kind, id, err)
		return
	}
	if migrateErr := saveStateSnapshot(ctx, store, namespace, snapshotID, storage); migrateErr != nil {
		log.Warnf("failed to migrate legacy %s state for %s: %v", kind, id, migrateErr)
	}
}

func (r stateRepository) deleteSnapshot(ctx context.Context, namespace, taskID string) error {
	if validation.IsNil(r.store) {
		return fmt.Errorf("state store is nil")
	}
	namespace, taskID, err := normalizeSnapshotKey(namespace, taskID)
	if err != nil {
		return err
	}
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := r.store.Delete(ctx, namespace, taskID); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
