package attach

import "testing"

func TestWithTaskLockIfAvailable(t *testing.T) {
	runtime := &fakeRuntime{}
	called := false

	ok := withTaskLockIfAvailable(runtime, func() {
		called = true
	})
	if !ok {
		t.Fatal("expected lock helper to acquire for concrete runtime")
	}
	if !called {
		t.Fatal("expected body to execute for concrete runtime")
	}

	called = false
	var nilRuntime *fakeRuntime
	ok = withTaskLockIfAvailable(nilRuntime, func() {
		called = true
	})
	if ok {
		t.Fatal("expected lock helper to skip when runtime is nil")
	}
	if called {
		t.Fatal("expected body not to execute for nil runtime")
	}
}
