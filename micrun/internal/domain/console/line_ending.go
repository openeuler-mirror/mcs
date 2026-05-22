package console

type inputLineEnding struct {
	prevWasCR bool
}

type lineEndingResult struct {
	lineEnd bool
	skip    bool
}

func (e *inputLineEnding) consume(ch byte) lineEndingResult {
	switch ch {
	case '\r':
		e.prevWasCR = true
		return lineEndingResult{lineEnd: true}
	case '\n':
		if e.prevWasCR {
			e.prevWasCR = false
			return lineEndingResult{skip: true}
		}
		return lineEndingResult{lineEnd: true}
	default:
		e.prevWasCR = false
		return lineEndingResult{}
	}
}

func (e *inputLineEnding) reset() {
	e.prevWasCR = false
}

func convertLFToCRLF(data []byte) []byte {
	needsConversion := false
	for idx, ch := range data {
		if ch == '\n' && (idx == 0 || data[idx-1] != '\r') {
			needsConversion = true
			break
		}
	}
	if !needsConversion {
		return data
	}

	result := make([]byte, 0, len(data)*2)
	for idx, ch := range data {
		if ch == '\n' && (idx == 0 || data[idx-1] != '\r') {
			result = append(result, '\r', '\n')
			continue
		}
		result = append(result, ch)
	}
	return result
}
