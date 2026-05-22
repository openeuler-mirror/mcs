package shim

import (
	"fmt"

	cntr "micrun/internal/domain/container"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
)

func validateFirmwareForContainer(config *cntr.ContainerConfig) error {
	if config.IsInfra {
		log.Debugf("skipping firmware validation for infra container")
		return nil
	}

	var err error
	if config.PedestalType == cntr.PedestalXen {
		if err = validateContainerAsset(config.PedestalConf); err != nil {
			return fmt.Errorf("xen pedestal image validation failed for %s: %w", config.PedestalConf, err)
		}
	}
	err = validateContainerAsset(config.ImageAbsPath)
	if err != nil {
		return fmt.Errorf("failed to validate container image file %s: %w", config.ImageAbsPath, err)
	}

	return nil
}

func validateContainerAsset(path string) error {
	_, err := fs.EnsureRegularFilePath(path)
	return err
}
