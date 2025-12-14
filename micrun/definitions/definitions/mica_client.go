package defs

// Client default values.
const (
	// pass "<bundle>/rootfs/<DefaultXenImg>" to pedestalCfg for xen-mica case
	// all these default values should be in configuration
	DefaultXenImg       = "image.bin"
	DefaultFirmwareName = "firmware.elf"
	DefaultMinMemMB     = 16
)

var (
	TrustyOS = [...]string{"zephyr", "uniproton", "linux", "liteos"}
)
