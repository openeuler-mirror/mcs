package sys

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	log "micrun/internal/support/logger"
)

func KoLoaded(name string) (bool, error) {
	staticList, err := loadKoList()
	if err != nil {
		return false, err
	}
	_, ok := staticList[name]
	return ok, nil
}

// loadKoList reads /proc/modules fresh on every call. The kernel module
// list is dynamic (modules can be loaded/unloaded at runtime), so caching
// the snapshot indefinitely would report stale results after FindAndLoadKo
// loads a module. KoLoaded/FindAndLoadKo are called rarely (boot-time
// detection), so a fresh procfs read is cheap.
func loadKoList() (map[string]struct{}, error) {
	f, err := os.Open("/proc/modules")
	if err != nil {
		return nil, fmt.Errorf("cannot open /proc/modules: %w", err)
	}
	defer f.Close()

	result := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		result[fields[0]] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func FindAndLoadKo(name string) error {
	if loaded, _ := KoLoaded(name); loaded {
		return nil
	}

	cmd := exec.Command("modprobe", name)
	if output, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else {
		log.Debugf("modprobe failed for %s: %v, output: %s", name, err, string(output))
	}

	modulePaths := []string{
		"/lib/modules/$(uname -r)/kernel/drivers/",
		"/lib/modules/$(uname -r)/extra/",
		"/lib/modules/$(uname -r)/",
		"/usr/lib/modules/$(uname -r)/kernel/drivers/",
		"/usr/lib/modules/$(uname -r)/extra/",
		"/usr/lib/modules/$(uname -r)/",
	}

	for _, basePath := range modulePaths {
		expandedPath := expandKernelPath(basePath)

		moduleFile := findModuleFile(expandedPath, name)
		if moduleFile != "" {
			cmd := exec.Command("insmod", moduleFile)
			if output, err := cmd.CombinedOutput(); err == nil {
				return nil
			} else {
				log.Debugf("insmod failed for %s: %v, output: %s", moduleFile, err, string(output))
			}
		}
	}

	return fmt.Errorf("failed to find and load kernel module: %s", name)
}

func expandKernelPath(path string) string {
	if strings.Contains(path, "$(uname -r)") {
		cmd := exec.Command("uname", "-r")
		kernelRelease, err := cmd.Output()
		if err != nil {
			log.Debugf("failed to get kernel release: %v", err)
			return strings.Replace(path, "$(uname -r)", "", -1)
		}
		kernelStr := strings.TrimSpace(string(kernelRelease))
		return strings.Replace(path, "$(uname -r)", kernelStr, -1)
	}
	return path
}

// moduleExtensions lists the on-disk suffixes a kernel module file may carry,
// in lookup order. Modern distributions (recent Debian/Ubuntu/Fedora/Arch)
// ship modules compressed with zstd/xz/gzip; matching only the bare ".ko"
// suffix (as the previous implementation did) silently misses every module on
// such kernels and leaves the manual insmod fallback dead. kmod decompresses
// these transparently, so the compressed path can be handed to insmod verbatim.
var moduleExtensions = []string{".ko", ".ko.zst", ".ko.xz", ".ko.gz"}

func findModuleFile(dirPath, moduleName string) string {
	for _, ext := range moduleExtensions {
		exactPath := filepath.Join(dirPath, moduleName+ext)
		if _, err := os.Stat(exactPath); err == nil {
			return exactPath
		}
	}

	foundPath := ""
	if err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if baseName, ok := moduleBaseName(info.Name()); ok && baseName == moduleName {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	}); err != nil {
		return ""
	}

	return foundPath
}

// moduleBaseName strips a recognized module-file extension from name and
// reports whether name was a module file at all.
func moduleBaseName(name string) (string, bool) {
	for _, ext := range moduleExtensions {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext), true
		}
	}
	return "", false
}
