package main

import (
	"os"

	"micrun/internal/bootstrap"
	shimtransport "micrun/internal/transport/shimv2"
)

const (
	defaultShimName = "io.containerd.mica.v2"
)

// ShimName can still be overridden by Makefile ldflags for packaged builds.
var ShimName = defaultShimName

func main() {
	os.Exit(bootstrap.Run(bootstrap.Config{
		ShimName: ShimName,
		Init:     shimtransport.New,
	}))
}
