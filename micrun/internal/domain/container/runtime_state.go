package container

import (
	defs "micrun/internal/support/definitions"
	"micrun/internal/support/statekey"
	"strings"
)

const (
	runtimeStateNamespaceSandbox   = "runtime/sandbox"
	runtimeStateNamespaceContainer = "runtime/container"
)

const legacySandboxStateRootDir = defs.SandboxDataDir
const legacyContainerStateRootDir = defs.MicrunStateDir

func sandboxSnapshotID(id string) string {
	return id
}

func containerSnapshotID(containerPath, containerID string) string {
	clean := normalizedContainerPath(containerPath)
	if clean == "" {
		return containerID
	}
	return clean
}

func normalizedContainerPath(containerPath string) string {
	if strings.TrimSpace(containerPath) == "" {
		return ""
	}
	normalized, err := statekey.Normalize(containerPath)
	if err != nil {
		return ""
	}
	return normalized
}
