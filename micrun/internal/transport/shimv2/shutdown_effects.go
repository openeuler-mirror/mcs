package shim

import (
	"errors"
	"os"

	log "micrun/internal/support/logger"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

type shutdownEffects struct {
	readAddress  func(string) (string, error)
	removeSocket func(string) error
	exit         func(int)
}

func shutdownEffectsFrom(deps runtimeDependencies) shutdownEffects {
	return deps.shutdown.withDefaults()
}

func (e shutdownEffects) withDefaults() shutdownEffects {
	if e.readAddress == nil {
		e.readAddress = shimv2.ReadAddress
	}
	if e.removeSocket == nil {
		e.removeSocket = shimv2.RemoveSocket
	}
	if e.exit == nil {
		e.exit = os.Exit
	}
	return e
}

func (s *shimService) runShutdownEffects() {
	if s.ss != nil {
		s.ss()
	}

	effects := s.shutdown.withDefaults()
	if sockAddr, err := effects.readAddress("address"); err == nil && sockAddr != "" {
		if err := effects.removeSocket(sockAddr); err != nil {
			if shouldWarnShutdownSocketRemoval(err) {
				log.Warnf("failed to remove shim socket %s: %v", sockAddr, err)
			} else {
				log.Debugf("shim socket already removed during shutdown: %s", sockAddr)
			}
		}
	}
	effects.exit(0)
}

func shouldWarnShutdownSocketRemoval(err error) bool {
	return err != nil && !errors.Is(err, os.ErrNotExist)
}
