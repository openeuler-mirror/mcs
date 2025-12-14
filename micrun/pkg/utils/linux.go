package utils

import (
	"bufio"
	"fmt"
	log "micrun/logger"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// KoLoaded reports whether the named kernel module is present in the running kernel.
// It reads /proc/modules once and caches the parsed list for subsequent calls.
func KoLoaded(name string) (bool, error) {
	staticList, err := loadKoList()
	if err != nil {
		return false, err
	}
	_, ok := staticList[name]
	return ok, nil
}

// loadList parses /proc/modules exactly once and returns the set of loaded modules.
// sync.Once guarantees the file is read only one time
var (
	loaded   map[string]struct{} // cached module names
	loadOnce sync.Once
	loadErr  error // capture the first error, if any
)

func loadKoList() (map[string]struct{}, error) {
	loadOnce.Do(func() {
		f, err := os.Open("/proc/modules")
		if err != nil {
			loadErr = fmt.Errorf("cannot open /proc/modules: %w", err)
			return
		}
		defer f.Close()

		loaded = make(map[string]struct{})
		sc := bufio.NewScanner(f)

		// Each line: "modulename size refs deps state addr"
		// We only need the first token
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) == 0 {
				continue
			}
			loaded[fields[0]] = struct{}{}
		}
		loadErr = sc.Err()
	})
	return loaded, loadErr
}

// FindAndLoadKo searches for the named kernel module and attempts to load it.
// It first checks common module directories, then tries modprobe, and finally insmod.
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

	// If modprobe fails, try to find the module file and use insmod
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

func findModuleFile(dirPath, moduleName string) string {
	exactPath := filepath.Join(dirPath, moduleName+".ko")
	if _, err := os.Stat(exactPath); err == nil {
		return exactPath
	}

	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ko") {
			baseName := strings.TrimSuffix(info.Name(), ".ko")
			if baseName == moduleName {
				return filepath.SkipAll
			}
		}
		return nil
	})

	return ""
}
