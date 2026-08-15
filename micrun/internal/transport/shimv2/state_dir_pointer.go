package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
)

// state_dir pointer (scan item 2.5).
//
// state_dir can be configured per-workload through the CRI RuntimeClass
// ConfigPath, which only arrives with the first Create request. A restarted
// shim (crash/upgrade) has no request in hand when recovery runs, so the
// host-only resolve falls back to the default /run/micrun while the live
// guests' persisted state lives in the custom directory — recovery then
// treats the sandbox as first-start and the Xen domains are orphaned from
// containerd's point of view.
//
// To close that window, every successful custom state-dir binding records the
// active directory in a small pointer file under the DEFAULT state root:
//
//	<defs.MicrunStateDir>/state-dir    (single line, absolute path)
//
// The pointer lives on tmpfs like the default state itself, so it survives
// shim restarts within a boot (exactly when domains survive and recovery
// matters) and disappears on guest reboot together with the domains —
// where an empty state is the correct answer anyway. Host config keeps
// precedence: an explicitly configured host state_dir is never overridden.
//
// The pointer is a single node-level slot. A binding to the default dir must
// NOT clear it: on a mixed node (one custom-state_dir RuntimeClass plus
// default-state_dir workloads or ctr debug sessions) the default binding's
// Create runs in a different shim than the custom one, and removing the
// pointer here would make the custom workload's next shim restart fall back
// to the default root and lose its task + Xen domain — the exact failure the
// pointer exists to prevent. A stale pointer (custom workloads gone) is
// harmless: recovery finds no sandboxes there and readStateDirPointerAt
// ignores garbage. Known limitation: when custom and default workloads run
// side by side, a default workloads's restarted shim still adopts the custom
// pointer and misses its own state in the default root; fixing that needs
// per-sandbox state-dir ownership during recovery.

const stateDirPointerName = "state-dir"

func stateDirPointerPath() string {
	return filepath.Join(defs.MicrunStateDir, stateDirPointerName)
}

// syncStateDirPointerAt records the pointer for a custom state-dir binding.
// Best-effort: a pointer failure must never break Create or recovery — it
// only means the next shim restart falls back to the previous behavior.
// Bindings to the default dir are a no-op (see the comment above).
func syncStateDirPointerAt(pointerPath, stateDir string) {
	if stateDir == "" || stateDir == defs.MicrunStateDir {
		return
	}
	if err := os.MkdirAll(filepath.Dir(pointerPath), 0o755); err != nil {
		log.Warnf("[STATE-DIR] failed to create pointer dir %s: %v", filepath.Dir(pointerPath), err)
		return
	}
	content := strings.TrimSpace(stateDir) + "\n"
	if err := fs.WriteFileAtomic(pointerPath, []byte(content), 0o644); err != nil {
		log.Warnf("[STATE-DIR] failed to write pointer %s: %v", pointerPath, err)
	}
}

// readStateDirPointerAt returns the pointer target when present and valid.
// Any garbage, relative path, or the default dir itself is ignored.
func readStateDirPointerAt(pointerPath string) (string, bool) {
	raw, err := os.ReadFile(pointerPath)
	if err != nil {
		return "", false
	}
	target := strings.TrimSpace(string(raw))
	if target == "" || target == defs.MicrunStateDir {
		return "", false
	}
	clean, err := fs.CleanAbsolutePath(target)
	if err != nil {
		log.Warnf("[STATE-DIR] ignoring invalid state-dir pointer %s -> %q: %v", pointerPath, target, err)
		return "", false
	}
	return clean, true
}

// adoptStateDirPointerAt returns the directory recovery should bind: an
// explicitly non-default stateDir wins; otherwise a valid pointer recorded
// by a previous binding is adopted so a restarted shim recovers from where
// the state actually lives.
func adoptStateDirPointerAt(pointerPath, stateDir string) string {
	if stateDir != "" && stateDir != defs.MicrunStateDir {
		return stateDir
	}
	if target, ok := readStateDirPointerAt(pointerPath); ok {
		log.Infof("[STATE-DIR] adopting state dir %s from %s (host config left it default)", target, pointerPath)
		return target
	}
	return defs.MicrunStateDir
}

func syncStateDirPointer(stateDir string) {
	syncStateDirPointerAt(stateDirPointerPath(), stateDir)
}

func adoptStateDirPointer(stateDir string) string {
	return adoptStateDirPointerAt(stateDirPointerPath(), stateDir)
}

// formatStateDirForLog keeps log lines uniform.
func formatStateDirForLog(stateDir string) string {
	if stateDir == "" {
		return fmt.Sprintf("%s (default)", defs.MicrunStateDir)
	}
	return stateDir
}
