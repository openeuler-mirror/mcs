package attach

import (
	"context"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
)

type ioEventPump struct {
	ctx    context.Context
	events ports.IOEventSubscriber
}

func newIOEventPump(ctx context.Context, events ports.IOEventSubscriber) ioEventPump {
	return ioEventPump{
		ctx:    contextx.OrBackground(ctx),
		events: events,
	}
}

func (p ioEventPump) next() (ports.IOEvent, bool) {
	select {
	case <-p.ctx.Done():
		return ports.IOEvent{}, false
	case event, ok := <-p.events:
		if !ok {
			return ports.IOEvent{}, false
		}
		return event, true
	}
}
