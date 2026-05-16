// Package console owns user-facing terminal input semantics.
//
// It deliberately does not know about FIFOs, RPMSG TTY devices, containerd,
// shim tasks, or logging. The adapter layer feeds bytes in and executes the
// returned actions.
package console

// InputConfig configures a byte interpreter for one container console session.
type InputConfig struct {
	Terminal   bool
	ExecMode   bool
	DetachKeys string
}

// InputInterpreter converts raw stdin bytes into device writes and semantic
// events. It is stateful because terminal input can be fragmented across FIFO
// reads.
type InputInterpreter struct {
	config InputConfig

	ttyLine    inputLine
	nonTTYLine inputLine
	lineEnding inputLineEnding

	detach detachSequence
}

// NewInputInterpreter creates a stateful interpreter for a session.
func NewInputInterpreter(config InputConfig) *InputInterpreter {
	return &InputInterpreter{
		config: config,
		detach: newDetachSequence(config),
	}
}

// Interpret processes a FIFO read chunk.
func (i *InputInterpreter) Interpret(data []byte) []Action {
	if i.config.Terminal {
		return i.interpretTTY(data)
	}
	return i.interpretNonTTY(data)
}

func (i *InputInterpreter) resetLineState() {
	i.ttyLine.reset()
	i.nonTTYLine.reset()
	i.lineEnding.reset()
	i.detach.reset()
}
