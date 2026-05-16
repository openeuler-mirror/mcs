package container

import (
	"fmt"
	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
	"strings"
)

func validOS(os string) bool {
	return defs.IsSupportedGuestOS(os)
}

func validComponentFile(component string) bool {
	if !fs.IsRegular(component) {
		log.Tracef("validComponentFile: %s is not a regular file", component)
		return false
	}
	return true
}

func validFirmware(firmware string) bool {
	return validComponentFile(firmware)
}

func validBinfile(binpath string) bool {
	return validComponentFile(binpath)
}

func (c *Container) validateMicaContainer() error {
	if c == nil || c.config == nil {
		return fmt.Errorf("container config is required")
	}
	if c.config.IsInfra {
		return nil
	}

	os := c.os()
	firmware := c.getFirmware()
	log.Tracef("validateMicaContainer: os=%q, firmware=%q", os, firmware)

	if !validOS(os) {
		return fmt.Errorf("unsupported guest os %q (supported: %s)", os, strings.Join(defs.SupportedGuestOS(), ", "))
	}
	if !validFirmware(firmware) {
		return fmt.Errorf("invalid firmware path %q", firmware)
	}
	if c.config.PedestalType == PedestalXen {
		pedConf := c.getPedConf()
		if !validBinfile(pedConf) {
			return fmt.Errorf("invalid xen pedestal image %q", pedConf)
		}
		log.Tracef("validateMicaContainer: pedConf=%q", pedConf)
	}

	return nil
}
