package defs

import "sort"

// Client default values.
const (
	// pass "<bundle>/rootfs/<DefaultXenImg>" to pedestalCfg for xen-mica case
	DefaultXenImg = "image.bin"

	DefaultFirmwareName = "firmware.elf"
	// DefaultContainerMinMemMB is the runtime config default for per-container memory reservation.
	DefaultContainerMinMemMB = 32
	// DefaultMinMemMB is the low-level mica resource fallback when no runtime default is available.
	DefaultMinMemMB = 16
	DefaultMaxVCPUs = 8
	DefaultOS       = "uniproton" // Default RTOS type when annotation is missing
)

var supportedGuestOS = map[string]struct{}{
	"zephyr":    {},
	"uniproton": {},
}

func IsSupportedGuestOS(os string) bool {
	_, ok := supportedGuestOS[os]
	return ok
}

func SupportedGuestOS() []string {
	names := make([]string, 0, len(supportedGuestOS))
	for name := range supportedGuestOS {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
