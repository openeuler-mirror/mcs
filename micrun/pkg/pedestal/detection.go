package pedestal

import (
	"context"
	"fmt"
	"sync"

	defs "micrun/definitions"
	log "micrun/logger"
	"micrun/pkg/utils"
	"os/exec"
	"time"
)

var (
	hostPedCache PedType
	hostPedOnce  sync.Once
	// TODO: considering calculate default max vcpu number when
	DefaultMaxVCPUs uint32
)

// GetHostPed returns the host pedestal type with lazy initialization and caching
// This is the preferred function for new code
func GetHostPed() PedType {
	hostPedOnce.Do(func() {
		hostPedCache = computeHostPed()
	})
	return hostPedCache
}

// computeHostPed performs the actual pedestal type detection
func computeHostPed() PedType {
	if defs.IsMock || detectXen() {
		return Xen
	}

	if detectACRN() {
		return ACRN
	}
	return Unsupported
}

func detectXen() bool {
	if !utils.FileExist("/proc/xen/xenbus") {
		log.Debug("missing xen bus")
		return false
	}

	if err := checkXenKos(); err != nil {
		log.Debugf("xen kernel modules requirements may not met: %v", err)
	}

	return true
}

func checkXenKos() error {
	// xen_gntalloc, xen_gntdev, xen-mcsback
	// TODO: migrate xen-essentials ko to mica-xen related ko
	essentials := []string{"xen_gntalloc", "xen_gntdev", "xen_mcsback"}
	for i, ko := range essentials {
		loaded, err := utils.KoLoaded(ko)
		if err != nil {
			return err
		}
		if !loaded {
			err = utils.FindAndLoadKo(ko)
			return fmt.Errorf("kernel module %s is not loaded", essentials[i])
		}
	}
	return nil
}

func checkXLCommand() error {
	path, err := exec.LookPath("xl")
	if err != nil {
		return fmt.Errorf("xl not found in PATH: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	// 3. 执行命令并捕获输出
	cmd := exec.CommandContext(ctx, path, "vcpu-list")
	output, err := cmd.CombinedOutput() // 合并stdout/stderr

	if err != nil {
		return fmt.Errorf("command failed: %v\nOutput: %s", err, output)
	}

	if len(output) == 0 {
		return fmt.Errorf("command produced no output")
	}

	return nil
}

func detectACRN() bool {
	return false
}

const hpsupport = false

// for xen, if ballooning driver was enable, hugepage is not supported
func HugePageSupport(dynamicMem bool) bool {
	if dynamicMem || GetHostPed() != Xen {
		return false
	}

	if ConflictKoLoaded, err := utils.KoLoaded(balloonDriverName); err != nil && hpsupport {
		return !ConflictKoLoaded
	}

	// default: not support
	return false
}
