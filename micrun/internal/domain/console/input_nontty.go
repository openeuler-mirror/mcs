package console

func (i *InputInterpreter) interpretNonTTY(data []byte) []Action {
	actions := newActionBuffer(4)
	lineStart := 0

	for idx, ch := range data {
		lineEnding := i.lineEnding.consume(ch)
		if lineEnding.skip {
			lineStart = idx + 1
			continue
		}
		if !lineEnding.lineEnd {
			continue
		}

		if i.appendNonTTYLineActions(&actions, data[lineStart:idx], true) {
			return actions.list()
		}
		lineStart = idx + 1
	}

	if lineStart == len(data) {
		return actions.list()
	}

	if i.appendNonTTYLineActions(&actions, data[lineStart:], false) {
		return actions.list()
	}
	return actions.list()
}

func (i *InputInterpreter) appendNonTTYLineActions(actions *actionBuffer, line []byte, hasNewline bool) bool {
	i.nonTTYLine.append(line)
	if hasNewline && i.nonTTYLine.isExitCommand() {
		i.resetLineState()
		return actions.append(emitEvent(EventExitCommand))
	}

	if hasNewline {
		i.nonTTYLine.reset()
	}

	writeData := line
	if hasNewline {
		writeData = append(append([]byte(nil), line...), '\n')
	}

	actions.appendPrintableTrack(writeData)
	actions.append(writeTTY(convertLFToCRLF(writeData)))
	return false
}
