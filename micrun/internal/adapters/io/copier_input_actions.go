package io

import (
	"io"

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
	if mode == console.ActionStopPreserve {
		e.copier.StopWithoutClosingFIFOs()
		return
	}
	e.copier.Stop()
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
	if err := writeActionData(e.copier.stdoutFIFO, action.Data); err != nil {
		log.Infof("[IO] Stdout action write FAILED for %s: %v", e.copier.config.ContainerID, err)
	}
	return console.ActionStopNone
}

func handleInputActionLocalEcho(e inputActionExecutor, action console.Action) console.ActionStopMode {
	if err := writeActionData(e.copier.stdoutFifoForEcho, action.Data); err != nil {
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
