package console

func (i *InputInterpreter) interpretTTY(data []byte) []Action {
	actions := newActionBuffer(len(data) * 3)
	for _, ch := range data {
		if actions.appendAll(i.interpretTTYByte(ch)) {
			return actions.list()
		}
	}
	return actions.list()
}

func (i *InputInterpreter) interpretTTYByte(ch byte) []Action {
	if next, consumed := i.consumeDetach(ch); consumed {
		return next
	}
	return i.interpretTTYByteWithoutDetach(ch)
}

func (i *InputInterpreter) interpretTTYByteWithoutDetach(ch byte) []Action {
	if isInterruptKey(ch) {
		i.resetLineState()
		return []Action{emitEvent(EventInterrupt)}
	}
	if isBackspaceKey(ch) {
		return i.handleBackspace(ch)
	}
	lineEnding := i.lineEnding.consume(ch)
	if lineEnding.skip {
		return nil
	}
	if lineEnding.lineEnd {
		return i.completeTTYLine()
	}
	return i.acceptTTYByte(ch)
}

func (i *InputInterpreter) handleBackspace(ch byte) []Action {
	actions := []Action{writeTTY([]byte{ch})}
	if i.ttyLine.backspace() {
		actions = append(actions, localEcho([]byte{'\b', ' ', '\b'}))
	}
	return actions
}

func (i *InputInterpreter) completeTTYLine() []Action {
	if i.ttyLine.isExitCommand() {
		i.resetLineState()
		return []Action{
			writeStdout([]byte{'\r', '\n'}),
			emitEvent(EventExitCommand),
		}
	}
	i.ttyLine.reset()
	return []Action{writeTTY([]byte{'\r', '\n'})}
}

func (i *InputInterpreter) acceptTTYByte(ch byte) []Action {
	i.ttyLine.appendByte(ch)
	return []Action{localEcho([]byte{ch}), trackEchoByte(ch), writeTTY([]byte{ch})}
}

func (i *InputInterpreter) consumeDetach(ch byte) ([]Action, bool) {
	result := i.detach.consume(ch)
	if !result.consumed {
		return nil, false
	}

	if result.matched {
		i.resetLineState()
		return []Action{emitEvent(EventDetach)}, true
	}
	if len(result.flushed) > 0 || result.hasReplay {
		actions := make([]Action, 0, 1+3)
		if len(result.flushed) > 0 {
			actions = append(actions, writeTTY(result.flushed))
		}
		if result.hasReplay {
			actions = append(actions, i.interpretTTYByteWithoutDetach(result.replay)...)
		}
		return actions, true
	}
	return nil, true
}
