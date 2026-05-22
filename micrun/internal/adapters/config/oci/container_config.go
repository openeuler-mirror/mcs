package oci

import (
	"context"
	"strconv"
	"strings"

	cntr "micrun/internal/domain/container"
	ann "micrun/internal/support/annotations"
	log "micrun/internal/support/logger"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func getAnnotation(key string, annotations map[string]string) (string, bool) {
	if annotations == nil {
		return "", false
	}
	if raw, ok := annotations[key]; ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			return trimmed, true
		}
	}
	return "", false
}

func resolveContainerFirmware(baseRootfs string, isInfra bool, annotations map[string]string, defaultFirmwarePath, fallbackFirmwarePath string) (string, error) {
	if isInfra {
		return "", nil
	}
	annotationFirmware, _ := getAnnotation(ann.FirmwarePathAnno, annotations)
	resolvedFallback := fallbackFirmwarePath
	if resolvedFallback == "" {
		resolvedFallback = defaultFirmwarePath
	}
	return resolveFirmwarePath(baseRootfs, annotationFirmware, resolvedFallback)
}

func applyMemoryReservationFromAnnotation(config *cntr.ContainerConfig, annotations map[string]string) {
	v, ok := getAnnotation(ann.ContainerMinMemMB, annotations)
	if !ok || v == "" {
		return
	}
	mb, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		log.Debugf("invalid %s: %s", ann.ContainerMinMemMB, v)
		return
	}
	config.SetMemoryReservationMB(uint32(mb))
}

// ContainerConfigRequest is the explicit input contract for building a container config.
type ContainerConfigRequest struct {
	ID                   string
	Bundle               string
	Spec                 specs.Spec
	ContainerType        cntr.ContainerType
	FallbackFirmwarePath string
	RuntimeConfig        *RuntimeConfig
	ResourcePolicy       *cntr.ResourcePolicy
}

func BuildContainerConfig(ctx context.Context, request ContainerConfigRequest) (*cntr.ContainerConfig, error) {
	builder, err := newContainerConfigBuilder(request)
	if err != nil {
		return nil, err
	}
	return builder.build(ctx)
}

func ParseContainerCfg(ctx context.Context, id, bundle string, ocispec specs.Spec, ct cntr.ContainerType, fallbackFirmwarePath string, runtimeConfig *RuntimeConfig, resourcePolicy *cntr.ResourcePolicy) (*cntr.ContainerConfig, error) {
	return BuildContainerConfig(ctx, ContainerConfigRequest{
		ID:                   id,
		Bundle:               bundle,
		Spec:                 ocispec,
		ContainerType:        ct,
		FallbackFirmwarePath: fallbackFirmwarePath,
		RuntimeConfig:        runtimeConfig,
		ResourcePolicy:       resourcePolicy,
	})
}
