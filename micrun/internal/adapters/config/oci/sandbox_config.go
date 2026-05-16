package oci

import (
	"context"
	"fmt"

	"micrun/internal/adapters/hypervisor/pedestal"
	cntr "micrun/internal/domain/container"
	ann "micrun/internal/support/annotations"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func SandboxConfig(ctx context.Context, ocispec *specs.Spec, rc RuntimeConfig, bundle, sbContainerID string, containerType cntr.ContainerType, resourcePolicy *cntr.ResourcePolicy) (cntr.SandboxConfig, error) {
	if ocispec == nil {
		return cntr.SandboxConfig{}, fmt.Errorf("oci spec is required")
	}

	containerConfig, err := BuildContainerConfig(ctx, ContainerConfigRequest{
		ID:                   sbContainerID,
		Bundle:               bundle,
		Spec:                 *ocispec,
		ContainerType:        containerType,
		FallbackFirmwarePath: rc.DefaultFirmwarePath,
		RuntimeConfig:        &rc,
		ResourcePolicy:       resourcePolicy,
	})
	if err != nil {
		return cntr.SandboxConfig{}, err
	}

	networkConfig := cntr.NetworkConfig{}
	staticResMngt := rc.StaticResourceManagement
	hostProfile := rc.host()
	hugePage := hostProfile.hugePageEnabled(staticResMngt)

	if hostProfile.Type == pedestal.Baremetal {
		staticResMngt = true
	}

	sandboxConfig := cntr.SandboxConfig{
		ID:       sbContainerID,
		Hostname: ocispec.Hostname,
		PedConfig: cntr.PedestalConfig{
			PedType:     cntr.PedestalTypeFromInt(int(hostProfile.Type)),
			PedConfig:   containerConfig.PedestalConf,
			MiniVCPUNum: rc.MiniVCPUNum,
		},
		ContainerConfigs: map[string]*cntr.ContainerConfig{
			sbContainerID: containerConfig,
		},
		NetworkConfig: networkConfig,
		Annotations: map[string]string{
			ann.BundlePathKey: bundle,
		},
		StaticResourceMgmt: staticResMngt,
		HugePageSupport:    hugePage,
		EnableVCPUsPinning: false,
		SharedCPUPool:      rc.SharedCPUPool,
		InfraOnly:          containerConfig.IsInfra,
	}

	applySandboxAnnotations(*ocispec, &sandboxConfig)
	if sandboxConfig.Annotations == nil {
		sandboxConfig.Annotations = make(map[string]string)
	}
	if containerConfig.ImageAbsPath != "" {
		sandboxConfig.Annotations[ann.FirmwarePathAnno] = containerConfig.ImageAbsPath
	}
	return sandboxConfig, nil
}
