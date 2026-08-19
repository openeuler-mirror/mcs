package console

import "sync"

// EchoSuppressor tracks recently sent interactive input and filters matching
// RTOS echo from output chunks. It is accessed from both the stdin copier
// goroutine (Track) and the stdout copier goroutine (Suppress), so all
// operations are guarded by a mutex.
type EchoSuppressor struct {
	mu      sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tracked)
}

// Suppress removes output bytes that match the tracked input prefix.
func (s *EchoSuppressor) Suppress(data []byte) EchoSuppressionResult {
	if s == nil {
		return EchoSuppressionResult{Data: data}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tracked) == 0 {
		return EchoSuppressionResult{Data: data}
	}
	if len(data) == 0 {
		return EchoSuppressionResult{Data: data}
	}
	if s.cursor == 0 && data[0] != s.tracked[0] {
		s.tracked = s.tracked[:0]
		return EchoSuppressionResult{Data: data}
	}
	// If a previous chunk partially matched (cursor > 0) and this chunk's
	// first byte breaks the match, the bytes swallowed in the prior chunk
	// are permanently lost. Signal LostSync so the consumer can log it.
	if s.cursor > 0 && s.cursor < len(s.tracked) && data[0] != s.tracked[s.cursor] {
		result := EchoSuppressionResult{
			Data:          data,
			LostSync:      true,
			Position:      s.cursor,
			Got:           data[0],
			Expected:      s.tracked[s.cursor],
			ExpectedValid: true,
		}
		s.tracked = s.tracked[:0]
		s.cursor = 0
		return result
	}

	result := EchoSuppressionResult{Data: make([]byte, 0, len(data))}
	for _, ch := range data {
		if s.cursor < len(s.tracked) && ch == s.tracked[s.cursor] {
			s.cursor++
			result.Suppressed++
			// Reset immediately when fully consumed so trailing non-echo
			// bytes in the same chunk pass through without false LostSync.
			if s.cursor >= len(s.tracked) {
				s.tracked = s.tracked[:0]
				s.cursor = 0
			}
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
