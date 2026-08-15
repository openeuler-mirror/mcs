package container

import (
	"context"
	"errors"
	"fmt"
	"sort"

	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
)

type sandboxContainerEntry struct {
	id        string
	container *Container
}

type sandboxContainerConfigEntry struct {
	key    string
	config *ContainerConfig
}

func (s *Sandbox) containerByID(id string) (*Container, error) {
	if s == nil {
		return nil, er.SandboxNotFound
	}
	if id == "" {
		return nil, er.EmptyContainerID
	}
	s.containersLock.RLock()
	defer s.containersLock.RUnlock()
	c, ok := s.containers[id]
	if !ok || c == nil {
		return nil, er.ContainerNotFound
	}
	return c, nil
}

// containerCount returns the number of containers under containersLock.
func (s *Sandbox) containerCount() int {
	if s == nil {
		return 0
	}
	s.containersLock.RLock()
	defer s.containersLock.RUnlock()
	return len(s.containers)
}

func (s *Sandbox) containerEntries() ([]sandboxContainerEntry, error) {
	if s == nil {
		return nil, er.SandboxNotFound
	}

	s.containersLock.RLock()
	defer s.containersLock.RUnlock()
	entries := make([]sandboxContainerEntry, 0, len(s.containers))
	for id, c := range s.containers {
		if c == nil {
			return nil, fmt.Errorf("sandbox %s container %q: %w", s.id, id, er.ContainerNotFound)
		}
		entries = append(entries, sandboxContainerEntry{id: id, container: c})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].id < entries[j].id
	})
	return entries, nil
}

func (s *Sandbox) containerConfigEntries() ([]sandboxContainerConfigEntry, error) {
	if s == nil {
		return nil, er.SandboxNotFound
	}
	if s.config == nil {
		return nil, fmt.Errorf("sandbox config is nil")
	}

	s.containersLock.RLock()
	defer s.containersLock.RUnlock()
	keys := make([]string, 0, len(s.config.ContainerConfigs))
	for key := range s.config.ContainerConfigs {
		keys = append(keys, key)
	}
	seenIDs := make(map[string]string, len(keys))
	entries := make([]sandboxContainerConfigEntry, 0, len(keys))
	for _, key := range keys {
		cfg := s.config.ContainerConfigs[key]
		if cfg == nil {
			return nil, fmt.Errorf("sandbox %s container config %q is nil", s.id, key)
		}
		if cfg.ID == "" {
			return nil, fmt.Errorf("sandbox %s container config %q has empty ID: %w", s.id, key, er.EmptyContainerID)
		}
		if previousKey, ok := seenIDs[cfg.ID]; ok {
			return nil, fmt.Errorf("sandbox %s duplicate container config id %q in %q and %q: %w", s.id, cfg.ID, previousKey, key, er.DuplicatedKey)
		}
		seenIDs[cfg.ID] = key
		entries = append(entries, sandboxContainerConfigEntry{key: key, config: cfg})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].config.ID < entries[j].config.ID
	})
	return entries, nil
}

func (s *Sandbox) addContainer(c *Container) error {
	if s == nil {
		return er.SandboxNotFound
	}
	if c == nil {
		return er.ContainerNotFound
	}
	if c.id == "" {
		return er.EmptyContainerID
	}
	s.containersLock.Lock()
	defer s.containersLock.Unlock()
	if s.containers == nil {
		s.containers = make(map[string]*Container)
	}
	if _, ok := s.containers[c.id]; ok {
		return er.DuplicatedKey
	}
	s.containers[c.id] = c
	return nil
}

func (s *Sandbox) removeContainer(containerID string) error {
	if _, err := s.containerByID(containerID); err != nil {
		if !errors.Is(err, er.ContainerNotFound) {
			return err
		}
		return fmt.Errorf("container %q not found in sandbox %q: %w", containerID, s.id, er.ContainerNotFound)
	}

	s.containersLock.Lock()
	defer s.containersLock.Unlock()
	delete(s.containers, containerID)
	return nil
}

// removeContainerConfig removes only the ContainerConfigs entry under
// containersLock. It is a subset of removeContainerResources used by
// cleanupAfterDelete to ensure the persisted sandbox snapshot does not
// reference a container whose state is being deleted.
func (s *Sandbox) removeContainerConfig(containerID string) {
	s.containersLock.Lock()
	if s.config != nil {
		delete(s.config.ContainerConfigs, containerID)
	}
	s.containersLock.Unlock()
}

// restoreContainerConfig re-adds a ContainerConfigs entry that was removed by
// removeContainerConfig, used when rolling back a failed StoreSandbox during
// delete so the containers map and ContainerConfigs stay in sync. An existing
// entry is left untouched: it belongs to a newer same-ID Create that slipped
// in after removeContainer, and overwriting it would pair the new container
// with the deleted one's stale config.
func (s *Sandbox) restoreContainerConfig(containerID string, cfg *ContainerConfig) {
	if cfg == nil {
		return
	}
	s.containersLock.Lock()
	if s.config != nil {
		if s.config.ContainerConfigs == nil {
			s.config.ContainerConfigs = make(map[string]*ContainerConfig)
		}
		if _, ok := s.config.ContainerConfigs[containerID]; !ok {
			s.config.ContainerConfigs[containerID] = cfg
		}
	}
	s.containersLock.Unlock()
}

func (s *Sandbox) removeContainerResources(id string) {
	if s == nil || id == "" {
		return
	}
	// ContainerConfigs is protected by containersLock; resManager maps by resMu.
	// Acquire in a fixed order (containersLock then resMu) to avoid deadlock.
	s.containersLock.Lock()
	if s.config != nil {
		delete(s.config.ContainerConfigs, id)
	}
	s.containersLock.Unlock()

	lockutil.WithLock(&s.resMu, func() {
		if s.resManager.ContainerCPUSet != nil {
			delete(s.resManager.ContainerCPUSet, id)
		}
		if s.resManager.ContainerVCPUs != nil {
			delete(s.resManager.ContainerVCPUs, id)
		}
	})
}

func (s *Sandbox) persistSandboxState(ctx context.Context) error {
	return s.StoreSandbox(ctx)
}
