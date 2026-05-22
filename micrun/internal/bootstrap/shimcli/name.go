package shimcli

import (
	"path/filepath"
	"strings"
)

const FallbackBinaryName = "micrun"

func BinaryName(shimName string, argv0 string) string {
	if name := ShimBinaryName(shimName); name != "" {
		return name
	}
	if argv0 != "" {
		return filepath.Base(argv0)
	}
	return FallbackBinaryName
}

func ShimBinaryName(shimName string) string {
	parts := strings.Split(shimName, ".")
	if len(parts) < 2 {
		return ""
	}
	runtimeName := parts[len(parts)-2]
	runtimeVersion := parts[len(parts)-1]
	if runtimeName == "" || runtimeVersion == "" {
		return ""
	}
	return "containerd-shim-" + runtimeName + "-" + runtimeVersion
}
