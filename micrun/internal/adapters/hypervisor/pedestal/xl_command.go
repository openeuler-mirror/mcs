package pedestal

import (
	"context"
	"os/exec"

	"micrun/internal/support/contextx"
)

type xlSubCmd string

const (
	info        xlSubCmd = "info"
	vcpulist    xlSubCmd = "vcpu-list"
	vcpupin     xlSubCmd = "vcpu-pin"
	vcpuset     xlSubCmd = "vcpu-set"
	vmlist      xlSubCmd = "list"
	pause       xlSubCmd = "pause"
	resume      xlSubCmd = "unpause"
	domid       xlSubCmd = "domid"
	memset      xlSubCmd = "mem-set"
	memmax      xlSubCmd = "mem-max"
	schedcredit xlSubCmd = "sched-credit2"
)

func newXLContext(ctx context.Context, subcmd xlSubCmd, args ...string) *exec.Cmd {
	ctx = contextx.OrBackground(ctx)
	cmdArgs := []string{string(subcmd)}
	cmdArgs = append(cmdArgs, args...)
	return exec.CommandContext(ctx, "xl", cmdArgs...)
}
