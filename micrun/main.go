package main

import (
	"os"

	log "micrun/logger"
	"micrun/pkg/shim"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// ShimName injected in Makefile.
// TODO: change to const value, after MicRun name is stable
var ShimName string

func main() {
	if isBootstrapStart() {
		// During bootstrap "start", containerd reads CombinedOutput from the shim
		// for a strict JSON/address handshake. Any stderr/stdout noise corrupts
		// the handshake and leaves the shim socket file behind. Silence console
		// output in this phase.
		log.SilenceOutput()
	}

	// Log shim startup for troubleshooting (before init, goes to discard)
	log.Debug("MicRun shim starting with args:", os.Args)

	if !isTaskRequest() {
		// This shouldn't happen in normal operation
		log.Debug("Not a task request, exiting early")
		os.Exit(0)
	}

	log.Debug("Initializing shim with containerd...")
	shimv2.Run(ShimName, shim.New, noReaper, noSubreaper, setupLogger)
	// Avoid noisy info log after start handshake; keep at debug level.
	log.Debug("shimv2.Run() returned normally")
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
		// If initialization fails, log to stderr and continue
		log.Error("Failed to initialize logger:", err)
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

	// Restore log output if we silenced it during bootstrap
	if isBootstrapStart() {
		if err := log.RestoreOutput(); err != nil {
			log.Error("Failed to restore log output:", err)
		}
	}
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

func isBootstrapStart() bool {
	for _, arg := range os.Args[1:] {
		if arg == "start" {
			return true
		}
	}
	return false
}

func isTaskRequest() bool {
	if len(os.Args) == 1 {
		return false
	}

	for _, arg := range os.Args[1:] {
		switch arg {
		case "-v", "--version", "-h", "--help":
			return false
		}
	}
	return true
}
