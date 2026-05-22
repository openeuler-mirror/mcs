package oci

import (
	"context"
	"fmt"

	cntr "micrun/internal/domain/container"
	log "micrun/internal/support/logger"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type containerConfigBuilder struct {
	request     ContainerConfigRequest
	baseRootfs  string
	hostProfile HostProfile
	annotations map[string]string
	isInfra     bool
	assets      containerConfigAssets
	policy      cntr.ResourcePolicy
}

type containerConfigAssets struct {
	osName       string
	pedestal     cntr.PedestalType
	pedestalConf string
	firmwarePath string
}

func newContainerConfigBuilder(request ContainerConfigRequest) (*containerConfigBuilder, error) {
	if request.RuntimeConfig == nil {
		return nil, fmt.Errorf("runtime config is required")
	}
	builder := &containerConfigBuilder{
		request:     request,
		baseRootfs:  bundleRootfs(request.Bundle),
		hostProfile: request.RuntimeConfig.host(),
		annotations: request.Spec.Annotations,
		isInfra:     checkInfra(request.ContainerType, request.Spec),
		policy:      cntr.ResourcePolicyOrDefault(request.ResourcePolicy),
	}
	return builder, nil
}

func (b *containerConfigBuilder) build(ctx context.Context) (*cntr.ContainerConfig, error) {
	if err := b.resolvePedestalConfig(); err != nil {
		return nil, err
	}
	if err := b.resolveContainerImage(); err != nil {
		return nil, err
	}
	if err := b.prepareContainerCache(); err != nil {
		return nil, err
	}

	config := b.newContainerConfig()
	if err := b.applyResources(ctx, config); err != nil {
		return nil, err
	}
	b.logConfigSummary(config)
	return config, nil
}

func (b *containerConfigBuilder) resolvePedestalConfig() error {
	pedtype, pedconf, err := extPedConfig(b.annotation, b.baseRootfs, b.hostProfile)
	if err != nil {
		return err
	}
	b.assets.pedestal = cntr.PedestalTypeFromInt(int(pedtype))
	b.assets.pedestalConf = pedconf
	b.assets.osName = getOSInfo(b.annotation)
	return nil
}

func (b *containerConfigBuilder) resolveContainerImage() error {
	elfPath, err := resolveContainerFirmware(
		b.baseRootfs,
		b.isInfra,
		b.annotations,
		b.request.RuntimeConfig.DefaultFirmwarePath,
		b.request.FallbackFirmwarePath,
	)
	if err != nil {
		return err
	}
	if !b.isInfra {
		if err := verifyContainerFirmwareHash(elfPath, b.annotations); err != nil {
			return err
		}
	}
	b.assets.firmwarePath = elfPath
	return nil
}

func (b *containerConfigBuilder) prepareContainerCache() error {
	pedconf, firmwarePath, err := prepCache(
		b.request.ID,
		b.assets.pedestalConf,
		b.assets.firmwarePath,
		b.hostProfile,
		b.request.RuntimeConfig.StateDir,
	)
	if err != nil {
		return err
	}
	b.assets.pedestalConf = pedconf
	b.assets.firmwarePath = firmwarePath
	return nil
}

func (b *containerConfigBuilder) newContainerConfig() *cntr.ContainerConfig {
	return &cntr.ContainerConfig{
		ID:               b.request.ID,
		ImageAbsPath:     b.assets.firmwarePath,
		PedestalType:     b.assets.pedestal,
		PedestalConf:     b.assets.pedestalConf,
		CacheRoot:        ContainerCacheRoot(b.request.RuntimeConfig.StateDir),
		OS:               b.assets.osName,
		PCPUNum:          1,
		Resources:        &specs.LinuxResources{},
		IsInfra:          b.isInfra,
		Annotations:      cloneAnnotations(b.annotations),
		ExclusiveDom0CPU: b.request.RuntimeConfig.ExclusiveDom0CPU,
	}
}

func (b *containerConfigBuilder) applyResources(ctx context.Context, config *cntr.ContainerConfig) error {
	if err := config.ParseOCIResourcesWithPolicy(ctx, &b.request.Spec, b.policy); err != nil {
		return err
	}

	applyMemoryReservationFromAnnotation(config, b.annotations)
	if err := applyContainerRuntimeDefaults(config, b.annotations, b.request.RuntimeConfig); err != nil {
		return err
	}
	if err := cntr.ValidateResourceLimitsWithPolicy(ctx, config, b.policy); err != nil {
		log.Warnf("resource validation warning: %v", err)
	}
	return nil
}

func (b *containerConfigBuilder) logConfigSummary(config *cntr.ContainerConfig) {
	log.Debugf("container OS: %s", config.OS)
	log.Debugf("container resource limits - CPU: %s, Memory: %s",
		formatCPULimit(config), formatMemoryLimit(config))
}

func (b *containerConfigBuilder) annotation(key string) (string, bool) {
	return getAnnotation(key, b.annotations)
}

func cloneAnnotations(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
