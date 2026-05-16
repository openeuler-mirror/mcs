package shimcli

import (
	"fmt"
	"io"

	"micrun/version"
)

func HandleEarlyCommand(startup Startup, stdout io.Writer) bool {
	if startup.BoolOption("-v", "-version", "--version") {
		printVersion(stdout, startup.BinaryName)
		return true
	}
	if startup.BoolOption("-h", "-help", "--help") {
		printHelp(stdout, startup.BinaryName)
		return true
	}
	return false
}

func printVersion(stdout io.Writer, binaryName string) {
	fmt.Fprintf(stdout, "%s:\n", binaryName)
	fmt.Fprintln(stdout, "  Version:  ", version.Version)
	fmt.Fprintln(stdout, "  Revision: ", version.Revision)
	fmt.Fprintln(stdout, "  Go version:", version.GoVersion)
}

func printHelp(stdout io.Writer, binaryName string) {
	fmt.Fprintf(stdout, "Usage: %s [OPTIONS]\n\n", binaryName)
	fmt.Fprintln(stdout, "micrun is a containerd shim v2 runtime for RTOS containers.")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Options:")
	fmt.Fprintln(stdout, "  -v, --version     Show version information and exit")
	fmt.Fprintln(stdout, "  -h, --help        Show this help message and exit")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Shim v2 options (handled by containerd):")
	fmt.Fprintln(stdout, "  -id string        Container ID")
	fmt.Fprintln(stdout, "  -namespace string Namespace for the container")
	fmt.Fprintln(stdout, "  -debug            Enable debug output in logs")
	fmt.Fprintln(stdout, "  -address string   Address of containerd's main grpc socket")
	fmt.Fprintln(stdout, "  -bundle string    Path to the bundle")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "For more information, see: https://github.com/openeuler/mica")
}
