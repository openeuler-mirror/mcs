package shimcli

import (
	"bytes"
	"testing"

	log "micrun/internal/support/logger"
)

func TestConfigureLoggerUsesStartupContext(t *testing.T) {
	t.Setenv(log.ContainerdLogPathEnv, t.TempDir()+"/missing-fifo")
	oldNamespace := log.GetNamespace()
	oldContainerID := log.GetContainerID()
	defer func() {
		log.SetNamespace(oldNamespace)
		log.SetContainerID(oldContainerID)
	}()

	startup := NewStartup("io.containerd.mica.v2", []string{"micrun", "-namespace", "k8s.io", "-id", "pod-1"})
	ConfigureLogger(startup, &bytes.Buffer{})

	if got := log.GetNamespace(); got != "k8s.io" {
		t.Fatalf("namespace = %q, want k8s.io", got)
	}
	if got := log.GetContainerID(); got != "pod-1" {
		t.Fatalf("container id = %q, want pod-1", got)
	}
}
