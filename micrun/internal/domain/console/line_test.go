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
