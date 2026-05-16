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

func (r stateRepository) validateContainerForSave(container *Container) error {
	if container == nil {
		return fmt.Errorf("container is nil")
	}
	if container.sandbox == nil {
		return fmt.Errorf("container sandbox is nil")
	}
	if container.config == nil {
		return fmt.Errorf("container config is nil")
	}
	if container.id == "" {
		return er.EmptyContainerID
	}
	return r.validateStore()
}

func (r stateRepository) validateStore() error {
	if r.store == nil {
		return fmt.Errorf("state store is nil")
	}
	return nil
}
