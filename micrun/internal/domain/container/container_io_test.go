package container

import (
	"reflect"
	"testing"
)

func TestContainerTTYDiscoveryRootsUseSandboxDependency(t *testing.T) {
	want := []string{"/dev", "/custom/micrun", "/tmp/mica"}
	container := &Container{
		sandbox: &Sandbox{
			deps: &Dependencies{
				TTYDiscoveryRoots: func() []string {
					return want
				},
			},
		},
	}

	if got := container.ttyDiscoveryRoots(); !reflect.DeepEqual(got, want) {
		t.Fatalf("tty discovery roots = %v, want %v", got, want)
	}
}
