package console

// EventKind is a semantic console event emitted from input bytes.
type EventKind int

const (
	EventNone EventKind = iota
	EventExitCommand
	EventDetach
	EventInterrupt
)

// ActionKind tells the IO adapter how to execute an interpreted action.
type ActionKind int

const (
	ActionWriteTTY ActionKind = iota
	ActionWriteStdout
	ActionLocalEcho
	ActionTrackEcho
	ActionEmitEvent
)

// ActionStopMode describes how the adapter should stop after an action.
type ActionStopMode int

const (
	ActionStopNone ActionStopMode = iota
	ActionStopClose
	ActionStopPreserve
)

// Action is an instruction produced by InputInterpreter.
type Action struct {
	Kind     ActionKind
	Data     []byte
	Event    EventKind
	StopMode ActionStopMode
}

func writeTTY(data []byte) Action {
	return Action{Kind: ActionWriteTTY, Data: cloneActionData(data)}
}

func writeStdout(data []byte) Action {
	return Action{Kind: ActionWriteStdout, Data: cloneActionData(data)}
}

func localEcho(data []byte) Action {
	return Action{Kind: ActionLocalEcho, Data: cloneActionData(data)}
}

func trackEcho(data []byte) Action {
	return Action{Kind: ActionTrackEcho, Data: cloneActionData(data)}
}

func trackEchoByte(ch byte) Action {
	return trackEcho([]byte{ch})
}

func emitEvent(event EventKind) Action {
	return Action{
		Kind:     ActionEmitEvent,
		Event:    event,
		StopMode: eventStopMode(event),
	}
}

func eventStopMode(event EventKind) ActionStopMode {
	switch event {
	case EventExitCommand, EventInterrupt:
		return ActionStopClose
	case EventDetach:
		return ActionStopPreserve
	default:
		return ActionStopNone
	}
}

// EffectiveStopMode returns how the adapter should stop after this action.
func (a Action) EffectiveStopMode() ActionStopMode {
	if a.StopMode != ActionStopNone {
		return a.StopMode
	}
	if a.Kind == ActionEmitEvent {
		return eventStopMode(a.Event)
	}
	return ActionStopNone
}

func cloneActionData(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return append([]byte(nil), data...)
}

func printableBytes(data []byte) []byte {
	var printable []byte
	for _, ch := range data {
		if ch >= 32 && ch <= 126 {
			printable = append(printable, ch)
		}
	}
	return printable
}

func stops(actions []Action) bool {
	for _, action := range actions {
		if action.EffectiveStopMode() != ActionStopNone {
			return true
		}
	}
	return false
}

type actionBuffer struct {
	actions []Action
}

func newActionBuffer(capacity int) actionBuffer {
	return actionBuffer{actions: make([]Action, 0, capacity)}
}

func (b *actionBuffer) append(next ...Action) bool {
	b.actions = append(b.actions, next...)
	return stops(next)
}

func (b *actionBuffer) appendAll(next []Action) bool {
	b.actions = append(b.actions, next...)
	return stops(next)
}

func (b *actionBuffer) appendPrintableTrack(data []byte) bool {
	printable := printableBytes(data)
	if len(printable) == 0 {
		return false
	}
	return b.append(trackEcho(printable))
}

func (b actionBuffer) list() []Action {
	return b.actions
}
