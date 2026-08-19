package console

import "testing"

func TestInputLineRecognizesTrimmedCaseInsensitiveExit(t *testing.T) {
	var line inputLine
	line.append([]byte("  ExIt \r"))

	if !line.isExitCommand() {
		t.Fatal("expected trimmed case-insensitive exit command")
	}
}

func TestInputLineRejectsPrefixedExitWord(t *testing.T) {
	var line inputLine
	line.append([]byte("exit now"))

	if line.isExitCommand() {
		t.Fatal("expected non-exit line to be rejected")
	}
}

func TestInputLineBackspaceAndReset(t *testing.T) {
	var line inputLine
	line.append([]byte("abc"))

	if !line.backspace() {
		t.Fatal("expected backspace to remove a byte")
	}
	if string(line.data) != "ab" {
		t.Fatalf("line after backspace = %q, want ab", line.data)
	}
	line.reset()
	if len(line.data) != 0 {
		t.Fatalf("line after reset = %q, want empty", line.data)
	}
	if line.backspace() {
		t.Fatal("empty line backspace should report false")
	}
}

func TestInputLineBoundedCapacity(t *testing.T) {
	var line inputLine
	// A hostile or chatty stdin may never send a newline: the buffer must
	// stay bounded instead of growing without limit.
	chunk := make([]byte, 4096)
	for i := range chunk {
		chunk[i] = 'a'
	}
	for i := 0; i < 16; i++ {
		line.append(chunk)
		line.appendByte('b')
	}
	if len(line.data) > maxInputLineLen {
		t.Fatalf("line buffer grew to %d bytes, want <= %d", len(line.data), maxInputLineLen)
	}
	if line.isExitCommand() {
		t.Fatal("overlong line must not be recognized as exit command")
	}

	// After a reset the line works again for a real exit command.
	line.reset()
	line.append([]byte("exit"))
	if !line.isExitCommand() {
		t.Fatal("expected exit command after reset")
	}
}
