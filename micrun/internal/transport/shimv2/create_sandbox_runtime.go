package shim

import (
	"context"
	"fmt"

	oci "micrun/internal/adapters/config/oci"
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func createSandboxContainer(ctx context.Context, s *shimService, containerType cntr.ContainerType,
	r ports.TaskCreateRequest, ociSpec *specs.Spec, runtimeConfig *oci.RuntimeConfig,
	bundlePath, rootfsPath string, rootfs *cntr.RootFs) (err error) {
	if err := reconcileExistingSandbox(ctx, s); err != nil {
		return err
	}

	s.config = runtimeConfig
	log.Debugf("createSandboxContainer: containerType=%v bundlePath=%s rootfsPath=%s", containerType, bundlePath, rootfsPath)

	if containerType != cntr.PodSandbox {
		logSingleContainerRootfsSource(r)
	}

	return withMountedRootfs(rootfsPath, r.Rootfs, rootfs, func() error {
		logMountedSingleContainerRootfs(containerType, rootfsPath)

		sandbox, createErr := createSandbox(
			ctx,
			ociSpec,
			runtimeConfig,
			*rootfs,
			containerType,
			r.ID,
			bundlePath,
			s.runtimeDeps.guestControl,
			s.runtimeDeps.hypervisor,
			s.runtimeDeps.containerDeps,
			&s.runtimeDeps.resourcePolicy,
		)
		if createErr != nil {
			return createErr
		}

		s.setSandboxTraits(sandbox)
		if err := persistCreatedSandbox(ctx, sandbox); err != nil {
			s.clearSandbox()
			if cleanupErr := sandbox.Delete(ctx); cleanupErr != nil {
				log.Warnf("failed to cleanup sandbox %s after state persistence failure: %v", sandbox.SandboxID(), cleanupErr)
			}
			return err
		}
		return nil
	})
}

// createSandbox initializes and creates a new sandbox instance.
func createSandbox(ctx context.Context, ocispec *specs.Spec,
	runtimeConfig *oci.RuntimeConfig, rootfs cntr.RootFs,
	containerType cntr.ContainerType, containerID, bundle string, guestControl ports.GuestControl,
	hypervisorControl ports.HypervisorControl, deps *cntr.Dependencies, resourcePolicy *cntr.ResourcePolicy) (_ *cntr.Sandbox, err error) {

	log.Debugf("createSandbox: containerId=%s bundle=%s rootfs.Mounted=%v", containerID, bundle, rootfs.Mounted)

	sandboxConfig, err := oci.SandboxConfig(ctx, ocispec, *runtimeConfig, bundle, containerID, containerType, resourcePolicy)
	if err != nil {
		log.Errorf("createSandbox: failed to get sandbox config: %v", err)
		return nil, err
	}

	sandboxConfig.GuestControl = guestControl
	sandboxConfig.HypervisorControl = hypervisorControl
	sandboxConfig.Dependencies = deps

	if err := injectUnmountedRootfs(containerID, rootfs, &sandboxConfig); err != nil {
		return nil, err
	}

	if err := setupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			if cleanupErr := cleanupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); cleanupErr != nil {
				log.Debugf("failed to cleanup network namespace for sandbox %s: %v", sandboxConfig.ID, cleanupErr)
			}
		}
	}()

	propagateNetworkNamespaceAnnotation(ocispec, &sandboxConfig)

	sandbox, err := cntr.CreateSandbox(ctx, &sandboxConfig)
	if err != nil {
		return nil, err
	}

	log.Debugf("Sandbox <%s> created.", sandbox.SandboxID())
	containers := sandbox.GetAllContainers()
	if len(containers) != 1 {
		return nil, fmt.Errorf("container list from sandbox is wrong, expecting only one container, got %d", len(containers))
	}
	return sandbox, nil
}

const nerdctlNetworkNamespaceAnnotation = "nerdctl/network-namespace"

func propagateNetworkNamespaceAnnotation(ociSpec *specs.Spec, sandboxConfig *cntr.SandboxConfig) {
	if ociSpec == nil || sandboxConfig == nil {
		return
	}
	if ociSpec.Annotations == nil {
		ociSpec.Annotations = make(map[string]string)
	}
	if sandboxConfig.Annotations == nil {
		sandboxConfig.Annotations = make(map[string]string)
	}

	networkID := sandboxConfig.NetworkConfig.NetworkID
	ociSpec.Annotations[nerdctlNetworkNamespaceAnnotation] = networkID
	sandboxConfig.Annotations[nerdctlNetworkNamespaceAnnotation] = networkID
}

func injectUnmountedRootfs(containerID string, rootfs cntr.RootFs, sandboxConfig *cntr.SandboxConfig) error {
	if sandboxConfig == nil {
		return fmt.Errorf("sandbox config is required")
	}
	if rootfs.Mounted || len(sandboxConfig.ContainerConfigs) != 1 {
		return nil
	}

	containerConfig := sandboxConfig.ContainerConfigs[containerID]
	if containerConfig == nil {
		return fmt.Errorf("container config %s not found for rootfs injection", containerID)
	}
	if rootfs.Source != "" {
		realPath, err := fs.ResolvePath(rootfs.Source)
		if err != nil {
			return err
		}
		rootfs.Source = realPath
	}
	containerConfig.Rootfs = rootfs
	return nil
}

func logMountedSingleContainerRootfs(containerType cntr.ContainerType, rootfsPath string) {
	if containerType == cntr.PodSandbox {
		return
	}
	log.Debug("rootfs mounted for single container, showing rootfs contents:")
	if err := fs.TravelDir(rootfsPath); err != nil {
		log.Debugf("failed to traverse rootfs directory: %v", err)
	}
}

func logSingleContainerRootfsSource(r ports.TaskCreateRequest) {
	if len(r.Rootfs) == 0 {
		log.Debug("single container create request has no rootfs source to inspect")
		return
	}

	source := r.Rootfs[0].GetSource()
	if source == "" {
		log.Debug("single container create request has empty rootfs source")
		return
	}

	log.Debug("rootfs mounted for single container, showing rootfs contents:")
	if err := fs.TravelDir(source); err != nil {
		log.Debugf("failed to traverse rootfs directory: %v", err)
	}
}
