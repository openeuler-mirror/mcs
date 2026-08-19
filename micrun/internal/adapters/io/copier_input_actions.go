package io

import (
	"io"
	"time"

	"micrun/internal/domain/console"
	log "micrun/internal/support/logger"
)

type inputActionExecutor struct {
	copier *Copier
}

type inputActionHandler func(inputActionExecutor, console.Action) console.ActionStopMode

var inputActionHandlers = map[console.ActionKind]inputActionHandler{
	console.ActionWriteTTY:    handleInputActionWriteTTY,
	console.ActionWriteStdout: handleInputActionWriteStdout,
	console.ActionLocalEcho:   handleInputActionLocalEcho,
	console.ActionTrackEcho:   handleInputActionTrackEcho,
	console.ActionEmitEvent:   handleInputActionEmitEvent,
}

func (c *Copier) executeInputActions(actions []console.Action) {
	newInputActionExecutor(c).execute(actions)
}

func newInputActionExecutor(c *Copier) inputActionExecutor {
	return inputActionExecutor{copier: c}
}

func (e inputActionExecutor) execute(actions []console.Action) {
	for _, action := range actions {
		stopMode := e.executeOne(action)
		if stopMode == console.ActionStopNone {
			stopMode = action.EffectiveStopMode()
		}
		if stopMode != console.ActionStopNone {
			e.stop(stopMode)
			return
		}
	}
}

func (e inputActionExecutor) executeOne(action console.Action) console.ActionStopMode {
	handler, ok := inputActionHandlers[action.Kind]
	if !ok {
		log.Warnf("[IO] Unsupported input action kind=%d for %s", action.Kind, e.copier.config.ContainerID)
		return console.ActionStopNone
	}
	return handler(e, action)
}

func (e inputActionExecutor) stop(mode console.ActionStopMode) {
	// This runs inside a copier worker goroutine (copyStdin), which is itself a
	// wg member. Both branches must use the no-wait variant to avoid wg.Wait()
	// waiting for the caller to finish (self-deadlock).
	if mode == console.ActionStopPreserve {
		e.copier.stopFromWorker(false)
		return
	}
	e.copier.stopFromWorker(true)
}

func handleInputActionWriteTTY(e inputActionExecutor, action console.Action) console.ActionStopMode {
	c := e.copier
	written, err := c.writeTTY(action.Data)
	if err != nil {
		log.Errorf("[IO] TTY write error for %s: %v", c.config.ContainerID, err)
		c.publishEvent(IOError, err)
		return console.ActionStopClose
	}
	log.Tracef("[IO] TTY write OK for %s: wrote %d bytes", c.config.ContainerID, written)
	return console.ActionStopNone
}

func handleInputActionWriteStdout(e inputActionExecutor, action console.Action) console.ActionStopMode {
	if err := writeActionDataWithRetry(e.copier, e.copier.stdoutFIFO, action.Data); err != nil {
		log.Infof("[IO] Stdout action write FAILED for %s: %v", e.copier.config.ContainerID, err)
	}
	return console.ActionStopNone
}

func handleInputActionLocalEcho(e inputActionExecutor, action console.Action) console.ActionStopMode {
	if err := writeActionDataWithRetry(e.copier, e.copier.stdoutFifoForEcho, action.Data); err != nil {
		log.Infof("[IO] Local echo FAILED for %s: %v", e.copier.config.ContainerID, err)
	}
	return console.ActionStopNone
}

func handleInputActionTrackEcho(e inputActionExecutor, action console.Action) console.ActionStopMode {
	e.copier.trackSentCharsForEcho(action.Data)
	return console.ActionStopNone
}

func handleInputActionEmitEvent(e inputActionExecutor, action console.Action) console.ActionStopMode {
	e.publishConsoleEvent(action.Event)
	return console.ActionStopNone
}

type consoleEventRoute struct {
	eventType EventType
	logFormat string
}

var consoleEventRoutes = map[console.EventKind]consoleEventRoute{
	console.EventExitCommand: {
		eventType: ExitCommandDetected,
		logFormat: "[IO] 'exit' command detected for %s, stopping IO copier",
	},
	console.EventDetach: {
		eventType: DetachDetected,
		logFormat: "[IO] Detach sequence detected for %s",
	},
	console.EventInterrupt: {
		eventType: InterruptDetected,
		logFormat: "[IO] Interrupt key (Ctrl+C) detected for %s",
	},
}

func (c *Copier) publishConsoleEvent(event console.EventKind) {
	newInputActionExecutor(c).publishConsoleEvent(event)
}

func (e inputActionExecutor) publishConsoleEvent(event console.EventKind) {
	route, ok := consoleEventRoutes[event]
	if !ok {
		log.Warnf("[IO] Unsupported console event=%d for %s", event, e.copier.config.ContainerID)
		return
	}
	if route.logFormat != "" {
		log.Infof(route.logFormat, e.copier.config.ContainerID)
	}
	e.copier.publishEvent(route.eventType, nil)
}

func writeActionData(writer io.Writer, data []byte) error {
	if writer == nil || len(data) == 0 {
		return nil
	}
	n, err := writer.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// writeActionDataWithRetry writes echo/stdout action data to the given
// FIFO with EAGAIN retry: these FIFOs are now non-blocking, so a slow
// reader makes Write return EAGAIN and a bare write would silently drop
// the echoed character. Retry briefly, bounded by context cancellation.
//
// EPIPE/ENXIO mean there is no FIFO reader (detached / stdout closed).
// Retrying them forever would stall the stdin worker before WriteTTY or
// ExitCommandDetected, freezing keystrokes and blocking typed "exit".
// Skip the best-effort echo/CRLF and let later actions proceed.
func writeActionDataWithRetry(c *Copier, writer io.Writer, data []byte) error {
	if c == nil || writer == nil || len(data) == 0 {
		return nil
	}
	remaining := data
	for {
		n, err := writer.Write(remaining)
		if err != nil {
			if isBrokenPipe(err) || isENXIO(err) {
				return nil
			}
			if isEAGAIN(err) {
				// Resume from the unwritten suffix, mirroring writeOutputFIFO:
				// an io.Writer may report a partial write alongside EAGAIN, and
				// re-sending the already-written prefix would duplicate the
				// echoed characters on the client's terminal.
				if n > 0 {
					remaining = remaining[n:]
					if len(remaining) == 0 {
						return nil
					}
				}
				select {
				case <-c.ctx.Done():
					return c.ctx.Err()
				case <-time.After(outputWriteRetryDelay):
				}
				continue
			}
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		if n < len(remaining) {
			remaining = remaining[n:]
			continue
		}
		return nil
	}
}
