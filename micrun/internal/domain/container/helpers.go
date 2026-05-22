package container

import er "micrun/internal/support/errors"

func copyUint32(v uint32) *uint32 {
	val := v
	return &val
}

func copyInt64(src *int64) *int64 {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

func copyUint64(src *uint64) *uint64 {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

func copyBool(src *bool) *bool {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

func (c *Container) requireSandbox() error {
	if c == nil {
		return er.ContainerNotFound
	}
	if c.sandbox == nil {
		return er.ContainerSandboxNil
	}
	return nil
}
