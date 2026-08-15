package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"micrun/internal/adapters/hypervisor/pedestal"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
	"micrun/internal/support/netns"

	"golang.org/x/sys/unix"
)

func loadSandbox(ctx context.Context, id string, guestCtl ports.GuestControl, deps *Dependencies) (sandbox *Sandbox, err error) {
	if deps == nil {
		return nil, fmt.Errorf("loadSandbox requires non-nil dependencies")
	}
	if guestCtl == nil {
		return nil, fmt.Errorf("loadSandbox requires non-nil guest control")
	}
	if id == "" {
		return nil, er.EmptySandboxID
	}

	repo, err := stateRepositoryFromDependenciesChecked(deps)
	if err != nil {
		return nil, err
	}
	ss, err := repo.LoadSandbox(ctx, id)
	if err != nil {
		log.Debugf("failed to restore sandbox from disk: %v.", err)
		return nil, err
	}

	var hyp ports.HypervisorControl
	if deps.DefaultHypervisorControl != nil {
		hyp = deps.DefaultHypervisorControl()
	}
	if err := validateOrCleanup(ctx, id, ss, guestCtl, hyp, repo); err != nil {
		return nil, err
	}

	return rebuildSandbox(ctx, ss, guestCtl, deps, repo)
}

func validateOrCleanup(ctx context.Context, id string, ss *SandboxStorage, guestCtl ports.GuestControl, hyp ports.HypervisorControl, repo stateRepository) error {
	result, err := validateSandboxState(ctx, id, ss, guestCtl, hyp, repo)
	if err != nil {
		return fmt.Errorf("sandbox state validation failed: %w", err)
	}
	if result.Cleanup {
		log.Infof("Cleaning up stale sandbox state for %s", id)
		// The state file is about to be deleted; terminate the recorded netns
		// holder first or the holder process and its anonymous netns leak
		// (nothing else remembers the pid once the state is gone).
		releasePersistedNetnsHolder(id, &ss.Network)
		if removeErr := repo.DeleteSandbox(ctx, id); removeErr != nil {
			log.Errorf("failed to remove persisted sandbox state for %s: %v", id, removeErr)
		}
		return er.SandboxNotFound
	}
	if !result.Valid {
		return fmt.Errorf("sandbox state is not valid for restoration")
	}
	if result.Corrected {
		log.Infof("Sandbox %s: persisting corrected state to disk", id)
		if storeErr := repo.SaveSandboxStorage(ctx, ss); storeErr != nil {
			return fmt.Errorf("failed to persist corrected sandbox state for %s: %w", id, storeErr)
		}
	}
	return nil
}

func rebuildSandbox(ctx context.Context, ss *SandboxStorage, guestCtl ports.GuestControl, deps *Dependencies, repo stateRepository) (*Sandbox, error) {
	c := ss.Config
	c.StateStore = repo.store
	c.Dependencies = deps
	if c.GuestControl == nil {
		c.GuestControl = guestCtl
	}
	if c.HypervisorControl == nil {
		c.HypervisorControl = deps.DefaultHypervisorControl()
	}

	sandbox, err := createSandbox(ctx, &c)
	if err != nil {
		log.Errorf("failed to create sandbox: %v.", err)
		return nil, err
	}
	if err := sandbox.loadContainersToSandbox(ctx); err != nil {
		return nil, err
	}
	reclaimPersistedNetnsHolder(ctx, sandbox)
	// Refresh the persisted ShimPID to the current process: the loaded state
	// file's shim_pid is the previous (now-dead) shim's PID. Without this,
	// checkShimCollision treats a PID recycled to another live micrun-shim
	// instance (same binary on a busy node) as a conflicting owner and blocks
	// recovery of this sandbox until that unrelated shim exits. Best-effort:
	// a persistence failure must not abort recovery (the in-memory sandbox is
	// fully usable); the PID is refreshed on the next Start/Stop otherwise.
	refreshCtx := context.WithoutCancel(ctx)
	if err := sandbox.StoreSandbox(refreshCtx); err != nil {
		log.Warnf("Sandbox %s: failed to refresh shim_pid during recovery: %v", sandbox.id, err)
	}
	return sandbox, nil
}

// reclaimPersistedNetnsHolder re-registers the netns holder recorded before a
// shim restart. The holder survives the shim by design (setsid, no Pdeathsig),
// but the in-memory holder registry starts empty after a restart, so a later
// Stop/Delete would find no registry entry and skip termination — leaking the
// holder process and its anonymous netns for the lifetime of the host. If the
// holder is gone (or its pid was recycled), clear the network record instead:
// the anonymous netns died with the holder, and cleanup must not terminate a
// reused pid.
func reclaimPersistedNetnsHolder(ctx context.Context, s *Sandbox) {
	netcfg, ok := s.network.(*NetworkConfig)
	if !ok || netcfg == nil || netcfg.HolderPid <= 0 {
		return
	}
	path, err := netns.RegisterExisting(s.id, netcfg.HolderPid)
	if err == nil {
		// Keep NetworkID aligned with the reclaimed holder path so Create
		// retries refresh annotations to the live ns, not a discarded
		// ephemeral holder's path.
		lockutil.WithLock(&s.containersLock, func() {
			netcfg.NetworkID = path
			netcfg.NetworkCreated = true
		})
		return
	}
	log.Warnf("Sandbox %s: cannot reclaim netns holder pid %d (%v); clearing network record", s.id, netcfg.HolderPid, err)
	// NetworkCleanup mutates the same fields under containersLock; mirror it.
	lockutil.WithLock(&s.containersLock, func() {
		netcfg.NetworkID = ""
		netcfg.NetworkCreated = false
		netcfg.HolderPid = 0
	})
	if err := s.StoreSandbox(ctx); err != nil {
		log.Warnf("Sandbox %s: failed to persist cleared network record: %v", s.id, err)
	}
}

// releasePersistedNetnsHolder best-effort terminates the netns holder of a
// sandbox whose persisted state is being discarded as stale. If the holder is
// dead or its pid was recycled, RegisterExisting refuses and there is nothing
// safe to terminate.
func releasePersistedNetnsHolder(id string, network *NetworkConfig) {
	if network == nil || network.HolderPid <= 0 {
		return
	}
	if _, err := netns.RegisterExisting(id, network.HolderPid); err != nil {
		return
	}
	if err := netns.Cleanup(id, 0); err != nil {
		log.Warnf("failed to terminate stale netns holder for sandbox %s: %v", id, err)
	}
}

func LoadSandboxWithDependencies(ctx context.Context, id string, guestCtl ports.GuestControl, deps *Dependencies) (sandbox *Sandbox, err error) {
	return loadSandbox(ctx, id, guestCtl, deps)
}

// isLiveShimInstance reports whether pid refers to a live process running the
// same shim binary as the current process. kill(pid, 0) alone cannot tell a
// live shim apart from a PID recycled to an unrelated process after a shim
// crash, so identity is verified via /proc/<pid>/exe instead.
func isLiveShimInstance(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := unix.Kill(pid, 0); err != nil {
		// ESRCH: the process is gone. EPERM: owned by another uid — containerd
		// shims always run with the same uid as the current process, so a
		// foreign-uid process cannot be a shim instance.
		return false
	}
	self, err := os.Executable()
	if err != nil {
		// Without our own identity there is no way to rule out a live shim;
		// keep the historical fail-closed behavior.
		return true
	}
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		// The process exited between the signal check and the readlink, or is
		// a zombie (zombies have no exe link): not a running shim.
		return false
	}
	// A shim started before a package upgrade runs a deleted binary; /proc
	// reports the original path with a " (deleted)" suffix.
	target = strings.TrimSuffix(target, " (deleted)")
	return target == self
}

type sandboxStateValidation struct {
	Valid     bool
	Cleanup   bool
	Corrected bool // state was corrected in-memory and needs to be persisted
}

func validateSandboxState(ctx context.Context, id string, storage *SandboxStorage, guestCtl ports.GuestControl, hyp ports.HypervisorControl, repo stateRepository) (sandboxStateValidation, error) {
	if storage == nil {
		return sandboxStateValidation{}, fmt.Errorf("nil storage")
	}
	if guestCtl == nil {
		return sandboxStateValidation{}, fmt.Errorf("guest control is required")
	}
	if !storage.State.Valid() {
		return sandboxStateValidation{}, fmt.Errorf("sandbox state invalid: %s", storage.State.State)
	}

	if err := checkShimCollision(id, storage.ShimPID); err != nil {
		return sandboxStateValidation{}, err
	}

	stale, err := isRTOSClientStale(ctx, id, storage, guestCtl, hyp, repo)
	if err != nil {
		return sandboxStateValidation{}, err
	}
	if stale {
		return sandboxStateValidation{Cleanup: true}, nil
	}

	corrected := compareStoredAndLiveState(ctx, id, storage, guestCtl, repo)
	return sandboxStateValidation{Valid: true, Corrected: corrected}, nil
}

func checkShimCollision(id string, shimPID int) error {
	if shimPID <= 0 {
		return nil
	}
	// A bare processExists check wedges on PID reuse: when the crashed shim's
	// PID is recycled by an unrelated long-lived process, the collision error
	// below is not retryable — recovery refuses to restore the sandbox and the
	// one-shot cleanup path fails too, until the unrelated process exits.
	// Verify the process is actually a shim binary before declaring collision.
	if isLiveShimInstance(shimPID) {
		log.Debugf("Sandbox %s: shim PID %d still running, another instance may be active", id, shimPID)
		return fmt.Errorf("another shim instance (PID %d) is already running", shimPID)
	}
	log.Infof("Sandbox %s: shim PID %d is dead or recycled, validating RTOS state", id, shimPID)
	return nil
}

func isRTOSClientStale(ctx context.Context, id string, storage *SandboxStorage, guestCtl ports.GuestControl, hyp ports.HypervisorControl, repo stateRepository) (bool, error) {
	// A fully stopped sandbox has no live mica clients by design: mica stop
	// removes the client (StopContext issues MRemove). Absence from micad is
	// expected, not orphanage — keep the state so the stopped tasks still
	// converge (State/Wait/Delete) after a shim restart.
	if storage.State.State == StateStopped {
		return false, nil
	}
	// CRI PodSandbox IDs are the infra container id and never register a mica
	// domain. Probe real RTOS container ids from the snapshot first; fall
	// back to the sandbox id only when no non-infra containers exist
	// (standalone / non-CRI workloads where sandbox id == client id).
	ids, fallback := rtosClientIDsForStaleCheck(id, storage)
	if !fallback {
		// Containers already stopped/down were removed from micad by design;
		// their absence must not mark the sandbox stale. Probe only the
		// containers that should still have a live client.
		ids = liveExpectedClientIDs(ctx, storage, id, ids, repo)
		if len(ids) == 0 {
			return false, nil
		}
	}
	anyExists := false
	for _, clientID := range ids {
		exists, err := guestCtl.Exists(ctx, clientID)
		if err != nil {
			return false, err
		}
		if exists {
			anyExists = true
			break
		}
	}
	if !anyExists {
		if fallback && sandboxHasInfraContainer(storage) {
			// The only probe id was the sandbox id itself because every
			// ContainerConfig is infra (CRI InfraOnly pod). The sandbox id IS
			// the infra container id, which never registers a mica domain —
			// its absence from micad is expected by design, not evidence of a
			// stale/orphaned sandbox. Marking it stale here would delete the
			// persisted state on every shim restart of an InfraOnly pod,
			// making the sandbox unrecoverable. Keep the state and let
			// reconcileSandbox decide on a duplicate Create.
			log.Debugf("Sandbox %s: infra-only (no non-infra containers), sandbox id absent from micad is expected; not stale", id)
			return false, nil
		}
		// Exists is socket-only: a micad restart wipes /run/mica while the
		// Xen domains stay up. Deleting the state here would orphan those
		// domains (nothing left knows their ids) and drop the tasks from
		// recovery. If any probed domain is still alive at the hypervisor,
		// keep the state: the normal paths converge it (checkState marks the
		// container Down; the next Start's Remove-before-register destroys
		// the lingering domain and re-creates the guest).
		alive, probeErr := anyDomainAlive(ctx, hyp, ids)
		if probeErr != nil {
			// The hypervisor could not answer (xl timeout, xenstore stall,
			// parse failure). Treating that as "no domain" would delete the
			// persisted state while the domains may still be running — the
			// exact orphaning this probe exists to prevent. Fail loudly:
			// recovery aborts and containerd retries the shim.
			return false, fmt.Errorf("sandbox %s: cannot determine domain liveness (probed %v): %w", id, ids, probeErr)
		}
		if alive {
			log.Infof("Sandbox %s: micad sockets absent but a domain is still alive (probed %v); keeping state for recovery", id, ids)
			return false, nil
		}
		log.Infof("Sandbox %s: RTOS client not found in micad (probed %v), persisted state is stale", id, ids)
		return true, nil
	}
	return false, nil
}

// anyDomainAlive reports whether any of the probed ids still has a live
// hypervisor domain. A definitive DomainState answer (success or an explicit
// not-found) settles the question; ErrNotSupported (non-Xen pedestal, no
// domain concept) likewise counts as "not alive". Any other error means the
// hypervisor could not be consulted reliably and is surfaced to the caller
// instead of being silently mapped to "not alive" (which would orphan live
// domains).
func anyDomainAlive(ctx context.Context, hyp ports.HypervisorControl, ids []string) (bool, error) {
	if hyp == nil {
		return false, nil
	}
	for _, clientID := range ids {
		_, err := hyp.DomainState(ctx, clientID)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, pedestal.ErrNotSupported) || isMissingDomainError(err) {
			continue
		}
		return false, fmt.Errorf("domain state probe for %s: %w", clientID, err)
	}
	return false, nil
}

// isMissingDomainError matches the explicit not-found wording produced by the
// hypervisor adapters (same semantics as the classifier in the micad control
// adapter; duplicated here to keep the container domain free of an adapter
// import cycle).
func isMissingDomainError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
}

// sandboxHasInfraContainer reports whether the sandbox storage has at least one
// infra container config. Used to distinguish a CRI InfraOnly pod (sandbox id is
// the infra id, never in micad) from a standalone sandbox with no containers
// (sandbox id IS the client id).
func sandboxHasInfraContainer(storage *SandboxStorage) bool {
	if storage == nil {
		return false
	}
	for _, cc := range storage.Config.ContainerConfigs {
		if cc != nil && cc.IsInfra {
			return true
		}
	}
	return false
}

// liveExpectedClientIDs drops ids whose persisted container state is already
// terminal (Stopped/Down): a stopped container's mica client was removed by
// design, so its absence from micad is expected and proves nothing about
// orphanage. Ids whose state cannot be determined are kept in the probe set —
// only positive terminal evidence may excuse an absence.
func liveExpectedClientIDs(ctx context.Context, storage *SandboxStorage, sandboxID string, ids []string, repo stateRepository) []string {
	live := make([]string, 0, len(ids))
	for _, containerID := range ids {
		state, ok := persistedContainerState(ctx, storage, sandboxID, containerID, repo)
		if !ok {
			log.Debugf("stale check: cannot determine container %s persisted state, keeping probe", containerID)
			live = append(live, containerID)
			continue
		}
		if state == StateStopped || state == StateDown {
			continue
		}
		live = append(live, containerID)
	}
	return live
}

// persistedContainerState reads a container's persisted state, preferring the
// combined sandbox document (single-write format); the per-container file is
// the pre-combined-format fallback.
func persistedContainerState(ctx context.Context, storage *SandboxStorage, sandboxID, containerID string, repo stateRepository) (StateString, bool) {
	if storage != nil && storage.Containers != nil {
		record, ok := storage.Containers[containerID]
		if !ok {
			return "", false
		}
		return record.State.State, true
	}
	cs, err := repo.LoadContainer(ctx, containerID, filepath.Join(sandboxID, containerID))
	if err != nil {
		return "", false
	}
	return cs.State.State, true
}

// rtosClientIDsForStaleCheck returns guest-domain ids that should be probed
// when deciding whether persisted sandbox state is orphaned. The second
// return value reports whether the standalone fallback (sandbox id itself)
// was used because no non-infra containers exist.
func rtosClientIDsForStaleCheck(sandboxID string, storage *SandboxStorage) (ids []string, fallback bool) {
	if storage != nil {
		ids = make([]string, 0, len(storage.Config.ContainerConfigs))
		for _, cc := range storage.Config.ContainerConfigs {
			if cc == nil || cc.IsInfra || cc.ID == "" {
				continue
			}
			ids = append(ids, cc.ID)
		}
		if len(ids) > 0 {
			return ids, false
		}
	}
	return []string{sandboxID}, true
}

func compareStoredAndLiveState(ctx context.Context, id string, storage *SandboxStorage, guestCtl ports.GuestControl, repo stateRepository) bool {
	// Probe ALL non-infra RTOS clients and decide based on the aggregate.
	// Using only the first successful probe is wrong for multi-container
	// sandboxes: if container A is stopped but container B is still running,
	// the sandbox is alive. Breaking on the first client (whose map-iteration
	// order is random) would mark a live sandbox Stopped, making the
	// still-running container unmanageable and causing its destruction on the
	// next Create (reconcileSandbox deletes stopped sandboxes).
	probeIDs, _ := rtosClientIDsForStaleCheck(id, storage)
	pausedIDs := pausedPersistedClientIDs(ctx, storage, id, probeIDs, repo)
	var (
		anyAlive bool
		probed   []string
	)
	for _, clientID := range probeIDs {
		status, err := guestCtl.Status(ctx, clientID)
		if err != nil {
			continue
		}
		probed = append(probed, clientID)
		// A client that is not stopped (running or paused/suspended) keeps
		// the sandbox alive.
		if !status.Stopped {
			anyAlive = true
			continue
		}
		// On non-Xen pedestals Pause is a full mica stop (MPause->MStop), so
		// a paused container's client reports Stopped while still healthy.
		// Mirror sandboxGuestLive: count persisted Paused as alive so recovery
		// does not "correct" sandbox Running→Stopped and break Resume.
		if pausedIDs[clientID] {
			anyAlive = true
		}
	}
	if len(probed) == 0 {
		log.Warnf("Sandbox %s: failed to query RTOS status for any client (probed %v)", id, probeIDs)
		return false
	}
	storageState := storage.State.State
	if storageState == StateRunning && !anyAlive {
		log.Warnf("Sandbox %s: state mismatch (clients %v), file=running, all clients stopped; correcting persisted state to stopped", id, probed)
		storage.State.State = StateStopped
		return true
	}
	if storageState == StateStopped && anyAlive {
		log.Warnf("Sandbox %s: state mismatch (clients %v), file=stopped, a client is alive; correcting persisted state to running", id, probed)
		storage.State.State = StateRunning
		return true
	}
	return false
}

// pausedPersistedClientIDs returns probe ids whose persisted container state
// is Paused. Used by compareStoredAndLiveState for non-Xen Pause==Stop.
func pausedPersistedClientIDs(ctx context.Context, storage *SandboxStorage, sandboxID string, ids []string, repo stateRepository) map[string]bool {
	paused := make(map[string]bool)
	if len(ids) == 0 {
		return paused
	}
	for _, containerID := range ids {
		state, ok := persistedContainerState(ctx, storage, sandboxID, containerID, repo)
		if ok && state == StatePaused {
			paused[containerID] = true
		}
	}
	return paused
}
