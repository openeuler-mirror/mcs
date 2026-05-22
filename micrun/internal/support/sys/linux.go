package sys

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

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

var (
	loaded   map[string]struct{}
	loadOnce sync.Once
	loadErr  error
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

func findModuleFile(dirPath, moduleName string) string {
	exactPath := filepath.Join(dirPath, moduleName+".ko")
	if _, err := os.Stat(exactPath); err == nil {
		return exactPath
	}

	foundPath := ""
	if err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ko") {
			baseName := strings.TrimSuffix(info.Name(), ".ko")
			if baseName == moduleName {
				foundPath = path
				return filepath.SkipAll
			}
		}
		return nil
	}); err != nil {
		return ""
	}

	return foundPath
}
