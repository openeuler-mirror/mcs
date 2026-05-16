package io

import (
	"errors"
	"fmt"

	"micrun/internal/support/validation"
)

func mergeCloseErrors(closeResults ...[]error) []error {
	if len(closeResults) == 0 {
		return nil
	}

	total := 0
	for _, result := range closeResults {
		total += len(result)
	}

	errs := make([]error, 0, total)
	for _, result := range closeResults {
		errs = append(errs, result...)
	}
	return errs
}

func joinCloseErrors(closeResults ...[]error) error {
	return errors.Join(mergeCloseErrors(closeResults...)...)
}

// closeStream closes resources that are known to implement io.Closer.
func closeStream[T interface{ Close() error }](name string, stream *T) []error {
	return closeStreamIfCloseable(name, stream, nil)
}

// closeStreamIfCloseable closes a possibly closable stream and always resets the
// caller's reference to a zero value. If the target is nil (including typed nil)
// or not closable, it is still reset for deterministic lifecycle handling.
// The optional ignore function can suppress wrapped errors for tolerated close
// failures (for example, os.ErrClosed).
func closeStreamIfCloseable[T any](name string, stream *T, ignoreErr func(error) bool) []error {
	if stream == nil {
		return nil
	}

	if validation.IsNil(*stream) {
		resetPointer(stream)
		return nil
	}

	closer, ok := any(*stream).(interface{ Close() error })
	if !ok {
		resetPointer(stream)
		return nil
	}

	err := closer.Close()
	resetPointer(stream)
	if err != nil && (ignoreErr == nil || !ignoreErr(err)) {
		return []error{fmt.Errorf("close %s: %w", name, err)}
	}
	return nil
}
