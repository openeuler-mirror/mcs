package netns

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	log "micrun/internal/support/logger"

	"golang.org/x/sys/unix"
)

const HolderArg = "--micrun-netns-holder"

func RunHolderCommand(args []string) bool {
	return runHolderCommand(args, waitForHolderSignal)
}

func runHolderCommand(args []string, wait func()) bool {
	if !isHolderInvocation(args) {
		return false
	}
	wait()
	return true
}

func isHolderInvocation(args []string) bool {
	return len(args) == 1 && args[0] == HolderArg
}

func startHolderCmd() (*exec.Cmd, error) {
	if cmd, err := selfHolderCmd(); err == nil {
		return cmd, nil
	} else {
		log.Debugf("self netns holder unavailable, falling back to external holder: %v", err)
	}
	return externalHolderCmd()
}

func selfHolderCmd() (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate current executable: %w", err)
	}
	if !isMicrunExecutable(exe) {
		return nil, fmt.Errorf("current executable %q is not a micrun shim", exe)
	}
	cmd := exec.Command(exe, HolderArg)
	cmd.SysProcAttr = holderSysProcAttr()
	return cmd, nil
}

func externalHolderCmd() (*exec.Cmd, error) {
	candidates := [][]string{
		{"sleep", "infinity"},
		{"tail", "-f", "/dev/null"},
	}

	var lastErr error
	for _, candidate := range candidates {
		bin, err := exec.LookPath(candidate[0])
		if err != nil {
			lastErr = err
			continue
		}

		cmd := exec.Command(bin, candidate[1:]...)
		cmd.SysProcAttr = holderSysProcAttr()
		return cmd, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no suitable holder command found")
	}
	return nil, fmt.Errorf("netns: %w", lastErr)
}

func holderSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Cloneflags: unix.CLONE_NEWNET,
		Setsid:     true,
	}
}

func isMicrunExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "micrun" || strings.HasPrefix(name, "containerd-shim-")
}

func waitForHolderSignal() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	<-signals
}
