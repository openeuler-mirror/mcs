package shim

import (
	"fmt"
	"path/filepath"

	oci "micrun/internal/adapters/config/oci"
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	log "micrun/internal/support/logger"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type createPlan struct {
	request       ports.TaskCreateRequest
	rootfs        cntr.RootFs
	ociSpec       *specs.Spec
	bundlePath    string
	rootfsPath    string
	runtimeConfig *oci.RuntimeConfig
	containerType cntr.ContainerType
}

func buildCreatePlan(s *shimService, r ports.TaskCreateRequest) (*createPlan, error) {
	log.Debugf("create: id=%s bundle=%s rootfs_count=%d", r.ID, r.Bundle, len(r.Rootfs))

	plan := newCreatePlan(r)
	if err := plan.loadSpec(); err != nil {
		return nil, err
	}
	if err := plan.resolveContainerType(); err != nil {
		return nil, err
	}
	if err := plan.resolveRuntimeConfig(s); err != nil {
		return nil, err
	}
	return plan, nil
}

func newCreatePlan(r ports.TaskCreateRequest) *createPlan {
	return &createPlan{
		request: r,
		rootfs:  extractRootfs(r),
	}
}

func (p *createPlan) loadSpec() error {
	log.Debugf("create: loading OCI spec...")
	ociSpec, bundlePath, err := loadSpec(p.request.ID, p.request.Bundle)
	if err != nil {
		log.Errorf("create: failed to load spec: %v", err)
		return fmt.Errorf("load OCI spec for %s: %w", p.request.ID, err)
	}
	p.ociSpec = ociSpec
	p.bundlePath = bundlePath
	p.rootfsPath = filepath.Join(bundlePath, "rootfs")
	return nil
}

func (p *createPlan) resolveContainerType() error {
	log.Debugf("create: getting container type...")
	containerType, err := oci.GetContainerType(p.ociSpec)
	if err != nil {
		log.Errorf("create: failed to get container type: %v", err)
		return fmt.Errorf("resolve container type for %s: %w", p.request.ID, err)
	}
	p.containerType = containerType
	return nil
}

func (p *createPlan) resolveRuntimeConfig(s *shimService) error {
	log.Debugf("create: loading runtime config...")
	runtimeConfig, err := loadRuntimeConfig(s, p.request, p.annotations())
	if err != nil {
		log.Errorf("create: failed to load runtime config: %v", err)
		return fmt.Errorf("load runtime config for %s: %w", p.request.ID, err)
	}
	p.runtimeConfig = runtimeConfig
	return nil
}

func (p *createPlan) annotations() map[string]string {
	if p == nil || p.ociSpec == nil {
		return nil
	}
	return p.ociSpec.Annotations
}

func extractRootfs(r ports.TaskCreateRequest) cntr.RootFs {
	rootfs := cntr.RootFs{}
	if len(r.Rootfs) != 1 {
		return rootfs
	}

	mnt := r.Rootfs[0]
	rootfs.Source = mnt.Source
	rootfs.Type = mnt.Type
	rootfs.Options = mnt.Options
	return rootfs
}
