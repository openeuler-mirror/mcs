package pedestal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"micrun/internal/support/contextx"
)

// xlCommandTimeout is the default upper bound for a single xl / xenstore
// invocation when the caller's context carries no deadline (most lifecycle
// paths run under context.Background or WithoutCancel). xl talks to a local
// hypervisor and normally answers in milliseconds; a hung xl (xenstored
// wedged, dom0 under pressure) would otherwise block forever while the caller
// holds the sandbox lifecycleLock — freezing every lifecycle RPC on the shim.
const xlCommandTimeout = 30 * time.Second

type xlSubCmd string

const (
	info        xlSubCmd = "info"
	vcpulist    xlSubCmd = "vcpu-list"
	vcpupin     xlSubCmd = "vcpu-pin"
	vcpuset     xlSubCmd = "vcpu-set"
	vmlist      xlSubCmd = "list"
	pause       xlSubCmd = "pause"
	resume      xlSubCmd = "unpause"
	destroy     xlSubCmd = "destroy"
	domid       xlSubCmd = "domid"
	memset      xlSubCmd = "mem-set"
	memmax      xlSubCmd = "mem-max"
	schedcredit xlSubCmd = "sched-credit2"
)

func newXLContext(ctx context.Context, subcmd xlSubCmd, args ...string) *exec.Cmd {
	ctx = contextx.OrBackground(ctx)
	cmdArgs := []string{string(subcmd)}
	cmdArgs = append(cmdArgs, args...)
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		// Bound the subprocess even for Background/WithoutCancel callers.
		// The cancel func is invoked from cmd.Cancel when the deadline
		// fires; on the normal path (command finishes first) the timer
		// simply expires and releases itself ≤30s later — a negligible
		// cost, and it keeps this helper's signature unchanged for the
		// many single-shot callers.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, xlCommandTimeout)
		cmd := exec.CommandContext(ctx, "xl", cmdArgs...)
		cmd.Cancel = func() error {
			defer cancel()
			// A process that already exited must not turn the context
			// timeout into "process already finished" — callers classify
			// DeadlineExceeded as "xl timed out".
			if err := cmd.Process.Kill(); errors.Is(err, os.ErrProcessDone) {
				return nil
			} else {
				return err
			}
		}
		return cmd
	}
	return exec.CommandContext(ctx, "xl", cmdArgs...)
}
