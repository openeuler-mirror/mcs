package shim

import (
	"context"
	"errors"
	"syscall"
	"testing"

	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"

	"github.com/stretchr/testify/require"
)

type fakeSandboxStorer struct {
	id        string
	storeErr  error
	storeCall int
}

func (f *fakeSandboxStorer) SandboxID() string {
	return f.id
}

func (f *fakeSandboxStorer) StoreSandbox(ctx context.Context) error {
	f.storeCall++
	return f.storeErr
}

func TestPersistCreatedSandboxStoresState(t *testing.T) {
	storer := &fakeSandboxStorer{id: "sandbox-1"}

	err := persistCreatedSandbox(context.Background(), storer)

	require.NoError(t, err)
	require.Equal(t, 1, storer.storeCall)
}

func TestPersistCreatedSandboxReturnsStoreError(t *testing.T) {
	storer := &fakeSandboxStorer{id: "sandbox-1", storeErr: errors.New("boom")}

	err := persistCreatedSandbox(context.Background(), storer)

	require.Error(t, err)
	require.Contains(t, err.Error(), "store sandbox state")
	require.Equal(t, 1, storer.storeCall)
}

func TestPersistCreatedSandboxRejectsTypedNilStorer(t *testing.T) {
	var storer *fakeSandboxStorer

	err := persistCreatedSandbox(context.Background(), storer)

	require.Error(t, err)
	require.Contains(t, err.Error(), "sandbox storer")
}

type fakeSandboxLifecycle struct {
	id         string
	state      cntr.StateString
	containers []cntr.ContainerTraits
	deleteErr  error
	deleteCall int
	stopCall   int
	stopCtxErr error
	delCtxErr  error
}

func (f *fakeSandboxLifecycle) SandboxID() string {
	return f.id
}

func (f *fakeSandboxLifecycle) GetState() cntr.StateString {
	return f.state
}

func (f *fakeSandboxLifecycle) GetAllContainers() []cntr.ContainerTraits {
	return f.containers
}

func (f *fakeSandboxLifecycle) Stop(ctx context.Context, force bool) error {
	f.stopCall++
	f.stopCtxErr = ctx.Err()
	return nil
}

func (f *fakeSandboxLifecycle) Delete(ctx context.Context) error {
	f.deleteCall++
	f.delCtxErr = ctx.Err()
	return f.deleteErr
}

type fakeContainerTraits struct {
	id      string
	isInfra bool
	status  cntr.StateString
}

func (f *fakeContainerTraits) ID() string                        { return f.id }
func (f *fakeContainerTraits) GetAnnotations() map[string]string { return nil }
func (f *fakeContainerTraits) GetPid() int                       { return 0 }
func (f *fakeContainerTraits) IsInfra() bool                     { return f.isInfra }
func (f *fakeContainerTraits) Sandbox() cntr.SandboxTraits       { return nil }
func (f *fakeContainerTraits) GetMemoryLimit() uint64            { return 0 }
func (f *fakeContainerTraits) Status() cntr.StateString          { return f.status }
func (f *fakeContainerTraits) State() *cntr.ContainerState       { return nil }
func (f *fakeContainerTraits) StateSnapshot() (cntr.ContainerState, error) {
	return cntr.ContainerState{}, nil
}
func (f *fakeContainerTraits) GetClientCPU() string                         { return "" }
func (f *fakeContainerTraits) SaveState() error                             { return nil }
func (f *fakeContainerTraits) Signal(context.Context, syscall.Signal) error { return nil }

type fakeReconcileGuestControl struct {
	exists    map[string]bool
	existsErr error
	status    map[string]ports.GuestStatus
	statusErr error
}

func (f *fakeReconcileGuestControl) Start(context.Context, string) error  { return nil }
func (f *fakeReconcileGuestControl) Stop(context.Context, string) error   { return nil }
func (f *fakeReconcileGuestControl) Remove(context.Context, string) error { return nil }
func (f *fakeReconcileGuestControl) Pause(context.Context, string) error  { return nil }
func (f *fakeReconcileGuestControl) Resume(context.Context, string) error { return nil }

func (f *fakeReconcileGuestControl) Exists(_ context.Context, id string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.exists[id], nil
}

func (f *fakeReconcileGuestControl) Status(_ context.Context, id string) (ports.GuestStatus, error) {
	if f.statusErr != nil {
		return ports.GuestStatus{}, f.statusErr
	}
	return f.status[id], nil
}

func TestReconcileSandboxRejectsRunningGuest(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}
	guestCtl := &fakeReconcileGuestControl{
		exists: map[string]bool{"sandbox-1": true},
		status: map[string]ports.GuestStatus{"sandbox-1": {State: "Running", Running: true}},
	}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.Error(t, err)
	require.False(t, clearSandbox)
	require.Equal(t, 0, sandbox.deleteCall)
}

func TestReconcileSandboxRejectsRunningPodSandbox(t *testing.T) {
	// A CRI pod sandbox has no Xen domain under its own id; liveness must be
	// derived from its workload containers, not from the sandbox id.
	sandbox := &fakeSandboxLifecycle{
		id:    "pod-sandbox",
		state: cntr.StateRunning,
		containers: []cntr.ContainerTraits{
			&fakeContainerTraits{id: "pod-sandbox", isInfra: true},
			&fakeContainerTraits{id: "workload-1"},
		},
	}
	guestCtl := &fakeReconcileGuestControl{
		exists: map[string]bool{"workload-1": true},
		status: map[string]ports.GuestStatus{"workload-1": {State: "Running", Running: true}},
	}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.Error(t, err)
	require.Contains(t, err.Error(), "guest is running")
	require.False(t, clearSandbox)
	require.Equal(t, 0, sandbox.deleteCall)
}

func TestReconcileSandboxPreservesInfraOnlyRunningSandbox(t *testing.T) {
	// A CRI InfraOnly pod (only the pause container) never registers a mica
	// domain. The infra container id IS the sandbox id and is absent from
	// micad by design. A duplicate Create must NOT destroy this sandbox.
	sandbox := &fakeSandboxLifecycle{
		id:    "pod-sandbox",
		state: cntr.StateRunning,
		containers: []cntr.ContainerTraits{
			&fakeContainerTraits{id: "pod-sandbox", isInfra: true},
		},
	}
	guestCtl := &fakeReconcileGuestControl{exists: map[string]bool{}}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.Error(t, err)
	require.Contains(t, err.Error(), "guest is running")
	require.False(t, clearSandbox)
	require.Equal(t, 0, sandbox.deleteCall, "must not destroy an infra-only pod sandbox")
}

func TestReconcileSandboxIgnoresTypedNilSandbox(t *testing.T) {
	var sandbox *fakeSandboxLifecycle

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, &fakeReconcileGuestControl{})

	require.NoError(t, err)
	require.False(t, clearSandbox)
}

func TestReconcileSandboxRejectsTypedNilGuestControl(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}
	var guestCtl *fakeReconcileGuestControl

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.Error(t, err)
	require.False(t, clearSandbox)
	require.Contains(t, err.Error(), "guest control")
}

func TestReconcileSandboxCleansStaleRunningSandbox(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}
	// No client exists in micad: the RUNNING record is stale.
	guestCtl := &fakeReconcileGuestControl{exists: map[string]bool{}}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxCleansStoppedClientSandbox(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}
	guestCtl := &fakeReconcileGuestControl{
		exists: map[string]bool{"sandbox-1": true},
		status: map[string]ports.GuestStatus{"sandbox-1": {State: "Stopped", Stopped: true}},
	}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxRefusesCleanupOnQueryError(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}
	guestCtl := &fakeReconcileGuestControl{existsErr: errors.New("micad unreachable")}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.Error(t, err)
	require.Contains(t, err.Error(), "liveness")
	require.False(t, clearSandbox)
	require.Equal(t, 0, sandbox.deleteCall, "must not destroy a sandbox whose liveness is unknown")
}

func TestReconcileSandboxReturnsStaleDeleteError(t *testing.T) {
	expectedErr := errors.New("delete failed")
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning, deleteErr: expectedErr}
	guestCtl := &fakeReconcileGuestControl{exists: map[string]bool{}}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.ErrorIs(t, err, expectedErr)
	require.False(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxCleansStoppedSandbox(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateStopped}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, &fakeReconcileGuestControl{})

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxReturnsStoppedDeleteError(t *testing.T) {
	expectedErr := errors.New("delete failed")
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateStopped, deleteErr: expectedErr}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, &fakeReconcileGuestControl{})

	require.ErrorIs(t, err, expectedErr)
	require.False(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxPreservesPausedSandbox(t *testing.T) {
	// Non-Xen pedestals implement Pause as a full mica stop (MPause->MStop),
	// so a paused workload's client reports Stopped in micad while being
	// perfectly healthy. A duplicate Create must NOT destroy a merely-paused
	// sandbox.
	sandbox := &fakeSandboxLifecycle{
		id:    "pod-sandbox",
		state: cntr.StateRunning,
		containers: []cntr.ContainerTraits{
			&fakeContainerTraits{id: "pod-sandbox", isInfra: true},
			&fakeContainerTraits{id: "workload-1", status: cntr.StatePaused},
		},
	}
	guestCtl := &fakeReconcileGuestControl{
		exists: map[string]bool{"workload-1": true},
		status: map[string]ports.GuestStatus{"workload-1": {State: "Stopped", Stopped: true}},
	}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.Error(t, err)
	require.Contains(t, err.Error(), "guest is running")
	require.False(t, clearSandbox)
	require.Equal(t, 0, sandbox.deleteCall, "must not destroy a merely-paused sandbox")
}

func TestReconcileSandboxCleansStoppedNonPausedSandbox(t *testing.T) {
	// A workload that is stopped without being Paused is really dead: the
	// stale RUNNING sandbox must still be cleaned up.
	sandbox := &fakeSandboxLifecycle{
		id:    "pod-sandbox",
		state: cntr.StateRunning,
		containers: []cntr.ContainerTraits{
			&fakeContainerTraits{id: "workload-1", status: cntr.StateStopped},
		},
	}
	guestCtl := &fakeReconcileGuestControl{
		exists: map[string]bool{"workload-1": true},
		status: map[string]ports.GuestStatus{"workload-1": {State: "Stopped", Stopped: true}},
	}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, guestCtl)

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxDetachesCtxForStaleRunningTeardown(t *testing.T) {
	// The Stop+Delete teardown of a stale RUNNING sandbox must run on a
	// context detached from the Create RPC: a kubelet retry that times out
	// mid-cleanup must not abort the guest-domain removal (which would orphan
	// the micad clients). Every other teardown path detaches for the same
	// reason.
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}
	guestCtl := &fakeReconcileGuestControl{exists: map[string]bool{}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	clearSandbox, err := reconcileSandbox(ctx, sandbox, guestCtl)

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.stopCall, "Stop must be called even under a canceled RPC ctx")
	require.Equal(t, 1, sandbox.deleteCall, "Delete must be called even under a canceled RPC ctx")
	require.NoError(t, sandbox.stopCtxErr, "Stop must receive a non-canceled context")
	require.NoError(t, sandbox.delCtxErr, "Delete must receive a non-canceled context")
}

func TestReconcileSandboxDetachesCtxForStoppedTeardown(t *testing.T) {
	// The Delete teardown of a STOPPED sandbox must run on a detached context
	// too: Delete iterates containers and issues guestControl.Remove per
	// container, which returns ctx.Err() immediately under a canceled ctx.
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateStopped}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	clearSandbox, err := reconcileSandbox(ctx, sandbox, &fakeReconcileGuestControl{})

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall, "Delete must be called even under a canceled RPC ctx")
	require.NoError(t, sandbox.delCtxErr, "Delete must receive a non-canceled context")
}
