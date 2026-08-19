package container

import (
	"context"
	"fmt"
	"micrun/internal/ports"
	"sync"
	"sync/atomic"
)

const (
	okCode = 0
)

var (
	_ SandboxTraits   = (*Sandbox)(nil)
	_ ContainerTraits = (*Container)(nil)
)

type SandboxStorage struct {
	ID      string        `json:"id"`
	State   SandboxState  `json:"state"`
	Config  SandboxConfig `json:"config"`
	Network NetworkConfig `json:"network"`

	// Containers embeds each container's runtime state so ONE atomic write of
	// the sandbox document captures the whole pod. Container configs already
	// live in Config.ContainerConfigs; this map carries only the fields that
	// the legacy per-container file held exclusively. nil means the document
	// was written by a pre-combined-format shim: readers fall back to the
	// per-container files, and the first StoreSandbox after recovery migrates
	// the document to the combined format. Deliberately NOT omitempty: an
	// empty-but-present map is the format discriminator for a combined-format
	// sandbox with zero containers.
	Containers map[string]ContainerRuntimeRecord `json:"containers"`

	CreatedAt int64 `json:"created_at,omitempty"`
	ShimPID   int   `json:"shim_pid,omitempty"`
}

// ContainerRuntimeRecord is the per-container runtime state persisted inside
// the sandbox document. Keeping it in the same document as the sandbox state
// makes every container state transition a single atomic file write — the
// legacy two-file layout dual-wrote container then sandbox and could diverge
// when a crash landed between the writes.
type ContainerRuntimeRecord struct {
	State         ContainerState `json:"state"`
	Mounts        []Mount        `json:"mounts,omitempty"`
	ContainerPath string         `json:"container_path,omitempty"`
}

type Sandbox struct {
	ctx        context.Context
	resManager sandboxResource
	stateRepo  stateRepository
	deps       *Dependencies
	config     *SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	guestControl      ports.GuestControl
	hypervisorControl ports.HypervisorControl

	networkCleaned bool
	// storageRemoved marks that the sandbox's persisted state was deleted
	// (cleanSandboxStorage). Late persist calls from in-flight exit watchers
	// or concurrent state checks must not resurrect the state files: a
	// recreated sandbox runtime.json would be loaded by a same-id Create and
	// its stale config would clobber the fresh one.
	storageRemoved atomic.Bool

	// restoredContainers holds the per-container runtime records loaded from
	// the combined sandbox document, consumed one-shot by each container's
	// restoreState during rebuild. One-shot: a later same-id re-create after
	// Delete must start fresh, not resurrect the pre-restart state.
	// Guarded by containersLock.
	restoredContainers map[string]ContainerRuntimeRecord

	lifecycleLock  sync.Mutex
	annotLock      sync.RWMutex
	containersLock sync.RWMutex // protects containers map and ContainerConfigs
	stateMu        sync.RWMutex // protects state.State field
	resMu          sync.Mutex   // protects resManager (VCPUCount, MemoryPoolBytes, ContainerCPUSet, ContainerVCPUs)
	// persistMu serializes snapshot+write of the sandbox document so a
	// stale snapshot taken before a structural change (e.g. container
	// removal in DeleteContainer) cannot overwrite a newer write from the
	// change itself. Without this, an in-flight StoreSandbox from a
	// concurrent State probe can resurrect a deleted container.
	persistMu sync.Mutex
}

func (s *Sandbox) stateRepositoryChecked() (stateRepository, error) {
	if s != nil && s.stateRepo.store != nil {
		return s.stateRepo, nil
	}
	if s != nil && s.config != nil && s.config.StateStore != nil {
		return stateRepositoryFromStore(s.config.StateStore), nil
	}
	if s != nil && s.deps != nil {
		return stateRepositoryFromDependenciesChecked(s.deps)
	}
	return stateRepository{}, fmt.Errorf("container: no state repository available; dependencies not configured")
}

func (s *Sandbox) dependenciesChecked() (*Dependencies, error) {
	if s == nil || s.deps == nil {
		return nil, fmt.Errorf("container: dependencies not configured; pass Dependencies via SandboxConfig")
	}
	return s.deps, nil
}
