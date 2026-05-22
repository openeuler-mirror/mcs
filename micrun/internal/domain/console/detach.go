package console

import (
	"bytes"
	"strings"
)

const (
	defaultDetachCtrlP = 16
	defaultDetachCtrlQ = 17
)

type detachSequence struct {
	keys    []byte
	buf     []byte
	pending bool
}

type detachResult struct {
	consumed  bool
	matched   bool
	flushed   []byte
	replay    byte
	hasReplay bool
}

func newDetachSequence(config InputConfig) detachSequence {
	keys := effectiveDetachKeys(config)
	return detachSequence{
		keys: keys,
		buf:  make([]byte, 0, len(keys)),
	}
}

func (d *detachSequence) consume(ch byte) detachResult {
	if len(d.keys) == 0 {
		return detachResult{}
	}

	if !d.pending {
		if ch != d.keys[0] {
			return detachResult{}
		}

		d.pending = true
		d.buf = append(d.buf[:0], ch)
		if len(d.keys) == 1 {
			d.reset()
			return detachResult{consumed: true, matched: true}
		}
		return detachResult{consumed: true}
	}

	d.buf = append(d.buf, ch)
	if bytes.Equal(d.buf, d.keys) {
		d.reset()
		return detachResult{consumed: true, matched: true}
	}
	if len(d.buf) < len(d.keys) && bytes.HasPrefix(d.keys, d.buf) {
		return detachResult{consumed: true}
	}

	return d.flushMismatch()
}

func (d *detachSequence) flushMismatch() detachResult {
	keep := longestDetachPrefixSuffix(d.buf, d.keys)
	flushLen := len(d.buf) - keep
	if keep == 0 {
		replay := d.buf[len(d.buf)-1]
		flushed := append([]byte(nil), d.buf[:len(d.buf)-1]...)
		d.reset()
		return detachResult{consumed: true, flushed: flushed, replay: replay, hasReplay: true}
	}

	flushed := append([]byte(nil), d.buf[:flushLen]...)
	copy(d.buf, d.buf[flushLen:])
	d.buf = d.buf[:keep]
	d.pending = true
	return detachResult{consumed: true, flushed: flushed}
}

func longestDetachPrefixSuffix(buf, keys []byte) int {
	maxLen := min(len(buf), len(keys)-1)
	for size := maxLen; size > 0; size-- {
		if bytes.Equal(buf[len(buf)-size:], keys[:size]) {
			return size
		}
	}
	return 0
}

func (d *detachSequence) reset() {
	d.buf = d.buf[:0]
	d.pending = false
}

func effectiveDetachKeys(config InputConfig) []byte {
	if detachKeysDisabled(config) {
		return nil
	}
	if config.DetachKeys == "" {
		return defaultDetachKeys()
	}
	return ParseDetachKeys(config.DetachKeys)
}

func detachKeysDisabled(config InputConfig) bool {
	return !config.Terminal || config.ExecMode
}

func defaultDetachKeys() []byte {
	return []byte{defaultDetachCtrlP, defaultDetachCtrlQ}
}

// ParseDetachKeys parses a detach sequence such as "ctrl-p,ctrl-q".
func ParseDetachKeys(keys string) []byte {
	parts := strings.Split(keys, ",")
	result := make([]byte, 0, len(parts))
	for _, part := range parts {
		b, ok := parseControlKey(part)
		if !ok {
			return nil
		}
		result = append(result, b)
	}
	return result
}

func parseControlKey(key string) (byte, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	const prefix = "ctrl-"
	if !strings.HasPrefix(key, prefix) || len(key) != len(prefix)+1 {
		return 0, false
	}
	ch := key[len(prefix)]
	if ch < 'a' || ch > 'z' {
		return parseSymbolControlKey(ch)
	}
	return ch - 'a' + 1, true
}

func parseSymbolControlKey(ch byte) (byte, bool) {
	switch ch {
	case '@':
		return 0, true
	case '[':
		return 27, true
	case '\\':
		return 28, true
	case ']':
		return 29, true
	case '^':
		return 30, true
	case '_':
		return 31, true
	case '?':
		return 127, true
	default:
		return 0, false
	}
}
