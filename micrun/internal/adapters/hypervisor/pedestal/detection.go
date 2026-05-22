package pedestal

import (
	"fmt"
	"os"
	"strings"
	"sync"

	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
	"micrun/internal/support/sys"
)

const baremetalDetectionEnv = "MICRUN_ENABLE_BAREMETAL"

var (
	hostPedCache PedType
	hostPedOnce  sync.Once
)

type hostPedestalDetector struct {
	isMock      func() bool
	isXen       func() bool
	isBaremetal func() bool
}

var defaultHostPedestalDetector = hostPedestalDetector{
	isMock: func() bool {
		return defs.IsMock
	},
	isXen:       detectXen,
	isBaremetal: detectBaremetal,
}

func (d hostPedestalDetector) detect() PedType {
	if d.isMock != nil && d.isMock() {
		return Xen
	}
	if d.isXen != nil && d.isXen() {
		return Xen
	}
	if d.isBaremetal != nil && d.isBaremetal() {
		return Baremetal
	}
	return Unsupported
}

// hostPed returns the host pedestal type with lazy initialization and caching
// This is the preferred function for new code
func hostPed() PedType {
	hostPedOnce.Do(func() {
		hostPedCache = computeHostPed()
	})
	if defs.IsMock {
		return Xen
	}
	return hostPedCache
}

// computeHostPed performs the actual pedestal type detection
func computeHostPed() PedType {
	return defaultHostPedestalDetector.detect()
}

func detectXen() bool {
	if !fs.FileExist("/proc/xen/xenbus") {
		log.Debug("missing xen bus")
		return false
	}

	if err := checkXenKos(); err != nil {
		log.Debugf("xen kernel modules requirements may not met: %v", err)
	}

	return true
}

func checkXenKos() error {
	essentials := []string{"xen_gntalloc", "xen_gntdev", "xen_mcsback"}
	for i, ko := range essentials {
		loaded, err := sys.KoLoaded(ko)
		if err != nil {
			return err
		}
		if !loaded {
			_ = sys.FindAndLoadKo(ko)
			return fmt.Errorf("kernel module %s is not loaded", essentials[i])
		}
	}
	return nil
}

// detectBaremetal enables baremetal only when explicitly requested. This keeps
// the default host detection conservative while preserving a tested path for
// baremetal deployments.
func detectBaremetal() bool {
	return envFlagEnabled(baremetalDetectionEnv)
}

func envFlagEnabled(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "no":
		return false
	default:
		return true
	}
}

const hpsupport = false

// for xen, if ballooning driver was enable, hugepage is not supported
func (f *PedestalFacade) HugePageSupport(dynamicMem bool) bool {
	if dynamicMem || f == nil || f.Type() != Xen {
		return false
	}

	if ConflictKoLoaded, err := sys.KoLoaded(balloonDriverName); err != nil && hpsupport {
		return !ConflictKoLoaded
	}

	return false
}
