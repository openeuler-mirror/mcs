package console

func isInterruptKey(ch byte) bool {
	return ch == 0x03
}

func isBackspaceKey(ch byte) bool {
	return ch == 0x08 || ch == 0x7f
}
