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

// verifyHolderProcess checks that the process at pid is actually one of the
// holder command shapes startHolderCmd can spawn. The pid persisted before a
// shim restart may have been recycled by an unrelated process; without this
// check a later Cleanup would SIGTERM an innocent process.
func verifyHolderProcess(pid int) error {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return fmt.Errorf("netns: holder pid %d cmdline unreadable: %w", pid, err)
	}
	argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if !holderArgvMatches(argv) {
		return fmt.Errorf("netns: pid %d is not a netns holder (argv %v)", pid, argv)
	}
	// An argv shape match is not proof: "sleep infinity" / "tail -f
	// /dev/null" are common commands. A real holder is spawned with
	// CLONE_NEWNET, so its network namespace differs from ours; an innocent
	// host process shares ours. Unreadable links are treated as a mismatch
	// of a different kind (already rejected above) — treat read failures
	// conservatively as different namespaces to not break exotic setups.
	if selfNS, err := os.Readlink("/proc/self/ns/net"); err == nil {
		if targetNS, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", pid)); err == nil && selfNS == targetNS {
			return fmt.Errorf("netns: pid %d shares our network namespace; not an isolated holder", pid)
		}
	}
	return nil
}

func holderArgvMatches(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := filepath.Base(argv[0])
	switch {
	case len(argv) == 2 && argv[1] == HolderArg && isMicrunExecutable(argv[0]):
		return true
	case len(argv) == 2 && base == "sleep" && argv[1] == "infinity":
		return true
	case len(argv) == 3 && base == "tail" && argv[1] == "-f" && argv[2] == "/dev/null":
		return true
	}
	return false
}

func waitForHolderSignal() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	<-signals
}
