package lifecycle

import (
	"io"
	"testing"
	"time"

	"micrun/internal/ports"
	ann "micrun/internal/support/annotations"

	"github.com/containerd/containerd/api/types/task"
)

type policyTask struct {
	id           string
	terminal     bool
	isCriSandbox bool
	annotations  map[string]string
}

func (p policyTask) ID() string                      { return p.id }
func (p policyTask) Bundle() string                  { return "" }
func (p policyTask) PID() uint32                     { return 0 }
func (p policyTask) Status() task.Status             { return task.Status_UNKNOWN }
func (p policyTask) SetStatus(task.Status)           {}
func (p policyTask) Terminal() bool                  { return p.terminal }
func (p policyTask) StdinPath() string               { return "" }
func (p policyTask) StdoutPath() string              { return "" }
func (p policyTask) StderrPath() string              { return "" }
func (p policyTask) ExitStatus() uint32              { return 0 }
func (p policyTask) ExitTime() time.Time             { return time.Time{} }
func (p policyTask) SetExitInfo(uint32, time.Time)   {}
func (p policyTask) StdinPipe() io.WriteCloser       { return nil }
func (p policyTask) StdinCloser() chan struct{}      { return nil }
func (p policyTask) ExitChan() chan struct{}         { return nil }
func (p policyTask) IOExit()                         {}
func (p policyTask) CanBeSandbox() bool              { return false }
func (p policyTask) IsCriSandbox() bool              { return p.isCriSandbox }
func (p policyTask) Annotations() map[string]string  { return p.annotations }
func (p policyTask) IOManager() ports.IOManager      { return nil }
func (p policyTask) SetIOManager(ports.IOManager)    {}
func (p policyTask) AttachInfo() *ports.AttachInfo   { return nil }
func (p policyTask) SetAttachInfo(*ports.AttachInfo) {}
func (p policyTask) SetStdinPipe(io.WriteCloser)     {}
func (p policyTask) SetAttached(bool) bool           { return false }

func TestResolveWaitPolicyDisablesAutoCloseOnZeroTimeout(t *testing.T) {
	policy := resolveWaitPolicy(policyTask{
		id: "task-1",
		annotations: map[string]string{
			ann.AutoCloseTimeout: "0",
		},
	})

	if policy.autoClose {
		t.Fatal("expected autoClose to be disabled")
	}
	if policy.timeout != 0 {
		t.Fatalf("expected timeout 0, got %v", policy.timeout)
	}
}

func TestResolveWaitPolicyTimeoutOverridesAutoCloseFalse(t *testing.T) {
	policy := resolveWaitPolicy(policyTask{
		id: "task-1",
		annotations: map[string]string{
			ann.AutoClose:        "false",
			ann.AutoCloseTimeout: "45s",
		},
	})

	if !policy.autoClose {
		t.Fatal("expected autoClose to be enabled when timeout is set")
	}
	if policy.timeout != 45*time.Second {
		t.Fatalf("expected timeout 45s, got %v", policy.timeout)
	}
}

func TestResolveWaitPolicyDisablesAutoCloseWhenExplicitFalse(t *testing.T) {
	policy := resolveWaitPolicy(policyTask{
		id: "task-1",
		annotations: map[string]string{
			ann.AutoClose: "false",
		},
	})

	if policy.autoClose {
		t.Fatal("expected explicit auto_close=false to disable auto-close")
	}
	if policy.timeout != defaultAutoCloseTimeout {
		t.Fatalf("expected default timeout to be retained, got %v", policy.timeout)
	}
}

func TestResolveWaitPolicyEnablesDefaultAutoCloseWhenUnspecified(t *testing.T) {
	for _, tc := range []struct {
		name     string
		terminal bool
	}{
		{name: "tty", terminal: true},
		{name: "notty", terminal: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := resolveWaitPolicy(policyTask{
				id:       "task-1",
				terminal: tc.terminal,
			})

			if !policy.autoClose {
				t.Fatal("expected default auto-close to be enabled")
			}
			if policy.timeout != defaultAutoCloseTimeout {
				t.Fatalf("expected default timeout, got %v", policy.timeout)
			}
		})
	}
}

func TestResolveWaitPolicyDisablesAutoCloseForCriSandbox(t *testing.T) {
	policy := resolveWaitPolicy(policyTask{
		id:           "task-1",
		isCriSandbox: true,
		annotations: map[string]string{
			ann.AutoClose: "true",
		},
	})

	if policy.autoClose {
		t.Fatal("expected cri sandbox to disable autoClose")
	}
	if policy.timeout != defaultAutoCloseTimeout {
		t.Fatalf("expected default timeout, got %v", policy.timeout)
	}
}

func TestResolveWaitPolicyDisablesAutoCloseForCriSandboxEvenWithTimeout(t *testing.T) {
	policy := resolveWaitPolicy(policyTask{
		id:           "task-1",
		isCriSandbox: true,
		annotations: map[string]string{
			ann.AutoCloseTimeout: "5s",
		},
	})

	if policy.autoClose {
		t.Fatal("expected cri sandbox to disable autoClose even when timeout is set")
	}
	if policy.timeout != 5*time.Second {
		t.Fatalf("expected timeout 5s, got %v", policy.timeout)
	}
}

func TestParseWaitPolicyAnnotationsCapturesSetFlags(t *testing.T) {
	parsed := parseWaitPolicyAnnotations(map[string]string{
		ann.AutoClose:        " false ",
		ann.AutoCloseTimeout: " 12s ",
	})

	if parsed.autoClose {
		t.Fatal("autoClose = true, want false")
	}
	if !parsed.autoCloseSet {
		t.Fatal("autoCloseSet = false, want true")
	}
	if parsed.timeout != 12*time.Second {
		t.Fatalf("timeout = %v, want 12s", parsed.timeout)
	}
	if !parsed.timeoutSet {
		t.Fatal("timeoutSet = false, want true")
	}
}

func TestParseWaitPolicyAnnotationsIgnoresNumericAutoCloseValue(t *testing.T) {
	parsed := parseWaitPolicyAnnotations(map[string]string{
		ann.AutoClose: "5",
	})

	if !parsed.autoClose {
		t.Fatal("numeric auto_close value should fall back to default true")
	}
	if parsed.autoCloseSet {
		t.Fatal("numeric auto_close value must not be treated as an explicit boolean")
	}
	if parsed.timeout != defaultAutoCloseTimeout || parsed.timeoutSet {
		t.Fatalf("timeout = %v set=%v, want default unset", parsed.timeout, parsed.timeoutSet)
	}
}

func TestParseWaitPolicyAnnotationsAcceptsNumericSecondsTimeout(t *testing.T) {
	parsed := parseWaitPolicyAnnotations(map[string]string{
		ann.AutoCloseTimeout: "7",
	})

	if parsed.timeout != 7*time.Second {
		t.Fatalf("timeout = %v, want 7s", parsed.timeout)
	}
	if !parsed.timeoutSet {
		t.Fatal("timeoutSet = false, want true")
	}
}

func TestHasAnnotationIgnoresWhitespaceOnlyValues(t *testing.T) {
	if hasAnnotation(map[string]string{
		ann.OldAutoCloseTimeout: " \t\n ",
	}, ann.OldAutoCloseTimeout) {
		t.Fatal("expected whitespace-only annotation to be ignored")
	}
}

func TestGetDurationAnnotationDefaultsNegativeValues(t *testing.T) {
	for _, value := range []string{"-1s", "-1"} {
		t.Run(value, func(t *testing.T) {
			got, set := getDurationAnnotation(map[string]string{
				ann.AutoCloseTimeout: value,
			}, ann.AutoCloseTimeout, defaultAutoCloseTimeout)
			if got != defaultAutoCloseTimeout {
				t.Fatalf("duration = %v, want default %v", got, defaultAutoCloseTimeout)
			}
			if !set {
				t.Fatal("set = false, want true")
			}
		})
	}
}

func TestWaitPolicyDecisionForTask(t *testing.T) {
	parsed := parseWaitPolicyAnnotations(map[string]string{
		ann.AutoClose:        "false",
		ann.AutoCloseTimeout: "30s",
	})

	decision := waitPolicyDecisionForTask(true, parsed)
	if !decision.autoClose {
		t.Fatal("expected timeout to override auto-close and enable wait")
	}
	if decision.timeout != 30*time.Second {
		t.Fatalf("expected timeout 30s, got %v", decision.timeout)
	}
}

func TestWaitPolicyDecisionForTaskZeroTimeoutDisablesAutoClose(t *testing.T) {
	parsed := parseWaitPolicyAnnotations(map[string]string{
		ann.AutoCloseTimeout: "0",
	})

	decision := waitPolicyDecisionForTask(true, parsed)
	if decision.autoClose {
		t.Fatal("expected auto-close disabled for zero timeout")
	}
	if decision.timeout != 0 {
		t.Fatalf("expected timeout 0, got %v", decision.timeout)
	}
}
