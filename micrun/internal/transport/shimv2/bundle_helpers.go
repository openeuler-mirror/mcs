package shim

import (
	"fmt"
	"os"

	oci "micrun/internal/adapters/config/oci"
	"micrun/internal/support/fs"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Validate the bundle and rootfs.
func validBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("container ID is empty")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("bundle path is required")
	}

	// resolve path first to handle symlinks before other checks
	resolved, err := fs.ResolvePath(bundlePath)
	if err != nil {
		return "", err
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid resolved bundle path '%s': %w", resolved, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("invalid resolved bundle path '%s', it should be a directory", resolved)
	}

	return resolved, nil
}

func loadSpec(id, bundle string) (*specs.Spec, string, error) {
	bundle, err := validBundle(id, bundle)
	if err != nil {
		return nil, "", err
	}
	spec, err := oci.LoadSpec(bundle)
	if err != nil {
		return nil, "", err
	}

	return &spec, bundle, nil
}
