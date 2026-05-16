package console

import "bytes"

type inputLine struct {
	data []byte
}

func (l *inputLine) appendByte(ch byte) {
	l.data = append(l.data, ch)
}

func (l *inputLine) append(data []byte) {
	l.data = append(l.data, data...)
}

func (l *inputLine) backspace() bool {
	if len(l.data) == 0 {
		return false
	}
	l.data = l.data[:len(l.data)-1]
	return true
}

func (l *inputLine) reset() {
	l.data = l.data[:0]
}

func (l *inputLine) isExitCommand() bool {
	trimmed := bytes.TrimSpace(l.data)
	return len(trimmed) == 4 && bytes.EqualFold(trimmed, []byte("exit"))
}
