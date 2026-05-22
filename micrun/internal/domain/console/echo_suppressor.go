package console

// EchoSuppressor tracks recently sent interactive input and filters matching
// RTOS echo from output chunks.
type EchoSuppressor struct {
	tracked []byte
	cursor  int
	limit   int
}

// EchoSuppressionResult describes one output filtering pass.
type EchoSuppressionResult struct {
	Data          []byte
	Suppressed    int
	LostSync      bool
	Position      int
	Expected      byte
	ExpectedValid bool
	Got           byte
}

// NewEchoSuppressor creates a bounded echo suppression window.
func NewEchoSuppressor(limit int) *EchoSuppressor {
	if limit <= 0 {
		limit = 256
	}
	return &EchoSuppressor{
		tracked: make([]byte, 0, limit),
		limit:   limit,
	}
}

// Track records one byte sent to the RTOS console.
func (s *EchoSuppressor) Track(ch byte) {
	if s == nil {
		return
	}
	s.tracked = append(s.tracked, ch)
	if len(s.tracked) <= s.limit {
		return
	}

	trim := len(s.tracked) - s.limit
	copy(s.tracked, s.tracked[trim:])
	s.tracked = s.tracked[:s.limit]
	if s.cursor >= trim {
		s.cursor -= trim
	} else {
		s.cursor = 0
	}
}

// Len returns the number of bytes currently tracked for echo suppression.
func (s *EchoSuppressor) Len() int {
	if s == nil {
		return 0
	}
	return len(s.tracked)
}

// Suppress removes output bytes that match the tracked input prefix.
func (s *EchoSuppressor) Suppress(data []byte) EchoSuppressionResult {
	if s == nil || len(s.tracked) == 0 {
		return EchoSuppressionResult{Data: data}
	}
	if len(data) == 0 {
		return EchoSuppressionResult{Data: data}
	}
	if s.cursor == 0 && data[0] != s.tracked[0] {
		s.tracked = s.tracked[:0]
		return EchoSuppressionResult{Data: data}
	}

	result := EchoSuppressionResult{Data: make([]byte, 0, len(data))}
	for _, ch := range data {
		if s.cursor < len(s.tracked) && ch == s.tracked[s.cursor] {
			s.cursor++
			result.Suppressed++
			continue
		}

		if result.Suppressed > 0 && !result.LostSync {
			result.LostSync = true
			result.Position = s.cursor
			result.Got = ch
			if s.cursor < len(s.tracked) {
				result.Expected = s.tracked[s.cursor]
				result.ExpectedValid = true
			}
		}
		result.Data = append(result.Data, ch)
		s.cursor = 0
		s.tracked = s.tracked[:0]
	}

	if s.cursor >= len(s.tracked) {
		s.tracked = s.tracked[:0]
		s.cursor = 0
	}
	return result
}
