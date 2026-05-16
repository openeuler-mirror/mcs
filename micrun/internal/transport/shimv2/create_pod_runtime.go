package shim

import (
	"context"
	"fmt"
	"strings"

	oci "micrun/internal/adapters/config/oci"
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	ann "micrun/internal/support/annotations"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func createPodContainer(ctx context.Context, s *shimService, r ports.TaskCreateRequest,
	ociSpec *specs.Spec, bundlePath, rootfsPath string,
	rootfs *cntr.RootFs) (err error) {
	sandbox, ok := s.currentSandbox()
	if !ok {
		return fmt.Errorf("cannot start pod container %s: sandbox is not created", r.ID)
	}

	mergeSandboxMicrunAnnotations(ociSpec, sandbox)
	return withMountedRootfs(rootfsPath, r.Rootfs, rootfs, func() error {
		log.Debug("rootfs mounted for pod container, showing rootfs contents: ")
		return createPodContainerInSandbox(ctx, sandbox, *ociSpec, *rootfs, r.ID, bundlePath, s.config, &s.runtimeDeps.resourcePolicy)
	})
}

type sandboxAnnotationProvider interface {
	GetAnnotations() map[string]string
}

func mergeSandboxMicrunAnnotations(ociSpec *specs.Spec, sandbox cntr.SandboxTraits) {
	if ociSpec == nil || validation.IsNil(sandbox) {
		return
	}
	provider, ok := sandbox.(sandboxAnnotationProvider)
	if !ok {
		return
	}

	sandboxAnnotations := provider.GetAnnotations()
	if len(sandboxAnnotations) == 0 {
		return
	}
	if ociSpec.Annotations == nil {
		ociSpec.Annotations = make(map[string]string)
	}

	for key, value := range sandboxAnnotations {
		trimmed := strings.TrimSpace(value)
		if !strings.HasPrefix(key, ann.MicrunAnnotationPrefix) || trimmed == "" {
			continue
		}
		if existing := strings.TrimSpace(ociSpec.Annotations[key]); existing != "" {
			continue
		}
		ociSpec.Annotations[key] = trimmed
	}
}

// createPodContainerInSandbox creates a container within an existing sandbox.
func createPodContainerInSandbox(ctx context.Context, sandbox cntr.SandboxTraits,
	ocispec specs.Spec, rootfs cntr.RootFs,
	containerID, bundlePath string, runtimeConfig *oci.RuntimeConfig, resourcePolicy *cntr.ResourcePolicy) error {

	defaultFirmware := defaultFirmwareFromSandbox(sandbox)
	containerConfig, err := oci.BuildContainerConfig(ctx, oci.ContainerConfigRequest{
		ID:                   containerID,
		Bundle:               bundlePath,
		Spec:                 ocispec,
		ContainerType:        cntr.PodContainer,
		FallbackFirmwarePath: defaultFirmware,
		RuntimeConfig:        runtimeConfig,
		ResourcePolicy:       resourcePolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to create container config: %w", err)
	}

	containerConfig.Rootfs = rootfs

	if err := validateFirmwareForContainer(containerConfig); err != nil {
		return fmt.Errorf("firmware validation failed for container %s: %w", containerID, err)
	}

	if _, err := sandbox.CreateContainer(ctx, *containerConfig); err != nil {
		return fmt.Errorf("failed to create container in sandbox: %w", err)
	}

	return nil
}

func defaultFirmwareFromSandbox(sandbox cntr.SandboxTraits) string {
	if validation.IsNil(sandbox) {
		return ""
	}

	firmware, err := sandbox.Annotation(ann.FirmwarePathAnno)
	if err != nil {
		return ""
	}
	return firmware
}
