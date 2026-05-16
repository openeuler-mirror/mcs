package container

import (
	"errors"
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
