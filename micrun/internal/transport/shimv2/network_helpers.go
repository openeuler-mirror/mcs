package shim

import (
	"fmt"

	cntr "micrun/internal/domain/container"
	log "micrun/internal/support/logger"
	"micrun/internal/support/netns"
)

func setupNetNS(sandboxID string, netcfg *cntr.NetworkConfig) error {
	if netcfg == nil {
		return fmt.Errorf("setup netns: nil network config")
	}

	if netcfg.HolderPid > 0 {
		if path, err := netns.RegisterExisting(sandboxID, netcfg.HolderPid); err == nil {
			netcfg.NetworkID = path
			netcfg.NetworkCreated = true
			return nil
		}
		log.Warnf("existing netns holder pid %d for sandbox %s is invalid; recreating", netcfg.HolderPid, sandboxID)
		netcfg.HolderPid = 0
	}

	pid, path, err := netns.Create(sandboxID)
	if err != nil {
		return err
	}

	netcfg.NetworkID = path
	netcfg.NetworkCreated = true
	netcfg.HolderPid = pid
	return nil
}

func cleanupNetNS(sandboxID string, netcfg *cntr.NetworkConfig) error {
	if netcfg == nil {
		return fmt.Errorf("cleanup netns: nil network config")
	}

	if err := netcfg.NetworkCleanup(sandboxID); err != nil {
		return err
	}
	return nil
}
