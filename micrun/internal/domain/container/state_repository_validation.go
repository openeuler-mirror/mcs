package container

import (
	"fmt"

	er "micrun/internal/support/errors"
)

func (r stateRepository) validateSandboxForSave(sandbox *Sandbox) error {
	if sandbox == nil {
		return fmt.Errorf("sandbox is nil")
	}
	if sandbox.config == nil {
		return fmt.Errorf("sandbox config is nil")
	}
	if sandbox.id == "" {
		return er.EmptySandboxID
	}
	return r.validateStore()
}

func (r stateRepository) validateStore() error {
	if r.store == nil {
		return fmt.Errorf("state store is nil")
	}
	return nil
}
