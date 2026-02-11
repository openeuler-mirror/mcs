package main

import (
	"fmt"
	"os"
	"path/filepath"

	log "micrun/logger"
	"micrun/pkg/shim"
	"micrun/version"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// ShimName injected in Makefile.
// TODO: change to const value, after MicRun name is stable
var ShimName string

func main() {
	// Handle early commands (version/help) before starting shim
	if handleEarlyCommand() {
		fmt.Println("not starting shim, handled version/help request")
		os.Exit(0)
	}

	shimv2.Run(ShimName, shim.New, noReaper, noSubreaper, setupLogger)
}

// handleEarlyCommand handles version and help flags.
// Returns true if an early command was handled (program should exit).
// Note: This function manually checks arguments without using flag.Parse()
// to avoid interfering with shim.Run()'s flag parsing (e.g., -namespace).
func handleEarlyCommand() bool {
	// 手动检查参数，不使用 flag.Parse() 避免干扰 shim.Run() 的 flag 解析
	args := os.Args[1:]
	for _, arg := range args {
		switch arg {
		case "-v", "-version", "--version":
			printVersion()
			return true
		case "-h", "-help", "--help":
			printHelp()
			return true
		}
	}
	return false
}

// printVersion prints micrun version information
func printVersion() {
	fmt.Printf("%s:\n", binaryName())
	fmt.Println("  Version:  ", version.Version)
	fmt.Println("  Revision: ", version.Revision)
	fmt.Println("  Go version:", version.GoVersion)
}

// printHelp prints micrun help information
func printHelp() {
	fmt.Printf("Usage: %s [OPTIONS]\n\n", binaryName())
	fmt.Println("micrun is a containerd shim v2 runtime for RTOS containers.")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -v, --version     Show version information and exit")
	fmt.Println("  -h, --help        Show this help message and exit")
	fmt.Println()
	fmt.Println("Shim v2 options (handled by containerd):")
	fmt.Println("  -id string        Container ID")
	fmt.Println("  -namespace string Namespace for the container")
	fmt.Println("  -debug            Enable debug output in logs")
	fmt.Println("  -address string   Address of containerd's main grpc socket")
	fmt.Println("  -bundle string    Path to the bundle")
	fmt.Println()
	fmt.Println("For more information, see: https://github.com/openeuler/mica")
}

// binaryName returns the shim binary name
func binaryName() string {
	if ShimName != "" {
		return "containerd-shim-" + ShimName
	}
	if len(os.Args) > 0 {
		return filepath.Base(os.Args[0])
	}
	return "micrun"
}

func noReaper(c *shimv2.Config) {
	c.NoReaper = true
}

func noSubreaper(c *shimv2.Config) {
	c.NoSubreaper = true
}

func setupLogger(c *shimv2.Config) {
	// Disable containerd's default logger setup since we use our own logger
	// This prevents FIFO errors when containerd tries to set up log FIFOs
	c.NoSetupLogger = true

	// Initialize logger with configuration
	// Will load from /etc/micrun/config.json if available
	if err := log.Initialize(nil); err != nil {
		// If initialization fails, print to stderr and continue
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
	}

	// Extract container ID from command-line arguments and set it for logging context
	// This ensures all logs include the container_id field
	if containerID := extractContainerID(); containerID != "" {
		log.SetContainerID(containerID)
	}

	// Set namespace from environment variable for logging context
	// This ensures all logs include the namespace field
	namespace := log.GetDefaultNamespace()
	log.SetNamespace(namespace)
}

// extractContainerID extracts the container ID from command-line arguments.
// The containerd shim passes the container ID via the -id flag.
func extractContainerID() string {
	args := os.Args
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-id" {
			return args[i+1]
		}
	}
	return ""
}
