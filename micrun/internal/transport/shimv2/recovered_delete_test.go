package shim

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cntr "micrun/internal/domain/container"
)

func TestRecoveredTaskRootfs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// The daemon shim runs with cwd = the sandbox task's bundle.
	sandboxTask := &shimContainer{id: "sandbox-id", cType: cntr.PodSandbox}
	if got, want := recoveredTaskRootfs(sandboxTask), filepath.Join(cwd, "rootfs"); got != want {
		t.Fatalf("recoveredTaskRootfs(sandbox) = %q, want %q", got, want)
	}

	// A pod container's bundle is its sibling under the namespace directory.
	podTask := &shimContainer{id: "container-1", cType: cntr.PodContainer}
	if got, want := recoveredTaskRootfs(podTask), filepath.Join(filepath.Dir(cwd), "container-1", "rootfs"); got != want {
		t.Fatalf("recoveredTaskRootfs(pod container) = %q, want %q", got, want)
	}
}

// A recovered task carries no mounted/bundle bookkeeping; Delete must still
// succeed and attempt a best-effort unmount at the conventional bundle path
// (a no-op here since nothing is mounted at that path).
func TestDeleteContainerRecoveredTaskWithoutMountBookkeeping(t *testing.T) {
	svc := &shimService{}
	c := &shimContainer{
		id:        "recovered-task",
		cType:     cntr.PodSandbox,
		recovered: true,
	}
	if err := deleteContainer(context.Background(), svc, c); err != nil {
		t.Fatalf("deleteContainer error = %v", err)
	}
}
