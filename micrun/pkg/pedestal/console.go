package pedestal

import (
	"fmt"
	log "micrun/logger"
	"os"
)

// ConsolePTYPathForDomain resolves the PTY path published by xl console for a given domain.
func ConsolePTYPathForDomain(id string) (string, error) {
	ptyPath, err := xenStoreRead(id, "console/tty")
	if err != nil {
		return "", fmt.Errorf("failed to read PTY path from XenStore: %w", err)
	}

	log.Debugf("PTY path for domain %s: %s", id, ptyPath)
	path := ptyPath
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("console PTY %s not found: %w", path, err)
	}

	return path, nil
}
