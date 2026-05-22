package shim

import (
	"context"
	"errors"
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
	deleteErr  error
	deleteCall int
}

func (f *fakeSandboxLifecycle) SandboxID() string {
	return f.id
}

func (f *fakeSandboxLifecycle) GetState() cntr.StateString {
	return f.state
}

func (f *fakeSandboxLifecycle) Delete(ctx context.Context) error {
	f.deleteCall++
	return f.deleteErr
}

type fakeHypervisorControl struct {
	domainState string
	domainErr   error
}

func (f fakeHypervisorControl) Type() ports.HypervisorType {
	return ports.HypervisorXen
}

func (f fakeHypervisorControl) MaxCPUNum(ctx context.Context) uint32 {
	return 0
}

func (f fakeHypervisorControl) MemoryMB(context.Context) (uint32, uint32) {
	return 4096, 8192
}

func (f fakeHypervisorControl) DomainState(ctx context.Context, id string) (string, error) {
	return f.domainState, f.domainErr
}

func (f fakeHypervisorControl) SetVCPUCount(ctx context.Context, id string, count uint32) error {
	return nil
}

func (f fakeHypervisorControl) Pause(ctx context.Context, id string) error {
	return nil
}

func (f fakeHypervisorControl) Resume(ctx context.Context, id string) error {
	return nil
}

func (f fakeHypervisorControl) SetMemory(context.Context, string, uint32) error    { return nil }
func (f fakeHypervisorControl) SetMaxMemory(context.Context, string, uint32) error { return nil }
func (f fakeHypervisorControl) SetCPUWeight(context.Context, string, uint32) error { return nil }
func (f fakeHypervisorControl) SetCPUCapacity(context.Context, string, uint32) error {
	return nil
}

func TestReconcileSandboxRejectsRunningGuest(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, fakeHypervisorControl{domainState: "running"})

	require.Error(t, err)
	require.False(t, clearSandbox)
	require.Equal(t, 0, sandbox.deleteCall)
}

func TestReconcileSandboxIgnoresTypedNilSandbox(t *testing.T) {
	var sandbox *fakeSandboxLifecycle

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, fakeHypervisorControl{})

	require.NoError(t, err)
	require.False(t, clearSandbox)
}

func TestReconcileSandboxRejectsTypedNilHypervisor(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}
	var hypervisor *fakeHypervisorControl

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, hypervisor)

	require.Error(t, err)
	require.False(t, clearSandbox)
	require.Contains(t, err.Error(), "hypervisor control")
}

func TestReconcileSandboxCleansStaleRunningSandbox(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, fakeHypervisorControl{domainState: "shutdown"})

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxReturnsStaleDeleteError(t *testing.T) {
	expectedErr := errors.New("delete failed")
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateRunning, deleteErr: expectedErr}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, fakeHypervisorControl{domainState: "shutdown"})

	require.ErrorIs(t, err, expectedErr)
	require.False(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxCleansStoppedSandbox(t *testing.T) {
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateStopped}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, fakeHypervisorControl{})

	require.NoError(t, err)
	require.True(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}

func TestReconcileSandboxReturnsStoppedDeleteError(t *testing.T) {
	expectedErr := errors.New("delete failed")
	sandbox := &fakeSandboxLifecycle{id: "sandbox-1", state: cntr.StateStopped, deleteErr: expectedErr}

	clearSandbox, err := reconcileSandbox(context.Background(), sandbox, fakeHypervisorControl{})

	require.ErrorIs(t, err, expectedErr)
	require.False(t, clearSandbox)
	require.Equal(t, 1, sandbox.deleteCall)
}
