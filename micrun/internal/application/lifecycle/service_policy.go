package lifecycle

import (
	"time"

	"micrun/internal/ports"
	log "micrun/internal/support/logger"
)

const defaultAutoCloseTimeout = 30 * time.Second

type waitPolicy struct {
	autoClose bool
	timeout   time.Duration
}

type waitPolicyDecision struct {
	autoClose bool
	timeout   time.Duration
	logFn     func(string)
}

func resolveWaitPolicy(taskHandle ports.Task) waitPolicy {
	annotations := taskHandle.Annotations()
	reportDeprecatedWaitPolicyAnnotations(taskHandle.ID(), annotations)

	parsed := parseWaitPolicyAnnotations(annotations)
	decision := waitPolicyDecisionForTask(taskHandle.Terminal(), parsed)
	if decision.logFn != nil {
		decision.logFn(taskHandle.ID())
	}

	autoClose := decision.autoClose && !taskHandle.IsCriSandbox()

	return waitPolicy{
		autoClose: autoClose,
		timeout:   decision.timeout,
	}
}

func waitPolicyDecisionForTask(terminal bool, parsed waitPolicyAnnotations) waitPolicyDecision {
	switch {
	case parsed.timeoutSet && parsed.timeout == 0:
		return waitPolicyDecision{
			autoClose: false,
			timeout:   parsed.timeout,
			logFn: func(id string) {
				log.Infof("[TIMEOUT] Auto-close disabled by zero timeout for %s", id)
			},
		}
	case parsed.timeoutSet:
		timeout := parsed.timeout
		return waitPolicyDecision{
			autoClose: true,
			timeout:   timeout,
			logFn: func(id string) {
				if parsed.autoCloseSet && !parsed.autoClose {
					log.Warnf("[TIMEOUT] auto_close_timeout=%v takes priority over auto_close=false for %s", timeout, id)
				}
				log.Infof("[TIMEOUT] Auto-close enabled with timeout %v for %s", timeout, id)
			},
		}
	case parsed.autoCloseSet && !parsed.autoClose:
		return waitPolicyDecision{
			autoClose: false,
			timeout:   parsed.timeout,
			logFn: func(id string) {
				log.Infof("[TIMEOUT] Auto-close disabled by annotation for %s", id)
			},
		}
	case parsed.autoCloseSet:
		return waitPolicyDecision{
			autoClose: true,
			timeout:   defaultAutoCloseTimeout,
			logFn: func(id string) {
				log.Infof("[TIMEOUT] Auto-close enabled with default timeout %v for %s (auto_close=true)", defaultAutoCloseTimeout, id)
			},
		}
	default:
		mode := "foreground"
		if !terminal {
			mode = "non-TTY"
		}
		return waitPolicyDecision{
			autoClose: true,
			timeout:   parsed.timeout,
			logFn: func(id string) {
				log.Infof("[TIMEOUT] Auto-close enabled with default timeout %v for %s (%s)", parsed.timeout, id, mode)
			},
		}
	}
}
