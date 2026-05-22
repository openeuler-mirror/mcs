package oci

import (
	"strconv"
	"strings"

	log "micrun/internal/support/logger"
)

func parseRuntimeBool(key, raw string) (bool, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(trimmed)
	if err != nil {
		log.Debugf("failed to parse %s %q into bool: %v", key, raw, err)
		return false, false
	}
	return parsed, true
}

func parseRuntimeUint32(key, raw string) (uint32, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(trimmed, 10, 32)
	if err != nil {
		log.Debugf("failed to parse %s %q into uint32: %v", key, raw, err)
		return 0, false
	}
	return uint32(parsed), true
}
