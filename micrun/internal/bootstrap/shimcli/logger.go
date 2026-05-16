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
