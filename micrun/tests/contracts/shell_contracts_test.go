package contracts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", ".."))
}

func runBash(t *testing.T, script string) (string, error) {
	t.Helper()

	cmd := exec.Command("bash", "-lc", script)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readFile(t *testing.T, rel string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func TestRemoteSSHOptsDisableKnownHostsNoise(t *testing.T) {
	out, err := runBash(t, `
		source tests/common/remote.sh
		remote_ssh_opts | tr '\n' ' '
	`)
	if err != nil {
		t.Fatalf("remote_ssh_opts failed: %v\n%s", err, out)
	}

	if !strings.Contains(out, "UserKnownHostsFile") || !strings.Contains(out, "/dev/null") {
		t.Fatalf("remote_ssh_opts must disable known_hosts writes, got: %s", out)
	}

	if !strings.Contains(out, "LogLevel") || !strings.Contains(out, "ERROR") {
		t.Fatalf("remote_ssh_opts must suppress ssh host-key noise, got: %s", out)
	}
}

func TestSanitizeCommandOutputStripsSSHBanner(t *testing.T) {
	out, err := runBash(t, `
		source tests/io/test_helpers.sh
		cat <<'EOF' | sanitize_command_output
Warning: Permanently added '10.0.2.15' (ED25519) to the list of known hosts.
openEuler Embedded Reference Distro latest %h

                         ______      _
                        |  ____|    | |

Authorized uses only. All activity may be monitored and reported.
Last login: Thu Jan  1 00:18:07 1970 from 10.0.2.2
Hello, UniProton!
EOF
	`)
	if err != nil {
		t.Fatalf("sanitize_command_output failed: %v\n%s", err, out)
	}

	trimmed := strings.TrimSpace(out)
	if trimmed != "Hello, UniProton!" {
		t.Fatalf("unexpected sanitized output: %q", trimmed)
	}
}

func TestSanitizeCommandOutputStripsKnownNerdctlVersionWarning(t *testing.T) {
	out, err := runBash(t, `
		source tests/io/test_helpers.sh
		cat <<'EOF' | sanitize_command_output
time="2026-05-16T14:06:18Z" level=warning msg="failed to parse the containerd version \"v1.7.19.m\": Invalid Semantic Version"
support shell commond:
EOF
	`)
	if err != nil {
		t.Fatalf("sanitize_command_output failed: %v\n%s", err, out)
	}

	trimmed := strings.TrimSpace(out)
	if trimmed != "support shell commond:" {
		t.Fatalf("unexpected sanitized output: %q", trimmed)
	}
}

func TestCountShellMarkersIgnoresSSHBannerOpenEuler(t *testing.T) {
	out, err := runBash(t, `
		source tests/io/test_helpers.sh
		cat <<'EOF' | count_shell_markers
openEuler Embedded Reference Distro latest %h
Authorized uses only. All activity may be monitored and reported.
Hello, UniProton!
EOF
	`)
	if err != nil {
		t.Fatalf("count_shell_markers failed: %v\n%s", err, out)
	}

	if strings.TrimSpace(out) != "0" {
		t.Fatalf("banner-only output must not look like shell success, got: %q", strings.TrimSpace(out))
	}
}

func TestClassifyInteractionOutputPrefersShellMarkers(t *testing.T) {
	out, err := runBash(t, `
		source tests/io/test_helpers.sh
		cat <<'EOF' | classify_interaction_output
Hello, UniProton!
support shell commond
openEuler UniProton #
EOF
	`)
	if err != nil {
		t.Fatalf("classify_interaction_output failed: %v\n%s", err, out)
	}

	if strings.TrimSpace(out) != "shell" {
		t.Fatalf("expected shell classification, got: %q", strings.TrimSpace(out))
	}
}

func TestClassifyInteractionOutputFallsBackToHello(t *testing.T) {
	out, err := runBash(t, `
		source tests/io/test_helpers.sh
		cat <<'EOF' | classify_interaction_output
Hello, UniProton!
EOF
	`)
	if err != nil {
		t.Fatalf("classify_interaction_output failed: %v\n%s", err, out)
	}

	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("expected hello classification, got: %q", strings.TrimSpace(out))
	}
}

func TestClassifyInteractionOutputReturnsUnknownForNoise(t *testing.T) {
	out, err := runBash(t, `
		source tests/io/test_helpers.sh
		cat <<'EOF' | classify_interaction_output
some unrelated line
another unrelated line
EOF
	`)
	if err != nil {
		t.Fatalf("classify_interaction_output failed: %v\n%s", err, out)
	}

	if strings.TrimSpace(out) != "unknown" {
		t.Fatalf("expected unknown classification, got: %q", strings.TrimSpace(out))
	}
}

func TestHelloSuiteIncludesNerdctlCreateStartCoverage(t *testing.T) {
	suite := readFile(t, "tests/io/test_suite.sh")

	for _, marker := range []string{
		"test_hello_nerdctl_create_start",
		"test_hello_nerdctl_detached_lifecycle",
	} {
		if !strings.Contains(suite, marker) {
			t.Fatalf("missing nerdctl hello lifecycle marker %q in test suite", marker)
		}
	}
}

func TestHelloSuiteIncludesDelayedNerdctlInteractiveCommands(t *testing.T) {
	suite := readFile(t, "tests/io/test_suite.sh")
	helpers := readFile(t, "tests/io/test_helpers.sh")

	for _, marker := range []string{
		"Test 5: nerdctl non-TTY delayed interactive commands",
		"UNIPROTON_INTERACTION_WAIT_SECS",
		"help",
		"uname",
		"has_uniproton_response_markers",
	} {
		if !strings.Contains(suite, marker) {
			t.Fatalf("hello-profile nerdctl -i coverage missing marker %q", marker)
		}
	}
	if !strings.Contains(helpers, "UNIPROTON_INTERACTION_WAIT_SECS") {
		t.Fatal("missing configurable uniproton interaction wait helper")
	}
}

func TestCtrAttachCoverageUsesDelayedInputPattern(t *testing.T) {
	suite := readFile(t, "tests/io/test_suite.sh")

	for _, marker := range []string{
		"Test 6: ctr attach delayed input path stays observable",
		"UNIPROTON_INTERACTION_WAIT_SECS",
		"help\\\\nuname",
		"has_uniproton_response_markers",
	} {
		if !strings.Contains(suite, marker) {
			t.Fatalf("ctr attach coverage missing marker %q", marker)
		}
	}
}

func TestShellSuiteIncludesNerdctlAttachDetachCoverage(t *testing.T) {
	suite := readFile(t, "tests/io/test_suite.sh")
	expectScript := readFile(t, "tests/io/test_nerdctl_attach_detach.exp")

	if !strings.Contains(suite, "test_shell_nerdctl_attach_detach") {
		t.Fatal("missing shell nerdctl attach/detach lifecycle test in suite")
	}
	if !strings.Contains(suite, "test_nerdctl_attach_detach.exp") {
		t.Fatal("suite does not integrate the dedicated nerdctl attach/detach expect script")
	}
	if !strings.Contains(expectScript, "nerdctl attach") || !strings.Contains(expectScript, "nerdctl rm -f") {
		t.Fatal("attach/detach expect script does not cover attach and cleanup flow")
	}
}

func TestRunAllTestsIncludesK3sInteractionByDefault(t *testing.T) {
	runner := readFile(t, "tests/run_all_tests.sh")
	k3sSuite := readFile(t, "tests/k3s/run_k3s_tests.sh")

	for _, marker := range []string{
		"run_k3s_tests",
		"K3S_INCLUDE_INTERACTION=\"${K3S_INCLUDE_INTERACTION:-true}\"",
		"K3S_INCLUDE_OTA",
		"run_k3s_tests \"$test_id\"",
		"run_k3s_tests;",
		"kubectl attach 交互",
	} {
		if !strings.Contains(runner, marker) {
			t.Fatalf("run_all_tests.sh does not integrate K3s marker %q", marker)
		}
	}

	for _, marker := range []string{
		"interaction) test_k3s_008_interaction",
		"run_interaction_e2e.sh",
		"ota) test_k3s_009_ota",
		"run_ota_e2e.sh",
		// Legacy numeric IDs must stay accepted as aliases (old command
		// lines and docs reference them).
		"K3S-008) echo \"interaction\"",
		"K3S-009) echo \"ota\"",
	} {
		if !strings.Contains(k3sSuite, marker) {
			t.Fatalf("K3s suite does not include interaction marker %q", marker)
		}
	}
}

func TestPublicK3sEntrypointsStayRegistered(t *testing.T) {
	readFile(t, "tests/bin/test-k3s-single-node")
	readFile(t, "tests/bin/test-k3s-cloud-edge")
	readFile(t, "tests/bin/test-k3s-interaction")
	readFile(t, "tests/bin/test-k3s-ota")

	readme := readFile(t, "tests/README.md")
	for _, marker := range []string{
		"micrun/tests/bin/test-k3s-single-node",
		"micrun/tests/bin/test-k3s-cloud-edge",
		"micrun/tests/bin/test-k3s-interaction",
		"micrun/tests/bin/test-k3s-ota",
		"micrun/tests/run_all_tests.sh",
	} {
		if !strings.Contains(readme, marker) {
			t.Fatalf("tests README is missing public K3s entrypoint %q", marker)
		}
	}
}

// Spec acceptance 3 (builder image contract): the annotation keys
// mica-image-builder writes into images must match the keys the shim
// consumes (internal/support/annotations). A drift on either side silently
// breaks RTOS type / firmware / pedestal selection at run time — the image
// still imports and runs, just with defaults instead of its metadata.
func TestMicaImageBuilderAnnotationKeysMatchShim(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	if _, err := runBash(t, `python3 -c "
try:
    import tomllib
except Exception:
    import tomli
"`); err != nil {
		t.Skip("python3 lacks tomllib/tomli (label manager cannot load its config)")
	}

	out, err := runBash(t, `
		cd scripts/mica-image-builder && python3 - <<'EOF'
from mica_label_manager import MicaLabelManager
manager = MicaLabelManager()
for pedestal, os_type in (("xen", "uniproton"), ("xen", "zephyr"), ("baremetal", "uniproton")):
    _, annotations = manager.generate_labels_and_annotations(
        pedestal=pedestal, os_type=os_type,
        firmware_path="firmware.elf", xen_image_path="image.bin",
        description="contract", timestamp="t0",
        uniproton_version="v1", zephyr_version="v1")
    for key in sorted(annotations):
        print(f"{pedestal}/{os_type} {key}={annotations[key]}")
EOF
	`)
	if err != nil {
		t.Fatalf("mica_label_manager annotation generation failed: %v\n%s", err, out)
	}

	// Keys the shim actually consumes, spelled from the Go constants' string
	// values (annotations.go): container.os / container.firmware_path via
	// getOSInfo/resolveContainerFirmware, ped.pedestal / ped.conf via
	// extPedConfig.
	for _, want := range []string{
		"org.openeuler.micrun.container.os=",
		"org.openeuler.micrun.container.firmware_path=",
		"org.openeuler.micrun.ped.pedestal=",
		"org.openeuler.micrun.ped.conf=",
	} {
		for _, combo := range []string{"xen/uniproton", "xen/zephyr", "baremetal/uniproton"} {
			if !strings.Contains(out, combo+" "+want) {
				t.Fatalf("builder output for %s is missing shim-consumed annotation %q\n%s", combo, want, out)
			}
		}
	}

	// The os annotation value must be the exact whitelisted RTOS name.
	for _, want := range []string{
		"xen/uniproton org.openeuler.micrun.container.os=uniproton",
		"xen/zephyr org.openeuler.micrun.container.os=zephyr",
		"xen/uniproton org.openeuler.micrun.ped.pedestal=xen",
		"baremetal/uniproton org.openeuler.micrun.ped.pedestal=baremetal",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("builder output is missing %q\n%s", want, out)
		}
	}
}
