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
