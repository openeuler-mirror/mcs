package shim

import (
	"fmt"

	"micrun/internal/support/validation"
)

func normalizeShimNamespace(namespace string) (string, error) {
	normalized, err := validation.NormalizeSinglePathSegment(namespace)
	if err != nil {
		return "", fmt.Errorf("namespace is invalid: %w", err)
	}
	return normalized, nil
}
