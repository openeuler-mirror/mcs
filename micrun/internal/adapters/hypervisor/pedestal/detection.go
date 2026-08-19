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
	autoLoaded := []string{}
	for _, ko := range essentials {
		loaded, err := sys.KoLoaded(ko)
		if err != nil {
			return err
		}
		if loaded {
			continue
		}
		if loadErr := sys.FindAndLoadKo(ko); loadErr != nil {
			return fmt.Errorf("kernel module %s is not loaded and could not be loaded: %w", ko, loadErr)
		}
		autoLoaded = append(autoLoaded, ko)
	}
	if len(autoLoaded) > 0 {
		log.Debugf("auto-loaded xen kernel modules: %v", autoLoaded)
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

// hpsupport gates hugepage support. Currently disabled: enabling it requires
// verifying the balloon-driver conflict handling across all supported pedestals.
const hpsupport = false

// HugePageSupport reports whether hugepages may be used. The parameter is the
// sandbox's staticResource flag (static allocation => ballooning off), which
// is the inverse of "dynamic memory (ballooning) enabled". For Xen, hugepages
// are unsupported when memory ballooning is enabled, because the balloon
// driver conflicts with hugepage allocation. When the hpsupport flag is
// disabled, hugepage support is always reported as off.
func (f *PedestalFacade) HugePageSupport(staticResource bool) bool {
	dynamicMem := !staticResource
	if !hpsupport || dynamicMem || f == nil || f.Type() != Xen {
		return false
	}
	// Determine whether the balloon driver is actually loaded. The previous
	// implementation branched on `err != nil`, which acted only when the lookup
	// FAILED and reported hugepage as supported precisely when the state could
	// not be determined — the opposite of the intent. Query on success and be
	// conservative (unsupported) when the lookup errors out.
	conflictKoLoaded, err := sys.KoLoaded(balloonDriverName)
	if err != nil {
		log.Debugf("HugePageSupport: cannot determine balloon driver state: %v", err)
		return false
	}
	return !conflictKoLoaded
}
