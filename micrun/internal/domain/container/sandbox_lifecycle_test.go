package container

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
)

type failingSaveStore struct {
	err error
}

func (s failingSaveStore) Load(ctx context.Context, namespace, taskID string) (*ports.RuntimeSnapshot, error) {
	return nil, os.ErrNotExist
}

func (s failingSaveStore) Save(ctx context.Context, snapshot *ports.RuntimeSnapshot) error {
	return s.err
}

func (s failingSaveStore) Delete(ctx context.Context, namespace, taskID string) error {
	return nil
}

type countingGuestControl struct {
	stops     map[string]int
	removeErr error
}

func (g *countingGuestControl) Start(ctx context.Context, id string) error  { return nil }
func (g *countingGuestControl) Pause(ctx context.Context, id string) error  { return nil }
func (g *countingGuestControl) Resume(ctx context.Context, id string) error { return nil }
func (g *countingGuestControl) Exists(ctx context.Context, id string) (bool, error) {
	return true, nil
}
func (g *countingGuestControl) Status(ctx context.Context, id string) (ports.GuestStatus, error) {
	return ports.GuestStatus{Running: true}, nil
}

func (g *countingGuestControl) Stop(ctx context.Context, id string) error {
	g.stops[id]++
	return nil
}

func (g *countingGuestControl) Remove(ctx context.Context, id string) error {
	return g.removeErr
}

type blockingGuestControl struct {
	countingGuestControl
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *blockingGuestControl) Stop(ctx context.Context, id string) error {
	g.stops[id]++
	g.once.Do(func() {
		close(g.entered)
		<-g.release
	})
	return nil
}

func TestNilSandboxLifecycleReturnsNotFound(t *testing.T) {
	var sandbox *Sandbox
	ctx := context.Background()

	if err := sandbox.Start(ctx); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("Start error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.Stop(ctx, false); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("Stop error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.Delete(ctx); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("Delete error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.removeNetwork(); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("removeNetwork error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.stopContainers(ctx, false); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("stopContainers error = %v, want SandboxNotFound", err)
	}
}

func TestLifecycleContainersReturnsSortedContainers(t *testing.T) {
	sandbox := &Sandbox{
		id: "sandbox-lifecycle",
		containers: map[string]*Container{
			"worker-b": {id: "worker-b"},
			"worker-a": {id: "worker-a"},
		},
	}

	containers, err := sandbox.lifecycleContainers()
	if err != nil {
		t.Fatalf("lifecycleContainers returned error: %v", err)
	}

	got := []string{containers[0].ID(), containers[1].ID()}
	want := []string{"worker-a", "worker-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lifecycleContainers IDs = %v, want %v", got, want)
	}
}

func TestLifecycleContainersRejectsNilEntries(t *testing.T) {
	sandbox := &Sandbox{
		id: "sandbox-lifecycle",
		containers: map[string]*Container{
			"bad": nil,
		},
	}

	_, err := sandbox.lifecycleContainers()
	if !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("lifecycleContainers error = %v, want ContainerNotFound", err)
	}
}

func TestRemoveNetworkRunsForStoppedSandbox(t *testing.T) {
	sandbox := &Sandbox{
		id: "sandbox-network",
		config: &SandboxConfig{
			NetworkConfig: NetworkConfig{
				NetworkID:      "ns",
				NetworkCreated: true,
			},
		},
		state: SandboxState{State: StateStopped},
	}

	if err := sandbox.removeNetwork(); err != nil {
		t.Fatalf("removeNetwork returned error: %v", err)
	}
	if sandbox.config.NetworkConfig.NetworkCreated {
		t.Fatal("removeNetwork did not clear NetworkCreated")
	}
	if sandbox.config.NetworkConfig.NetworkID != "" {
		t.Fatalf("NetworkID = %q, want empty", sandbox.config.NetworkConfig.NetworkID)
	}
	if !sandbox.networkCleaned {
		t.Fatal("removeNetwork did not mark networkCleaned")
	}
}

func TestStopReturnsSandboxStateStoreError(t *testing.T) {
	expectedErr := errors.New("save failed")
	sandbox := &Sandbox{
		id:         "sandbox-stop",
		config:     &SandboxConfig{ID: "sandbox-stop"},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
		stateRepo:  stateRepositoryFromStore(failingSaveStore{err: expectedErr}),
	}

	err := sandbox.Stop(context.Background(), false)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Stop error = %v, want %v", err, expectedErr)
	}
}

func TestStopStopsEachContainerOnce(t *testing.T) {
	store := newMemoryStateStore()
	guestControl := &countingGuestControl{stops: map[string]int{}}
	sandbox := &Sandbox{
		ctx:          context.Background(),
		id:           "sandbox-stop-once",
		config:       &SandboxConfig{ID: "sandbox-stop-once"},
		containers:   map[string]*Container{},
		state:        SandboxState{State: StateRunning},
		stateRepo:    stateRepositoryFromStore(store),
		deps:         testDepsWithStore(store),
		guestControl: guestControl,
	}
	sandbox.containers["container-stop-once"] = &Container{
		ctx:           context.Background(),
		id:            "container-stop-once",
		config:        &ContainerConfig{ID: "container-stop-once"},
		sandbox:       sandbox,
		state:         ContainerState{State: StateRunning},
		containerPath: "sandbox-stop-once/container-stop-once",
		guestExec:     recordingGuestExecutor{},
	}

	if err := sandbox.Stop(context.Background(), false); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if got := guestControl.stops["container-stop-once"]; got != 1 {
		t.Fatalf("guest Stop calls = %d, want 1", got)
	}
}

func TestConcurrentStopStopsContainerOnce(t *testing.T) {
	store := newMemoryStateStore()
	guestControl := &blockingGuestControl{
		countingGuestControl: countingGuestControl{stops: map[string]int{}},
		entered:              make(chan struct{}),
		release:              make(chan struct{}),
	}
	sandbox := &Sandbox{
		ctx:          context.Background(),
		id:           "sandbox-stop-concurrent",
		config:       &SandboxConfig{ID: "sandbox-stop-concurrent"},
		containers:   map[string]*Container{},
		state:        SandboxState{State: StateRunning},
		stateRepo:    stateRepositoryFromStore(store),
		deps:         testDepsWithStore(store),
		guestControl: guestControl,
	}
	sandbox.containers["container-stop-concurrent"] = &Container{
		ctx:           context.Background(),
		id:            "container-stop-concurrent",
		config:        &ContainerConfig{ID: "container-stop-concurrent"},
		sandbox:       sandbox,
		state:         ContainerState{State: StateRunning},
		containerPath: "sandbox-stop-concurrent/container-stop-concurrent",
		guestExec:     recordingGuestExecutor{},
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- sandbox.Stop(context.Background(), true)
	}()

	select {
	case <-guestControl.entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first stop")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- sandbox.Stop(context.Background(), true)
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second stop completed before first stop finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(guestControl.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Stop returned error: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Stop returned error: %v", err)
	}
	if got := guestControl.stops["container-stop-concurrent"]; got != 1 {
		t.Fatalf("guest Stop calls = %d, want 1", got)
	}
}

func TestDeleteReturnsContainerDeleteErrors(t *testing.T) {
	expectedErr := errors.New("remove failed")
	store := newMemoryStateStore()
	guestControl := &countingGuestControl{
		stops:     map[string]int{},
		removeErr: expectedErr,
	}
	sandbox := &Sandbox{
		ctx:          context.Background(),
		id:           "sandbox-delete",
		config:       &SandboxConfig{ID: "sandbox-delete"},
		containers:   map[string]*Container{},
		state:        SandboxState{State: StateStopped},
		stateRepo:    stateRepositoryFromStore(store),
		deps:         testDepsWithStore(store),
		guestControl: guestControl,
	}
	sandbox.containers["container-delete"] = &Container{
		ctx:           context.Background(),
		id:            "container-delete",
		config:        &ContainerConfig{ID: "container-delete"},
		sandbox:       sandbox,
		state:         ContainerState{State: StateReady},
		containerPath: "sandbox-delete/container-delete",
		guestExec:     recordingGuestExecutor{},
	}

	err := sandbox.Delete(context.Background())
	if err == nil {
		t.Fatal("Delete returned nil error")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Delete error = %v, want wrapped %v", err, expectedErr)
	}
}

func TestDeleteUsesOperationContextForSandboxStateCleanup(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	store := &recordingStateStore{}
	sandbox := &Sandbox{
		ctx:        context.Background(),
		id:         "sandbox-delete-context",
		config:     &SandboxConfig{ID: "sandbox-delete-context"},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateStopped},
		stateRepo:  stateRepositoryFromStore(store),
	}

	if err := sandbox.Delete(ctx); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if len(store.deleteCtxs) != 1 {
		t.Fatalf("Delete calls = %d, want 1", len(store.deleteCtxs))
	}
	if store.deleteCtxs[0] != ctx {
		t.Fatal("sandbox state cleanup did not receive operation context")
	}
}
