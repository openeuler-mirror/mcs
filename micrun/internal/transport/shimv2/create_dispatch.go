package shim

import (
	"context"
	"fmt"

	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	log "micrun/internal/support/logger"
)

// create is the internal implementation for the Create RPC. It handles sandbox and container creation.
func create(ctx context.Context, s *shimService, r ports.TaskCreateRequest) (*shimContainer, error) {
	plan, err := buildCreatePlan(s, r)
	if err != nil {
		return nil, err
	}

	log.Debugf("create: calling setupContainer...")
	if err := plan.setupContainer(ctx, s); err != nil {
		log.Errorf("create: setupContainer failed: %v", err)
		return nil, err
	}

	container, err := plan.newShimContainer(s)
	if err != nil {
		return nil, err
	}

	plan.applySandboxProcessMetadata(s, container)
	return container, nil
}

func (p *createPlan) setupContainer(ctx context.Context, s *shimService) error {
	if p == nil {
		return fmt.Errorf("create plan is required")
	}
	switch p.containerType {
	case cntr.PodSandbox, cntr.SingleContainer:
		return createSandboxContainer(ctx, s, p.containerType, p.request, p.ociSpec, p.runtimeConfig, p.bundlePath, p.rootfsPath, &p.rootfs)
	case cntr.PodContainer:
		return createPodContainer(ctx, s, p.request, p.ociSpec, p.bundlePath, p.rootfsPath, &p.rootfs)
	default:
		return fmt.Errorf("unsupported container type: %v", p.containerType)
	}
}

func (p *createPlan) newShimContainer(s *shimService) (*shimContainer, error) {
	if p == nil {
		return nil, fmt.Errorf("create plan is required")
	}
	return newContainer(s, p.request, p.containerType, p.ociSpec, p.rootfs.Mounted)
}

func (p *createPlan) applySandboxProcessMetadata(s *shimService, container *shimContainer) {
	if p == nil || p.containerType != cntr.PodSandbox || container == nil {
		return
	}
	if sandbox, ok := s.currentSandbox(); ok {
		if pid := sandbox.NetnsHolderPID(); pid > 0 {
			container.pid = uint32(pid)
		}
	}
}
