package ports

import (
	"context"
	"io"
	"time"
)

// IOEventType describes high-level IO lifecycle signals that the application
// layer reacts to, without depending on a concrete transport implementation.
type IOEventType int

const (
	IOEventExitCommand IOEventType = iota
	IOEventError
	IOEventTTYReady
	IOEventStdinClosed
	IOEventDetach
	IOEventInterrupt
)

// IOEvent carries an IO-side notification to the application layer.
type IOEvent struct {
	Type        IOEventType
	ContainerID string
	Err         error
	Timestamp   time.Time
}

// IOEventSubscriber receives IO lifecycle events selected by an IOEventStream.
type IOEventSubscriber <-chan IOEvent

// IOEventStream abstracts subscription to a merged IO lifecycle event stream.
type IOEventStream interface {
	SubscribeMany(eventTypes ...IOEventType) IOEventSubscriber
}

// IOSessionConfig describes the stdio wiring needed to bootstrap a task IO
// session independent of the concrete adapter implementation.
type IOSessionConfig struct {
	ContainerID string
	StdinFIFO   string
	StdoutFIFO  string
	StderrFIFO  string
	TTYIn       io.WriteCloser
	TTYOut      io.Reader
	TTYErr      io.Reader
	Terminal    bool
	FilterNUL   bool
	ExecMode    bool
	DetachKeys  string
}

// IOSessionFactory constructs adapter-backed IO sessions and their event
// streams for the application layer.
type IOSessionFactory interface {
	NewSession(ctx context.Context, config IOSessionConfig) (IOManager, IOEventStream, error)
	IsValidFIFOPath(path string) bool
	GenerateFIFOPath(namespace, containerID, stream string) string
}
