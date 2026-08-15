package container

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"
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
		// A snapshot that cannot be unmarshalled (truncated write left by a
		// pre-atomic-write shim, bit rot, manual edits) used to fail recovery
		// forever: every shim restart re-read the same bytes and died, so the
		// node stayed down until someone deleted the file by hand. Quarantine
		// it and report not-found instead — the stale-state machinery then
		// probes micad/Xen and decides whether anything must be cleaned up.
		type quarantiner interface {
			Quarantine(ctx context.Context, namespace, taskID string) error
		}
		if namespace == runtimeStateNamespaceSandbox {
			// Quarantining a SANDBOX document destroys the only record of
			// which guest ids existed: the stale-state probes cannot run
			// without it, so a still-running Xen domain would be orphaned
			// silently. Best-effort salvage the container ids for the log
			// so an operator can reconcile manually.
			var salvage struct {
				Config struct {
					ContainerConfigs map[string]struct {
						ID string `json:"ID"`
					} `json:"ContainerConfigs"`
				} `json:"Config"`
			}
			ids := []string{}
			if json.Unmarshal(snapshot.Data, &salvage) == nil {
				for _, cc := range salvage.Config.ContainerConfigs {
					if cc.ID != "" {
						ids = append(ids, cc.ID)
					}
				}
			}
			log.Warnf("corrupt sandbox snapshot %s/%s; salvaged container ids (may need manual xl destroy): %v (unmarshal: %v)", namespace, taskID, ids, err)
		}
		if q, ok := store.(quarantiner); ok {
			if qErr := q.Quarantine(ctx, namespace, taskID); qErr != nil {
				log.Warnf("failed to quarantine corrupt snapshot %s/%s: %v", namespace, taskID, qErr)
			} else {
				log.Warnf("quarantined corrupt snapshot %s/%s; treating as absent", namespace, taskID)
			}
		} else {
			log.Warnf("corrupt snapshot %s/%s cannot be quarantined by this store; treating as absent", namespace, taskID)
		}
		return nil, os.ErrNotExist
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
