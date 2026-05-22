package oci

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	cntr "micrun/internal/domain/container"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"

	ctrAnnotations "github.com/containerd/containerd/pkg/cri/annotations"
	podmanAnnotations "github.com/containers/podman/v4/pkg/annotations"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type annotationContainerType struct {
	annotation    string
	containerType cntr.ContainerType
}

type sandboxBoolAnnotationApplier func(*cntr.SandboxConfig, bool)

// CRI types list reference: kata-containers.
var (
	// CRIContainerTypeKeyList lists all the CRI keys that could define
	// the container type from annotations in the config.json.
	// io.kubernetes.cri.container_type || io.kubernetes.cri-o.container_type
	CRIContainerTypeKeyList = []string{ctrAnnotations.ContainerType, podmanAnnotations.ContainerType}

	// CRISandboxNameKeyList lists all the CRI keys that could define
	// the sandbox ID from annotations in the config.json.
	// "io.kubernetes.cri.sandbox-id" || "io.kubernetes.cri-o.SandboxID"
	CRISandboxNameKeyList = []string{ctrAnnotations.SandboxID, podmanAnnotations.SandboxID}

	// CRIContainerTypeList lists all the maps from CRI ContainerTypes annotations
	// to a virtcontainers ContainerType.
	CRIContainerTypeList = []annotationContainerType{
		{ctrAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{ctrAnnotations.ContainerTypeContainer, cntr.PodContainer},
		{podmanAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{podmanAnnotations.ContainerTypeContainer, cntr.PodContainer},
	}

	sandboxBoolAnnotationAppliers = map[string]sandboxBoolAnnotationApplier{
		ann.RuntimeEnableVCPUsPinning: func(cfg *cntr.SandboxConfig, value bool) {
			cfg.EnableVCPUsPinning = value
		},
		ann.VCPUBinding: func(cfg *cntr.SandboxConfig, value bool) {
			cfg.EnableVCPUsPinning = value
		},
		ann.RuntimeStaticResource: func(cfg *cntr.SandboxConfig, value bool) {
			cfg.StaticResourceMgmt = value
		},
		ann.RuntimeHugePageEnable: func(cfg *cntr.SandboxConfig, value bool) {
			cfg.HugePageSupport = value
		},
	}
)

func GetContainerType(spec *specs.Spec) (cntr.ContainerType, error) {
	if spec == nil {
		return cntr.UnknownContainerType, fmt.Errorf("oci spec is nil")
	}
	for _, key := range CRIContainerTypeKeyList {
		raw, ok := spec.Annotations[key]
		if !ok {
			continue
		}
		containerType := strings.TrimSpace(raw)
		if containerType == "" {
			continue
		}

		for _, t := range CRIContainerTypeList {
			if t.annotation == containerType {
				return t.containerType, nil
			}
		}
		return cntr.UnknownContainerType, fmt.Errorf("unknown container type: %s", containerType)
	}
	return cntr.SingleContainer, nil
}

func GetSandboxID(spec *specs.Spec) (string, error) {
	if spec == nil {
		return "", fmt.Errorf("oci spec is nil")
	}
	for _, key := range CRISandboxNameKeyList {
		raw, ok := spec.Annotations[key]
		if !ok {
			continue
		}
		sandboxID := strings.TrimSpace(raw)
		if sandboxID != "" {
			return sandboxID, nil
		}
	}
	return "", fmt.Errorf("sandbox ID not found in annotations")
}

func GetSandboxConfigPath(annotations map[string]string) string {
	return annotations[ann.SandboxConfigPathKey]
}

// getOSInfo extracts OS name from annotations.
// Returns the OS from annotation, or defaults to defs.DefaultOS if not specified.
func getOSInfo(getAnnotation func(string) (string, bool)) string {
	var osName string
	if getAnnotation != nil {
		if osAnno, ok := getAnnotation(ann.OSAnnotation); ok {
			osName = osAnno
			log.Debugf("found OS annotation: %s", osName)
			return osName
		}
	}
	if osName == "" {
		osName = defs.DefaultOS
		log.Debugf("OS annotation not found, using default OS: %s", osName)
	}
	return osName
}

func applySandboxBoolAnnotation(cfg *cntr.SandboxConfig, key, value string, apply sandboxBoolAnnotationApplier) {
	if cfg == nil {
		return
	}
	trimmed := strings.TrimSpace(value)
	if b, err := strconv.ParseBool(trimmed); err == nil {
		apply(cfg, b)
	} else {
		log.Debugf("invalid bool for %s: %s", key, trimmed)
	}
	cfg.Annotations[key] = trimmed
}

// checkInfra determines whether the container should behave as an infra sandbox.
func checkInfra(ct cntr.ContainerType, ocispec specs.Spec) bool {
	hasMicrunAnn := false
	hasCRIInfraAnnotation := false

	if ocispec.Annotations != nil {
		hasMicrunAnn = annotationMatches(ocispec.Annotations, func(k, v string) bool {
			return strings.HasPrefix(k, ann.MicrunAnnotationPrefix)
		})
		hasCRIInfraAnnotation = annotationMatches(ocispec.Annotations, func(k, v string) bool {
			v = strings.TrimSpace(v)
			return slices.Contains(CRIContainerTypeKeyList, k) &&
				(v == ctrAnnotations.ContainerTypeSandbox || v == podmanAnnotations.ContainerTypeSandbox)
		})
	}

	isRequestedSandbox := ct.IsCriSandbox()
	log.Debugf("isRequestedSandbox?%v, hasCRIInfraAnnotation?%v, hasMicrunAnnotation?%v",
		isRequestedSandbox, hasCRIInfraAnnotation, hasMicrunAnn)

	return isRequestedSandbox || hasCRIInfraAnnotation
}

func annotationMatches(annotations map[string]string, match func(key, value string) bool) bool {
	for key, value := range annotations {
		if match(key, value) {
			return true
		}
	}
	return false
}

func applySandboxAnnotations(ocispec specs.Spec, cfg *cntr.SandboxConfig) {
	if ocispec.Annotations == nil || cfg == nil {
		return
	}
	if cfg.Annotations == nil {
		cfg.Annotations = make(map[string]string)
	}

	for key, value := range ocispec.Annotations {
		trimmed := strings.TrimSpace(value)
		if !strings.HasPrefix(key, ann.MicrunAnnotationPrefix) || trimmed == "" {
			continue
		}
		cfg.Annotations[key] = trimmed
		if apply, ok := sandboxBoolAnnotationAppliers[key]; ok {
			applySandboxBoolAnnotation(cfg, key, trimmed, apply)
		}
	}
}
