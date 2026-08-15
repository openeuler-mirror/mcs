package shimcli

import (
	"fmt"
	"io"

	log "micrun/internal/support/logger"
)

func ConfigureLogger(startup Startup, stderr io.Writer) {
	if err := log.Initialize(nil); err != nil {
		fmt.Fprintf(stderr, "Failed to initialize logger: %v\n", err)
	}
	// The containerd shim framework skips its own logger setup for us
	// (NoSetupLogger), so the -debug flag passed to the daemon subcommand
	// reaches nobody. Honor it here: debug mode must actually lower the log
	// threshold, not just set a flag nobody reads.
	if startup.BoolOption("-debug", "--debug") {
		log.ForceDebugLevel()
	}
	if startup.ContainerID != "" {
		log.SetContainerID(startup.ContainerID)
	}
	log.SetNamespace(startup.Namespace)
}

func namespaceForLogging(args Args) string {
	if namespace := args.Value("-namespace", "--namespace"); namespace != "" {
		return namespace
	}
	return log.GetDefaultNamespace()
}
