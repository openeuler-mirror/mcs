package container

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/statekey"
	"micrun/internal/support/validation"
)

func saveStateSnapshot[T any](ctx context.Context, store ports.StateStore, namespace, taskID string, value T) error {
	if validation.IsNil(store) {
		return fmt.Errorf("state store is nil")
	}
	namespace, taskID, err := normalizeSnapshotKey(namespace, taskID)
	if err != nil {
		return err
	}
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal snapshot %s/%s: %w", namespace, taskID, err)
	}

	return store.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: namespace,
		TaskID:    taskID,
		Data:      data,
	})
}

func loadStateSnapshot[T any](ctx context.Context, store ports.StateStore, namespace, taskID string) (*T, error) {
	if validation.IsNil(store) {
		return nil, fmt.Errorf("state store is nil")
	}
	namespace, taskID, err := normalizeSnapshotKey(namespace, taskID)
	if err != nil {
		return nil, err
	}
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	snapshot, err := store.Load(ctx, namespace, taskID)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(snapshot.Data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot %s/%s: %w", namespace, taskID, err)
	}
	return &out, nil
}

func validateSnapshotKey(namespace, taskID string) error {
	_, _, err := normalizeSnapshotKey(namespace, taskID)
	return err
}

func normalizeSnapshotKey(namespace, taskID string) (string, string, error) {
	normalizedNamespace, err := normalizeSnapshotKeyPart("namespace", namespace)
	if err != nil {
		return "", "", err
	}
	normalizedTaskID, err := normalizeSnapshotKeyPart("task id", taskID)
	if err != nil {
		return "", "", err
	}
	return normalizedNamespace, normalizedTaskID, nil
}

func normalizeSnapshotKeyPart(label, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("snapshot %s is empty", label)
	}
	normalized, err := statekey.Normalize(value)
	if err != nil {
		return "", fmt.Errorf("snapshot %s is invalid: %w", label, err)
	}
	return normalized, nil
}
