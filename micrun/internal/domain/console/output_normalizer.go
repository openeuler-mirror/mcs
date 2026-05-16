package console

import "bytes"

// OutputNormalizer applies stateful output normalization across read chunks.
type OutputNormalizer struct {
	config OutputConfig

	pendingCR      bool
	lastLineEnding bool
}

// NewOutputNormalizer creates a stream normalizer.
func NewOutputNormalizer(config OutputConfig) *OutputNormalizer {
	return &OutputNormalizer{config: config}
}

// FilterNUL removes NUL bytes from src and appends the result to dst.
// If filtering is not needed and dst is nil, it returns src directly.
func FilterNUL(dst, src []byte) []byte {
	if len(src) == 0 {
		return dst
	}
	if bytes.IndexByte(src, 0) < 0 {
		if len(dst) == 0 {
			return src
		}
		return append(dst, src...)
	}

	for _, ch := range src {
		if ch != 0 {
			dst = append(dst, ch)
		}
	}
	return dst
}

// CompressLineEndings is a stateless convenience wrapper around OutputNormalizer.
func CompressLineEndings(data []byte) []byte {
	normalizer := NewOutputNormalizer(OutputConfig{CompressLineEndings: true})
	result := normalizer.Normalize(data)
	return append(result, normalizer.Flush()...)
}

// Normalize transforms one output chunk. Consecutive line endings are compressed
// across chunk boundaries when CompressLineEndings is enabled.
func (n *OutputNormalizer) Normalize(data []byte) []byte {
	if !n.config.FilterNUL && !n.config.CompressLineEndings {
		return data
	}

	if n.config.FilterNUL && !n.config.CompressLineEndings && bytes.IndexByte(data, 0) < 0 {
		return data
	}

	if n.config.FilterNUL && n.config.CompressLineEndings {
		if !n.pendingCR && bytes.IndexAny(data, "\r\n\x00") < 0 {
			return data
		}
	}

	if n.config.CompressLineEndings && !n.config.FilterNUL {
		if !n.pendingCR && !bytes.ContainsAny(data, "\r\n") {
			return data
		}
	}

	result := make([]byte, 0, len(data))
	for _, ch := range data {
		if n.config.FilterNUL && ch == 0 {
			continue
		}

		if !n.config.CompressLineEndings {
			result = append(result, ch)
			continue
		}

		result = n.consumeByte(result, ch)
	}
	return result
}

// Flush emits a pending standalone CR if the stream closes before the next byte.
func (n *OutputNormalizer) Flush() []byte {
	if !n.pendingCR {
		return nil
	}
	n.pendingCR = false
	n.lastLineEnding = false
	return []byte{'\r'}
}

func (n *OutputNormalizer) consumeByte(dst []byte, ch byte) []byte {
	if n.pendingCR {
		n.pendingCR = false
		if ch == '\n' {
			return n.appendLineEnding(dst)
		}
		dst = append(dst, '\r')
		n.lastLineEnding = false
	}

	switch ch {
	case '\r':
		n.pendingCR = true
		return dst
	case '\n':
		return n.appendLineEnding(dst)
	default:
		n.lastLineEnding = false
		return append(dst, ch)
	}
}

func (n *OutputNormalizer) appendLineEnding(dst []byte) []byte {
	if n.lastLineEnding {
		return dst
	}
	n.lastLineEnding = true
	return append(dst, '\r', '\n')
}
