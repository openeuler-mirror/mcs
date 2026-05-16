package shim

import (
	"bytes"
	"strings"
	"testing"

	log "micrun/internal/support/logger"

	"github.com/sirupsen/logrus"
)

func TestIOExitDoesNotEmitStackTrace(t *testing.T) {
	var buf bytes.Buffer
	oldOut := log.Log.Out
	oldLevel := log.Log.Level
	log.Log.SetOutput(&buf)
	log.Log.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		log.Log.SetOutput(oldOut)
		log.Log.SetLevel(oldLevel)
	})

	c := &shimContainer{
		id:       "ioexit-test",
		exitIOch: make(chan struct{}),
	}
	c.ioExit()

	output := buf.String()
	if strings.Contains(output, "ioExit() call stack") || strings.Contains(output, "ioExit() called from") {
		t.Fatalf("unexpected ioExit stack output: %s", output)
	}
}
