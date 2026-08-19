package container

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	er "micrun/internal/support/errors"
)

func TestContainerConfigEntriesValidatesSandbox(t *testing.T) {
	var sandbox *Sandbox
	if _, err := sandbox.containerConfigEntries(); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox containerConfigEntries error = %v, want SandboxNotFound", err)
	}

	if _, err := (&Sandbox{}).containerConfigEntries(); err == nil || !strings.Contains(err.Error(), "sandbox config") {
		t.Fatalf("missing config containerConfigEntries error = %v, want sandbox config error", err)
	}
}

func TestContainerConfigEntriesRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name    string
		configs map[string]*ContainerConfig
		wantErr error
		want    string
	}{
		{
			name: "nil config",
			configs: map[string]*ContainerConfig{
				"bad": nil,
			},
			want: "bad",
		},
		{
			name: "empty id",
			configs: map[string]*ContainerConfig{
				"bad": {},
			},
			wantErr: er.EmptyContainerID,
			want:    "bad",
		},
		{
			name: "duplicate id",
			configs: map[string]*ContainerConfig{
				"first":  {ID: "worker"},
				"second": {ID: "worker"},
			},
			wantErr: er.DuplicatedKey,
			want:    "worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox := &Sandbox{
				id: "sandbox-bootstrap",
				config: &SandboxConfig{
					ContainerConfigs: tt.configs,
				},
			}
			_, err := sandbox.containerConfigEntries()
			if err == nil {
				t.Fatal("containerConfigEntries returned nil error, want failure")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("containerConfigEntries error = %v, want %v", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("containerConfigEntries error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestContainerConfigEntriesReturnsSortedConfigs(t *testing.T) {
	sandbox := &Sandbox{
		id: "sandbox-bootstrap",
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{
				"z-key": {ID: "worker-b"},
				"a-key": {ID: "worker-a"},
			},
		},
	}

	entries, err := sandbox.containerConfigEntries()
	if err != nil {
		t.Fatalf("containerConfigEntries returned error: %v", err)
	}

	got := []string{entries[0].config.ID, entries[1].config.ID}
	want := []string{"worker-a", "worker-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containerConfigEntries IDs = %v, want %v", got, want)
	}
}

func TestInitContainersCreateFailureStopsOrphanGuest(t *testing.T) {
	firmware := t.TempDir() + "/firmware.elf"
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	// Same injection as the CreateContainer path: the second save (c.create's
	// final setContainerState — each state transition is now a single
	// combined-document write) fails after registerClient already created
	// the guest domain, so initContainers must tear it down — Sandbox.Delete
	// only iterates the containers map and would never see it.
	saveErr := errors.New("save failed")
	store := &failNthSaveStore{memoryStateStore: newMemoryStateStore(), failOn: 2, err: saveErr}
	deps := testDepsWithStore(store)
	guestCtl := &stopCountingGuestControl{}
	deps.CreateGuest = func(context.Context, GuestClientConfig) error {
		guestCtl.exists = true
		return nil
	}

	sandbox := &Sandbox{
		id:           "sandbox-bootstrap-cleanup",
		ctx:          context.Background(),
		stateRepo:    stateRepositoryFromStore(store),
		deps:         deps,
		containers:   map[string]*Container{},
		guestControl: guestCtl,
		config: &SandboxConfig{
			ID: "sandbox-bootstrap-cleanup",
			ContainerConfigs: map[string]*ContainerConfig{
				"c1": {ID: "c1", OS: "uniproton", ImageAbsPath: firmware, PedestalType: PedestalBaremetal},
			},
		},
	}

	err := sandbox.initContainers(context.Background())
	if !errors.Is(err, saveErr) {
		t.Fatalf("initContainers error = %v, want save error", err)
	}
	if guestCtl.stopCalls == 0 {
		t.Fatal("guest domain was not stopped after create failure (orphan domain)")
	}
	if _, ok := sandbox.config.ContainerConfigs["c1"]; ok {
		t.Fatal("ContainerConfigs entry was not removed")
	}
	if len(store.deleted) == 0 {
		t.Fatal("container state file was not deleted")
	}
}
