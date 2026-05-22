package bootstrap

import (
	"fmt"
	"io"
	"os"

	"micrun/internal/bootstrap/shimcli"
	"micrun/internal/support/netns"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

type Config struct {
	ShimName         string
	Args             []string
	Stdout           io.Writer
	Stderr           io.Writer
	Init             shimv2.Init
	RunHolderCommand func([]string) bool
	RunShim          func(string, shimv2.Init, ...shimv2.BinaryOpts)
}

func Run(config Config) int {
	config = config.withDefaults()
	startup := shimcli.NewStartup(config.ShimName, config.Args)
	if config.RunHolderCommand(startup.Args) {
		return 0
	}

	if shimcli.HandleEarlyCommand(startup, config.Stdout) {
		return 0
	}

	if config.Init == nil {
		fmt.Fprintln(config.Stderr, "micrun bootstrap: shim init function is required")
		return 2
	}

	config.RunShim(
		config.ShimName,
		config.Init,
		noReaper,
		noSubreaper,
		loggerOption(startup, config.Stderr),
	)
	return 0
}

func (c Config) withDefaults() Config {
	if c.Args == nil {
		c.Args = os.Args
	}
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	if c.RunHolderCommand == nil {
		c.RunHolderCommand = netns.RunHolderCommand
	}
	if c.RunShim == nil {
		c.RunShim = shimv2.Run
	}
	return c
}

func noReaper(c *shimv2.Config) {
	c.NoReaper = true
}

func noSubreaper(c *shimv2.Config) {
	c.NoSubreaper = true
}

func loggerOption(startup shimcli.Startup, stderr io.Writer) shimv2.BinaryOpts {
	return func(c *shimv2.Config) {
		c.NoSetupLogger = true
		shimcli.ConfigureLogger(startup, stderr)
	}
}
