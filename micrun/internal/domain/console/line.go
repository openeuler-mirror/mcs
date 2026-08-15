package console

import "bytes"

// maxInputLineLen bounds the line buffer used for exit-command detection.
// stdin is untrusted and may never contain a newline; without a cap the
// buffer grows without bound and a hostile or chatty client can OOM the shim
// (CWE-400). 256 matches the EchoSuppressor limit and is far longer than any
// legitimate "exit" line.
const maxInputLineLen = 256

type inputLine struct {
	data []byte
}

func (l *inputLine) appendByte(ch byte) {
	if len(l.data) >= maxInputLineLen {
		return
	}
	l.data = append(l.data, ch)
}

func (l *inputLine) append(data []byte) {
	if room := maxInputLineLen - len(l.data); room > 0 {
		l.data = append(l.data, data[:min(len(data), room)]...)
	}
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
