package container

import (
	"context"
	"errors"
	"testing"
)

func TestValidateSnapshotKeyAcceptsNestedKeys(t *testing.T) {
	if err := validateSnapshotKey(runtimeStateNamespaceContainer, "sandbox1/container1"); err != nil {
		t.Fatalf("validateSnapshotKey returned error: %v", err)
	}
}

func TestValidateSnapshotKeyRejectsInvalidKeys(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		taskID    string
	}{
		{name: "empty namespace", namespace: "", taskID: "task"},
		{name: "blank task", namespace: "namespace", taskID: " \t "},
		{name: "absolute namespace", namespace: "/namespace", taskID: "task"},
		{name: "parent traversal task", namespace: "namespace", taskID: "../task"},
		{name: "embedded parent traversal task", namespace: "namespace", taskID: "sandbox/../task"},
		{name: "empty namespace segment", namespace: "runtime//container", taskID: "task"},
		{name: "empty task segment", namespace: "namespace", taskID: "sandbox//task"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateSnapshotKey(tt.namespace, tt.taskID); err == nil {
				t.Fatalf("validateSnapshotKey(%q, %q) expected error", tt.namespace, tt.taskID)
			}
		})
	}
}

func TestSaveStateSnapshotNormalizesKeysBeforeStore(t *testing.T) {
	store := &recordingStateStore{}

	if err := saveStateSnapshot(context.Background(), store, `runtime\container`, `sandbox1\container1`, struct{}{}); err != nil {
		t.Fatalf("saveStateSnapshot returned error: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved snapshots = %d, want 1", len(store.saved))
	}
	if got := store.saved[0].Namespace; got != runtimeStateNamespaceContainer {
		t.Fatalf("snapshot namespace = %q, want %q", got, runtimeStateNamespaceContainer)
	}
	if got := store.saved[0].TaskID; got != "sandbox1/container1" {
		t.Fatalf("snapshot task id = %q, want sandbox1/container1", got)
	}
}

func TestStateSnapshotHelpersHonorCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &recordingStateStore{}

	err := saveStateSnapshot(ctx, store, runtimeStateNamespaceContainer, "container1", struct{}{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("saveStateSnapshot error = %v, want context.Canceled", err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("saved snapshots = %d, want 0", len(store.saved))
	}

	_, err = loadStateSnapshot[struct{}](ctx, store, runtimeStateNamespaceContainer, "container1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loadStateSnapshot error = %v, want context.Canceled", err)
	}
}

func TestStateSnapshotHelpersRejectTypedNilStore(t *testing.T) {
	var store *memoryStateStore

	err := saveStateSnapshot(context.Background(), store, runtimeStateNamespaceContainer, "container1", struct{}{})
	if err == nil || err.Error() != "state store is nil" {
		t.Fatalf("saveStateSnapshot typed nil error = %v, want state store is nil", err)
	}

	_, err = loadStateSnapshot[struct{}](context.Background(), store, runtimeStateNamespaceContainer, "container1")
	if err == nil || err.Error() != "state store is nil" {
		t.Fatalf("loadStateSnapshot typed nil error = %v, want state store is nil", err)
	}
}
