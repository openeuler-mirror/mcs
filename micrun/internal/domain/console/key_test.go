package console

import "testing"

func TestKeyClassification(t *testing.T) {
	if !isInterruptKey(0x03) || isInterruptKey('c') {
		t.Fatal("interrupt key classification failed")
	}
	if !isBackspaceKey(0x08) || !isBackspaceKey(0x7f) || isBackspaceKey('x') {
		t.Fatal("backspace key classification failed")
	}
}
