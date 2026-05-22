package console

import (
	"bytes"
	"testing"
)

func TestTTYCtrlCEmitsInterruptWithoutWriting(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true})

	actions := interpreter.Interpret([]byte{0x03})

	assertActions(t, actions, []Action{
		{Kind: ActionEmitEvent, Event: EventInterrupt, StopMode: ActionStopClose},
	})
}

func TestTTYDetachSequenceIsSwallowedAndEmitsDetach(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true})

	first := interpreter.Interpret([]byte{16})
	if len(first) != 0 {
		t.Fatalf("first detach byte actions = %+v, want none", first)
	}

	second := interpreter.Interpret([]byte{17})
	assertActions(t, second, []Action{
		{Kind: ActionEmitEvent, Event: EventDetach, StopMode: ActionStopPreserve},
	})
}

func TestTTYCustomDetachTakesPriorityOverInterrupt(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true, DetachKeys: "ctrl-c"})

	actions := interpreter.Interpret([]byte{0x03})

	assertActions(t, actions, []Action{
		{Kind: ActionEmitEvent, Event: EventDetach, StopMode: ActionStopPreserve},
	})
}

func TestTTYCustomDetachSupportsMultiKeySequence(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true, DetachKeys: "ctrl-a,ctrl-b,ctrl-c"})

	first := interpreter.Interpret([]byte{0x01})
	if len(first) != 0 {
		t.Fatalf("first detach byte actions = %+v, want none", first)
	}
	second := interpreter.Interpret([]byte{0x02})
	if len(second) != 0 {
		t.Fatalf("second detach byte actions = %+v, want none", second)
	}

	third := interpreter.Interpret([]byte{0x03})
	assertActions(t, third, []Action{
		{Kind: ActionEmitEvent, Event: EventDetach, StopMode: ActionStopPreserve},
	})
}

func TestTTYPartialDetachFlushesBufferedBytesOnce(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true})

	_ = interpreter.Interpret([]byte{16})
	actions := interpreter.Interpret([]byte{'x'})

	assertActions(t, actions, []Action{
		{Kind: ActionWriteTTY, Data: []byte{16}},
		{Kind: ActionLocalEcho, Data: []byte{'x'}},
		{Kind: ActionTrackEcho, Data: []byte{'x'}},
		{Kind: ActionWriteTTY, Data: []byte{'x'}},
	})
}

func TestTTYPartialDetachReplaysInterruptSemantics(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true})

	_ = interpreter.Interpret([]byte{16})
	actions := interpreter.Interpret([]byte{0x03})

	assertActions(t, actions, []Action{
		{Kind: ActionWriteTTY, Data: []byte{16}},
		{Kind: ActionEmitEvent, Event: EventInterrupt, StopMode: ActionStopClose},
	})
}

func TestTTYDetachMismatchKeepsOverlappingPrefix(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true})

	_ = interpreter.Interpret([]byte{16})
	actions := interpreter.Interpret([]byte{16})
	assertActions(t, actions, []Action{
		{Kind: ActionWriteTTY, Data: []byte{16}},
	})

	next := interpreter.Interpret([]byte{17})
	assertActions(t, next, []Action{
		{Kind: ActionEmitEvent, Event: EventDetach, StopMode: ActionStopPreserve},
	})
}

func TestTTYExitEmitsExitBeforeLineEnding(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true})

	actions := interpreter.Interpret([]byte("exit\r"))

	assertActions(t, actions, []Action{
		{Kind: ActionLocalEcho, Data: []byte{'e'}},
		{Kind: ActionTrackEcho, Data: []byte{'e'}},
		{Kind: ActionWriteTTY, Data: []byte{'e'}},
		{Kind: ActionLocalEcho, Data: []byte{'x'}},
		{Kind: ActionTrackEcho, Data: []byte{'x'}},
		{Kind: ActionWriteTTY, Data: []byte{'x'}},
		{Kind: ActionLocalEcho, Data: []byte{'i'}},
		{Kind: ActionTrackEcho, Data: []byte{'i'}},
		{Kind: ActionWriteTTY, Data: []byte{'i'}},
		{Kind: ActionLocalEcho, Data: []byte{'t'}},
		{Kind: ActionTrackEcho, Data: []byte{'t'}},
		{Kind: ActionWriteTTY, Data: []byte{'t'}},
		{Kind: ActionWriteStdout, Data: []byte{'\r', '\n'}},
		{Kind: ActionEmitEvent, Event: EventExitCommand, StopMode: ActionStopClose},
	})
}

func TestTTYCRLFSubmitsOneLineEnding(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{Terminal: true})

	actions := interpreter.Interpret([]byte("help\r\n"))

	var writes [][]byte
	for _, action := range actions {
		if action.Kind == ActionWriteTTY {
			writes = append(writes, action.Data)
		}
	}
	if got, want := bytes.Join(writes, nil), []byte("help\r\n"); !bytes.Equal(got, want) {
		t.Fatalf("TTY writes = %q, want %q", got, want)
	}
}

func TestNonTTYFragmentedExitStopsBeforeWritingExitFragment(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{})

	first := interpreter.Interpret([]byte("ex"))
	assertActions(t, first, []Action{
		{Kind: ActionTrackEcho, Data: []byte("ex")},
		{Kind: ActionWriteTTY, Data: []byte("ex")},
	})

	second := interpreter.Interpret([]byte("it\n"))
	assertActions(t, second, []Action{
		{Kind: ActionEmitEvent, Event: EventExitCommand, StopMode: ActionStopClose},
	})
}

func TestNonTTYCRSubmitsExitCommand(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{})

	actions := interpreter.Interpret([]byte("exit\r"))

	assertActions(t, actions, []Action{
		{Kind: ActionEmitEvent, Event: EventExitCommand, StopMode: ActionStopClose},
	})
}

func TestNonTTYFragmentedCRExitStopsBeforeWritingExitFragment(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{})

	first := interpreter.Interpret([]byte("ex"))
	assertActions(t, first, []Action{
		{Kind: ActionTrackEcho, Data: []byte("ex")},
		{Kind: ActionWriteTTY, Data: []byte("ex")},
	})

	second := interpreter.Interpret([]byte("it\r"))
	assertActions(t, second, []Action{
		{Kind: ActionEmitEvent, Event: EventExitCommand, StopMode: ActionStopClose},
	})
}

func TestNonTTYCtrlCRemainsInputByte(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{})

	actions := interpreter.Interpret([]byte{0x03, '\n'})

	assertActions(t, actions, []Action{
		{Kind: ActionWriteTTY, Data: []byte{0x03, '\r', '\n'}},
	})
}

func TestInputActionsOwnWritableData(t *testing.T) {
	interpreter := NewInputInterpreter(InputConfig{})
	input := []byte("abc")

	actions := interpreter.Interpret(input)
	input[0] = 'z'

	assertActions(t, actions, []Action{
		{Kind: ActionTrackEcho, Data: []byte("abc")},
		{Kind: ActionWriteTTY, Data: []byte("abc")},
	})
}

func TestParseDetachKeysSupportsGenericControlKeys(t *testing.T) {
	got := ParseDetachKeys("ctrl-a, Ctrl-Z")
	want := []byte{1, 26}
	if !bytes.Equal(got, want) {
		t.Fatalf("ParseDetachKeys returned %v, want %v", got, want)
	}
}

func TestParseDetachKeysSupportsSymbolControlKeys(t *testing.T) {
	got := ParseDetachKeys(`ctrl-@,ctrl-[,ctrl-\,ctrl-],ctrl-^,ctrl-_,ctrl-?`)
	want := []byte{0, 27, 28, 29, 30, 31, 127}
	if !bytes.Equal(got, want) {
		t.Fatalf("ParseDetachKeys returned %v, want %v", got, want)
	}
}

func TestParseDetachKeysRejectsPartialInvalidSequence(t *testing.T) {
	if got := ParseDetachKeys("ctrl-p,bad,ctrl-q"); got != nil {
		t.Fatalf("ParseDetachKeys returned %v, want nil for partially invalid sequence", got)
	}
	if got := ParseDetachKeys("ctrl-p,"); got != nil {
		t.Fatalf("ParseDetachKeys returned %v, want nil for trailing empty key", got)
	}
}

func TestEffectiveDetachKeysPolicy(t *testing.T) {
	if got := effectiveDetachKeys(InputConfig{Terminal: false}); got != nil {
		t.Fatalf("non-terminal detach keys = %v, want nil", got)
	}
	if got := effectiveDetachKeys(InputConfig{Terminal: true, ExecMode: true, DetachKeys: "ctrl-a"}); got != nil {
		t.Fatalf("exec detach keys = %v, want nil", got)
	}
	if got, want := effectiveDetachKeys(InputConfig{Terminal: true}), []byte{16, 17}; !bytes.Equal(got, want) {
		t.Fatalf("default detach keys = %v, want %v", got, want)
	}
	if got, want := effectiveDetachKeys(InputConfig{Terminal: true, DetachKeys: "ctrl-a"}), []byte{1}; !bytes.Equal(got, want) {
		t.Fatalf("custom detach keys = %v, want %v", got, want)
	}
}

func assertActions(t *testing.T, got, want []Action) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("actions length = %d, want %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for idx := range got {
		if got[idx].Kind != want[idx].Kind ||
			got[idx].Event != want[idx].Event ||
			got[idx].EffectiveStopMode() != want[idx].EffectiveStopMode() ||
			!bytes.Equal(got[idx].Data, want[idx].Data) {
			t.Fatalf("action[%d] = %+v, want %+v", idx, got[idx], want[idx])
		}
	}
}
