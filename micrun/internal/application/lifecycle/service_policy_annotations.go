package lifecycle

import (
	"math"
	"strconv"
	"strings"
	"time"

	ann "micrun/internal/support/annotations"
	log "micrun/internal/support/logger"
)

type waitPolicyAnnotations struct {
	autoClose    bool
	autoCloseSet bool
	timeout      time.Duration
	timeoutSet   bool
}

func parseWaitPolicyAnnotations(annotations map[string]string) waitPolicyAnnotations {
	autoClose, autoCloseSet := getBoolAnnotation(annotations, ann.AutoClose, true)
	timeout, timeoutSet := getDurationAnnotation(annotations, ann.AutoCloseTimeout, defaultAutoCloseTimeout)
	return waitPolicyAnnotations{
		autoClose:    autoClose,
		autoCloseSet: autoCloseSet,
		timeout:      timeout,
		timeoutSet:   timeoutSet,
	}
}

func reportDeprecatedWaitPolicyAnnotations(taskID string, annotations map[string]string) {
	if hasAnnotation(annotations, ann.OldAutoCloseTimeout) {
		log.Errorf("[TIMEOUT] Annotation '%s' is not supported, use '%s' instead for %s",
			ann.OldAutoCloseTimeout, ann.AutoCloseTimeout, taskID)
	}
}

func getBoolAnnotation(annotations map[string]string, key string, defaultValue bool) (bool, bool) {
	if annotations == nil {
		return defaultValue, false
	}

	if raw, ok := annotations[key]; ok {
		value := strings.TrimSpace(raw)
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed, true
		}
		if _, err := strconv.Atoi(value); err == nil {
			log.Warnf("Boolean annotation '%s' has numeric value '%s'. Did you mean to use '%s' for timeout duration?",
				key, value, ann.AutoCloseTimeout)
		} else {
			log.Warnf("failed to parse boolean annotation '%s' with value '%s', using default: %v",
				key, value, defaultValue)
		}
	}
	return defaultValue, false
}

func getDurationAnnotation(annotations map[string]string, key string, defaultValue time.Duration) (time.Duration, bool) {
	if annotations == nil {
		return defaultValue, false
	}

	if raw, ok := annotations[key]; ok {
		value := strings.TrimSpace(raw)
		duration, parseErr := time.ParseDuration(value)
		if parseErr == nil {
			return normalizeAnnotationDuration(key, value, duration, defaultValue), true
		}
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			// seconds*time.Second overflows for huge positive OR negative
			// values. A large negative bare integer (e.g. -18446744073) wraps
			// to a small positive duration, bypassing the negative-value
			// fallback below and making auto-close fire almost immediately.
			// Reject both bounds so the overflow can never produce a usable
			// (and wrong) duration.
			if seconds > math.MaxInt64/int64(time.Second) || seconds < math.MinInt64/int64(time.Second) {
				log.Errorf("annotation %s value '%s' overflows duration, defaulting to %v", key, value, defaultValue)
				return defaultValue, true
			}
			duration := time.Duration(seconds) * time.Second
			return normalizeAnnotationDuration(key, value, duration, defaultValue), true
		}
		log.Warnf("annotation %s parse error: %v, defaulting to %v", key, parseErr, defaultValue)
	}
	return defaultValue, false
}

func normalizeAnnotationDuration(key, raw string, duration, defaultValue time.Duration) time.Duration {
	if duration < 0 {
		log.Errorf("annotation %s has invalid negative duration %s, using default %v", key, raw, defaultValue)
		return defaultValue
	}
	return duration
}

func hasAnnotation(annotations map[string]string, key string) bool {
	if annotations == nil {
		return false
	}
	val, ok := annotations[key]
	return ok && strings.TrimSpace(val) != ""
}
