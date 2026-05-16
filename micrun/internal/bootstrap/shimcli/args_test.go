package shimcli

import "testing"

func TestParseArgsSupportsSeparatedAndEqualsValues(t *testing.T) {
	args := ParseArgs([]string{
		"-namespace", "k8s.io",
		"-id=pod-1",
		"--bundle", "/run/bundle",
	})

	if got := args.Value("-namespace"); got != "k8s.io" {
		t.Fatalf("namespace = %q, want k8s.io", got)
	}
	if got := args.Value("-id"); got != "pod-1" {
		t.Fatalf("id = %q, want pod-1", got)
	}
	if got := args.Value("--bundle"); got != "/run/bundle" {
		t.Fatalf("bundle = %q, want /run/bundle", got)
	}
	if !args.HasOption("-namespace", "-id", "--bundle") {
		t.Fatal("expected parsed options to be recorded")
	}
}

func TestParseArgsStopsAtDoubleDash(t *testing.T) {
	args := ParseArgs([]string{"-id", "pod-1", "--", "-namespace", "ignored"})

	if got := args.Value("-id"); got != "pod-1" {
		t.Fatalf("id = %q, want pod-1", got)
	}
	if got := args.Value("-namespace"); got != "" {
		t.Fatalf("namespace = %q, want empty", got)
	}
}

func TestParseArgsDoesNotTreatFlagValueAsOption(t *testing.T) {
	args := ParseArgs([]string{"-id", "--help"})

	if args.HasOption("--help") {
		t.Fatal("flag value was recorded as option")
	}
}

func TestParseArgsBoolOptionHonorsExplicitFalse(t *testing.T) {
	args := ParseArgs([]string{"--help=false", "-v=true"})

	if args.BoolOption("--help") {
		t.Fatal("help bool option should be false")
	}
	if !args.BoolOption("-v") {
		t.Fatal("version bool option should be true")
	}
}

func TestParseArgsStopsAtFirstAction(t *testing.T) {
	args := ParseArgs([]string{"-debug", "start", "--help", "-id", "ignored"})

	if args.HasOption("--help") {
		t.Fatal("option after action was recorded")
	}
	if got := args.Value("-id"); got != "" {
		t.Fatalf("id = %q, want empty after action", got)
	}
}

func TestNewStartupUsesExplicitNamespace(t *testing.T) {
	t.Setenv("CONTAINERD_NAMESPACE", "from-env")

	startup := NewStartup("io.containerd.mica.v2", []string{"micrun", "-namespace", "from-arg"})

	if startup.Namespace != "from-arg" {
		t.Fatalf("namespace = %q, want from-arg", startup.Namespace)
	}
}

func TestNewStartupSupportsLongEqualsContextFlags(t *testing.T) {
	startup := NewStartup("io.containerd.mica.v2", []string{
		"micrun",
		"--namespace=k8s.io",
		"--id=pod-1",
	})

	if startup.Namespace != "k8s.io" {
		t.Fatalf("namespace = %q, want k8s.io", startup.Namespace)
	}
	if startup.ContainerID != "pod-1" {
		t.Fatalf("container id = %q, want pod-1", startup.ContainerID)
	}
}

func TestNewStartupFallsBackToContainerdNamespaceEnv(t *testing.T) {
	t.Setenv("CONTAINERD_NAMESPACE", "from-env")

	startup := NewStartup("io.containerd.mica.v2", []string{"micrun"})

	if startup.Namespace != "from-env" {
		t.Fatalf("namespace = %q, want from-env", startup.Namespace)
	}
}
