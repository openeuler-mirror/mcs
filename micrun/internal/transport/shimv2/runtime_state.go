package shim

import (
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	"micrun/internal/support/validation"
)

func (s *shimService) ensureTaskStore() map[string]*shimContainer {
	if s == nil {
		return nil
	}
	if s.containers == nil {
		s.containers = make(map[string]*shimContainer)
	}
	return s.containers
}

func (s *shimService) lookupShimTask(id string) (*shimContainer, bool) {
	if s == nil {
		return nil, false
	}
	c, ok := s.containers[id]
	if !ok || validation.IsNil(c) {
		return nil, false
	}
	return c, true
}

func (s *shimService) hasShimTask(id string) bool {
	_, found := s.lookupShimTask(id)
	return found
}

func (s *shimService) saveShimTask(id string, taskHandle ports.Task) {
	if validation.IsNil(taskHandle) {
		return
	}
	c, ok := taskHandle.(*shimContainer)
	if !ok || validation.IsNil(c) {
		return
	}
	tasks := s.ensureTaskStore()
	if tasks == nil {
		return
	}
	tasks[id] = c
}

func (s *shimService) deleteShimTask(id string) {
	if s == nil {
		return
	}
	delete(s.containers, id)
}

func (s *shimService) hasShimTasks() bool {
	if s == nil {
		return false
	}
	for _, taskHandle := range s.containers {
		if !validation.IsNil(taskHandle) {
			return true
		}
	}
	return false
}

func (s *shimService) currentSandbox() (cntr.SandboxTraits, bool) {
	if s == nil || validation.IsNil(s.sandbox) {
		return nil, false
	}
	return s.sandbox, true
}

func (s *shimService) setSandboxTraits(sandbox cntr.SandboxTraits) {
	if s == nil {
		return
	}
	if validation.IsNil(sandbox) {
		s.sandbox = nil
		return
	}
	s.sandbox = sandbox
}

func (s *shimService) clearSandbox() {
	s.setSandboxTraits(nil)
}

func (s *shimService) sandboxRuntime() ports.Sandbox {
	sandbox, ok := s.currentSandbox()
	if !ok {
		return nil
	}
	return runtimeSandbox{SandboxTraits: sandbox}
}
