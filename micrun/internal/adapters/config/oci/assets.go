package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"micrun/internal/adapters/hypervisor/pedestal"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
)

func bundleRootfs(bundle string) string {
	return filepath.Join(bundle, "rootfs")
}

// resolvePedestalPath resolves pedestal image path with precedence: annotation > runtime default.
// Returns an absolute path if the file exists, otherwise returns the path as-is for later validation.
func resolvePedestalPath(baseRootfs, annotationPedestal string) string {
	var pedPath string

	// Try annotation path first
	if annotationPedestal != "" {
		candidatePath := getBundleImageFile(baseRootfs, annotationPedestal)
		if candidatePath != "" {
			pedPath = candidatePath
			log.Debugf("using pedestal from annotation: %s", pedPath)
			return pedPath
		}
	}

	// Fallback to default image name and try to find it in rootfs
	defaultPath := getBundleImageFile(baseRootfs, defs.DefaultXenImg)
	if defaultPath != "" {
		pedPath = defaultPath
		log.Debugf("using default pedestal path: %s", pedPath)
		return pedPath
	}

	// Return the relative path as fallback - will be validated/used by micad
	log.Debugf("pedestal file not found in rootfs, using relative path: %s", defs.DefaultXenImg)
	return defs.DefaultXenImg
}

// extPedConfig extracts and validates pedestal configuration from annotations.
// Returns pedestal type, config path, or error if validation fails.
// This includes checking pedestal type compatibility and resolving Xen image paths.
func extPedConfig(getAnnotation func(string) (string, bool), baseRootfs string, hostProfile HostProfile) (pedestal.PedType, string, error) {
	pedtype := normalizeHostProfile(hostProfile).Type

	// if pedType is not specified, use host ped type, skip matching
	if pedAnnoation, ok := getAnnotation(ann.Pedtype); ok {
		if pedAnnoation != pedtype.String() {
			return pedtype, "", fmt.Errorf("hypervisor type mismatched: expect %s but found %s", pedtype.String(), pedAnnoation)
		}
	}

	var pedconfAnnotation string
	if cfg, ok := getAnnotation(ann.PedestalConf); ok {
		pedconfAnnotation = cfg
		log.Debugf("pedestal config path from annotation: %s", pedconfAnnotation)
	}

	var pedconf string
	if pedtype == pedestal.Xen {
		// Resolve Xen pedestal image path to absolute path
		pedconf = resolvePedestalPath(baseRootfs, pedconfAnnotation)
		log.Debugf("Resolved Xen pedestal config path: %s", pedconf)
	} else {
		pedconf = pedconfAnnotation
	}

	return pedtype, pedconf, nil
}

// resolveFirmwarePath resolves firmware ELF file path with precedence:
// annotation > fallback path (sandbox/runtime default) > runtime discovery.
func resolveFirmwarePath(baseRootfs, annotationFirmware, fallbackFirmwarePath string) (string, error) {
	if annotationFirmware != "" {
		fwPath := getBundleImageFile(baseRootfs, annotationFirmware)
		if fwPath == "" {
			return "", fmt.Errorf("firmware file not found: %s", annotationFirmware)
		}
		return fwPath, nil
	}

	if fallback := strings.TrimSpace(fallbackFirmwarePath); fallback != "" {
		if abs, err := fs.ResolvePath(fallback); err == nil && fs.FileExist(abs) {
			log.Debugf("using fallback firmware path: %s", abs)
			return abs, nil
		}
		if bundlePath := getBundleImageFile(baseRootfs, fallback); bundlePath != "" {
			log.Debugf("using fallback firmware from bundle path: %s", bundlePath)
			return bundlePath, nil
		}
		return "", fmt.Errorf("fallback firmware file not found: %s", fallback)
	}

	defaultPath := getBundleImageFile(baseRootfs, defs.DefaultFirmwareName)
	if defaultPath != "" {
		log.Debugf("using default elf path: %s", defaultPath)
		return defaultPath, nil
	}

	candidates, _ := filepath.Glob(filepath.Join(baseRootfs, "*.elf"))
	if len(candidates) > 0 {
		log.Debugf("found RTOS image file: %s", candidates[0])
		return candidates[0], nil
	}

	return "", fmt.Errorf("no RTOS image file found in container rootfs and no firmware path provided via annotation or runtime configuration")
}

func verifyContainerFirmwareHash(elfPath string, annotations map[string]string) error {
	expected, ok := getAnnotation(ann.FirmwareHash, annotations)
	if !ok {
		return nil
	}
	if elfPath == "" {
		return fmt.Errorf("firmware hash annotation set but firmware path is empty")
	}
	return verifySHA256File(elfPath, expected)
}

func verifySHA256File(path, expected string) error {
	expected, err := normalizeSHA256Digest(expected)
	if err != nil {
		return err
	}

	got, err := sha256FileDigest(path)
	if err != nil {
		return fmt.Errorf("failed to hash firmware %s: %w", path, err)
	}
	if got != expected {
		return fmt.Errorf("firmware sha256 mismatch for %s: got %s, want %s", path, got, expected)
	}
	return nil
}

func normalizeSHA256Digest(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimPrefix(normalized, "sha256:")
	if len(normalized) != sha256.Size*2 {
		return "", fmt.Errorf("invalid firmware sha256 length: %q", value)
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return "", fmt.Errorf("invalid firmware sha256 digest %q: %w", value, err)
	}
	return normalized, nil
}

// prepCache prepares a stable container cache directory and copies regular
// bundle assets there. The cache stays enabled because containerd may unmount
// the bundle before micad consumes pedestal and firmware paths.
func prepCache(id, pedconf, elfPath string, hostProfile HostProfile, stateDir string) (string, string, error) {
	// Create a dedicated directory for the container to cache firmware, image, etc.
	// This avoids race conditions with the bundle being unmounted by containerd.
	cacheRoot, err := containerCacheRootForStateDir(stateDir)
	if err != nil {
		return "", "", err
	}
	containerCacheDir := filepath.Join(cacheRoot, id)
	if err := os.MkdirAll(containerCacheDir, defs.DirMode); err != nil {
		return "", "", fmt.Errorf("failed to create container cache directory %s: %w", containerCacheDir, err)
	}

	if pedconf, err = cacheRegularFile(containerCacheDir, pedconf); err != nil && normalizeHostProfile(hostProfile).Type == pedestal.Xen {
		return "", "", err
	}
	if elfPath, err = cacheRegularFile(containerCacheDir, elfPath); err != nil {
		return "", "", err
	}
	return pedconf, elfPath, nil
}

// ContainerCacheRoot returns the container asset cache root for a runtime state directory.
func ContainerCacheRoot(stateDir string) string {
	trimmed := strings.TrimSpace(stateDir)
	if trimmed == "" {
		return defs.DefaultMicaContainersRoot
	}
	return filepath.Join(trimmed, "containers")
}

func containerCacheRootForStateDir(stateDir string) (string, error) {
	root := ContainerCacheRoot(stateDir)
	cleanRoot, err := fs.CleanAbsolutePath(root)
	if err != nil {
		return "", fmt.Errorf("container cache root is invalid: %w", err)
	}
	return cleanRoot, nil
}

// getBundleImageFile now does path conversion and simple file existence check
func getBundleImageFile(dir, p string) string {
	baseRootfs, err := fs.ResolvePath(dir)
	if err != nil {
		return ""
	}

	rp := bundleFilePath(baseRootfs, p)
	if rp == "" {
		return ""
	}
	if abs, err := fs.ResolvePath(rp); err == nil {
		if !pathWithinBase(baseRootfs, abs) {
			log.Debugf("rejecting bundle path outside rootfs: %s -> %s", p, abs)
			return ""
		}
		log.Debugf("resolved path (to be validated later): %s -> %s", p, abs)
		if fs.FileExist(abs) {
			return abs
		}
	}
	return ""
}

// bundleFilePath resolves a container bundle file path to a host path under baseRootfs.
func bundleFilePath(dir, p string) string {
	baseRootfs, err := fs.CleanAbsolutePath(dir)
	if err != nil {
		return ""
	}

	trimmed := strings.TrimSpace(p)
	if trimmed == "" || strings.ContainsRune(trimmed, '\x00') {
		return ""
	}

	relative := strings.TrimLeft(trimmed, `/\`)
	if relative == "" {
		return ""
	}
	canonical := strings.ReplaceAll(relative, "\\", "/")
	for _, segment := range strings.Split(canonical, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ""
		}
	}

	return filepath.Join(baseRootfs, filepath.FromSlash(canonical))
}

func pathWithinBase(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
