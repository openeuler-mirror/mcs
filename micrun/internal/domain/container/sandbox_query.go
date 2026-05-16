package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
)

func (s *Sandbox) GetAllContainers() []ContainerTraits {
	if s == nil {
		return nil
	}
	list := make([]ContainerTraits, 0, len(s.containers))
	for _, c := range s.containers {
		if c == nil {
			continue
		}
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID() < list[j].ID()
	})
	return list
}

func (s *Sandbox) SandboxID() string {
	if s == nil {
		return ""
	}
	return s.id
}

func (s *Sandbox) Annotation(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("annotation key is empty")
	}
	if s == nil || s.config == nil {
		return "", fmt.Errorf("sandbox config is nil")
	}
	s.annotLock.RLock()
	defer s.annotLock.RUnlock()
	value, found := s.config.Annotations[key]
	if !found {
		return "", fmt.Errorf("annotation not found: %s", key)
	}
	return value, nil
}

func (s *Sandbox) GetAnnotations() map[string]string {
	if s == nil || s.config == nil || len(s.config.Annotations) == 0 {
		return nil
	}
	s.annotLock.RLock()
	defer s.annotLock.RUnlock()

	annotations := make(map[string]string, len(s.config.Annotations))
	for key, value := range s.config.Annotations {
		annotations[key] = value
	}
	return annotations
}

func (s *Sandbox) Monitor() {
}

func (s *Sandbox) GetNetNamespace() string {
	if s == nil || s.network == nil {
		return ""
	}
	return s.network.NetID()
}

func (s *Sandbox) GuestCtl() ports.GuestControl {
	if s == nil {
		return nil
	}
	return s.guestControl
}

func (s *Sandbox) NetnsHolderPID() int {
	if s == nil {
		return 0
	}
	if cfg := s.config; cfg != nil {
		return cfg.NetworkConfig.HolderPid
	}
	return 0
}

func (s *Sandbox) GetState() StateString {
	if s == nil {
		return StateDown
	}
	return s.state.State
}

func (s *Sandbox) StatusContainer(ctx context.Context, containerID string) (ContainerStatus, error) {
	cs := ContainerStatus{}
	if err := s.requireQuerySandbox(); err != nil {
		return cs, err
	}
	var err error
	ctx, err = activeContainerContext(ctx)
	if err != nil {
		return cs, err
	}
	if err := requireContainerQueryID(containerID); err != nil {
		return cs, er.EmptyContainerID
	}

	c, err := s.queryContainer(containerID)
	if err != nil {
		log.Debugf("container %s not found in sandbox %s", containerID, s.id)
		return cs, err
	}
	if _, err := c.ensureClientPresenceWithContext(ctx); err != nil {
		return cs, err
	}

	state, err := c.checkStateWithContext(ctx)
	if err != nil {
		return cs, err
	}
	if state == StateDown {
		return cs, er.ContainerNotFound
	}
	if c.config == nil {
		return cs, fmt.Errorf("container config is nil")
	}

	rootfs := c.config.Rootfs.Source
	if c.config.Rootfs.Mounted {
		rootfs = c.config.Rootfs.Target
	}

	cs.Spec = nil
	cs.State = c.state
	cs.ID = c.id
	cs.Rootfs = rootfs
	cs.Pid = c.GetPid()
	cs.Annotations = c.config.Annotations
	return cs, nil
}

func (s *Sandbox) StatsContainer(ctx context.Context, containerID string) (ContainerStats, error) {
	if err := s.requireQuerySandbox(); err != nil {
		return ContainerStats{}, err
	}
	var err error
	ctx, err = activeContainerContext(ctx)
	if err != nil {
		return ContainerStats{}, err
	}
	if err := requireContainerQueryID(containerID); err != nil {
		return ContainerStats{}, err
	}
	c, err := s.queryContainer(containerID)
	if err != nil {
		return ContainerStats{}, err
	}

	stats, err := c.stats(ctx)
	if err != nil {
		log.Errorf("failed to get stats for container %s: %v", containerID, err)
		return ContainerStats{}, err
	}
	return *stats, nil
}

func (s *Sandbox) IOStream(ctx context.Context, containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	if err := s.requireQuerySandbox(); err != nil {
		return nil, nil, nil, err
	}
	var err error
	ctx, err = activeContainerContext(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := requireContainerQueryID(containerID); err != nil {
		return nil, nil, nil, err
	}
	if s.state.State != StateRunning {
		return nil, nil, nil, er.SandboxDown
	}

	c, err := s.queryContainer(containerID)
	if err != nil {
		return nil, nil, nil, err
	}

	return c.ioStream(ctx, taskID)
}

func (s *Sandbox) WaitContainerExit(ctx context.Context, containerID string) (int32, error) {
	if err := s.requireQuerySandbox(); err != nil {
		return okCode, err
	}
	var err error
	ctx, err = activeContainerContext(ctx)
	if err != nil {
		return okCode, err
	}
	if err := requireContainerQueryID(containerID); err != nil {
		return okCode, err
	}
	c, err := s.queryContainer(containerID)
	if err != nil {
		return okCode, err
	}

	state, err := c.checkStateWithContext(ctx)
	if err != nil {
		return okCode, err
	}
	if state == StateDown {
		return okCode, er.ContainerDown
	}

	if state == StateStopped {
		return okCode, nil
	}

	log.Infof("wait for container=%s exiting", containerID)

	notifier := c.exitNotifierForState(state)

	select {
	case <-ctx.Done():
		return okCode, ctx.Err()
	case <-notifier:
		return okCode, nil
	}
}

func (s *Sandbox) WinResize(ctx context.Context, containerID string, height, width uint32) error {
	if err := s.requireQuerySandbox(); err != nil {
		return err
	}
	var err error
	ctx, err = activeContainerContext(ctx)
	if err != nil {
		return err
	}
	if err := requireContainerQueryID(containerID); err != nil {
		return err
	}
	if s.state.State != StateRunning {
		return er.SandboxDown
	}

	c, err := s.queryContainer(containerID)
	if err != nil {
		return err
	}

	return c.winresize(ctx, height, width)
}

func (s *Sandbox) OpenTTYs(ctx context.Context, containerID string) (stdin, stdout *os.File, err error) {
	if err := s.requireQuerySandbox(); err != nil {
		return nil, nil, err
	}
	ctx, err = activeContainerContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := requireContainerQueryID(containerID); err != nil {
		return nil, nil, err
	}
	if s.state.State != StateRunning {
		return nil, nil, er.SandboxDown
	}

	c, err := s.queryContainer(containerID)
	if err != nil {
		return nil, nil, err
	}

	return c.OpenTTYs(ctx)
}

func (s *Sandbox) requireQuerySandbox() error {
	if s == nil {
		return er.SandboxNotFound
	}
	return nil
}

func (s *Sandbox) queryContainer(containerID string) (*Container, error) {
	c, ok := s.containers[containerID]
	if !ok || c == nil {
		return nil, er.ContainerNotFound
	}
	return c, nil
}

func requireContainerQueryID(containerID string) error {
	if containerID == "" {
		log.Debugf("container query: empty id")
		return er.EmptyContainerID
	}
	return nil
}

func (s *Sandbox) SetSandboxState(state StateString) error {
	if s == nil {
		return er.SandboxNotFound
	}
	if !state.valid() {
		return er.InvalidState
	}
	s.state.State = state
	log.Debugf("SetSandboxState: sandbox %s state set to %s", s.id, state)
	return nil
}

func (s *Sandbox) setSandboxState(state StateString) error {
	if s == nil {
		return er.SandboxNotFound
	}
	if !state.valid() {
		return er.InvalidState
	}
	s.state.State = state
	return nil
}

func (s *Sandbox) notOperational() bool {
	if s == nil {
		return true
	}
	return s.state.State != StateReady && s.state.State != StateRunning
}

func (s *Sandbox) activeContainer(ctx context.Context, containerID string) (bool, error) {
	if s == nil {
		return false, nil
	}
	ctx = queryContext(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c, ok := s.containers[containerID]
	if !ok {
		return true, nil
	}
	state, err := c.checkStateWithContext(ctx)
	if err != nil {
		return false, err
	}
	if state == StateStopped || state == StateDown {
		log.Debugf("skipped inactive container %s (state=%s)", c.ID(), state)
		return false, nil
	}
	return true, nil
}
