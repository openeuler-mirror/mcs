package task

import er "micrun/internal/support/errors"

func rejectExecID(execID string) error {
	if execID == "" {
		return nil
	}
	return er.FlexibleTaskUnsupported
}
