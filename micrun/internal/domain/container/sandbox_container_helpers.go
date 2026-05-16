package container

import (
	"context"
	"errors"
	"fmt"
	"sort"

	er "micrun/internal/support/errors"
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
	c, ok := s.containers[id]
	if !ok || c == nil {
		return nil, er.ContainerNotFound
	}
	return c, nil
}

func (s *Sandbox) containerEntries() ([]sandboxContainerEntry, error) {
	if s == nil {
		return nil, er.SandboxNotFound
	}

	ids := make([]string, 0, len(s.containers))
	for id := range s.containers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	entries := make([]sandboxContainerEntry, 0, len(ids))
	for _, id := range ids {
		c, err := s.containerByID(id)
		if err != nil {
			return nil, fmt.Errorf("sandbox %s container %q: %w", s.id, id, err)
		}
		entries = append(entries, sandboxContainerEntry{id: id, container: c})
	}
	return entries, nil
}

func (s *Sandbox) containerConfigEntries() ([]sandboxContainerConfigEntry, error) {
	if s == nil {
		return nil, er.SandboxNotFound
	}
	if s.config == nil {
		return nil, fmt.Errorf("sandbox config is nil")
	}

	keys := make([]string, 0, len(s.config.ContainerConfigs))
	for key := range s.config.ContainerConfigs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

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

	delete(s.containers, containerID)
	return nil
}

func (s *Sandbox) removeContainerResources(id string) {
	if s == nil || id == "" {
		return
	}
	if s.config != nil {
		delete(s.config.ContainerConfigs, id)
	}
	if s.resManager.ContainerCPUSet != nil {
		delete(s.resManager.ContainerCPUSet, id)
	}
	if s.resManager.ContainerVCPUs != nil {
		delete(s.resManager.ContainerVCPUs, id)
	}
}

func (s *Sandbox) persistSandboxState(ctx context.Context) error {
	return s.StoreSandbox(ctx)
}
