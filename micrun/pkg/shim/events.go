package shim

import (
	"context"
	log "micrun/logger"
	"os"
	"time"

	"github.com/containerd/containerd/api/events"
	cdruntime "github.com/containerd/containerd/runtime"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// TODO: need a more reasonable timeout for event sending
	timeOut              = 5 * time.Second
	ttrpcAddrEnv         = "TTRPC_ADDRESS"
	contdShimEnvShedCore = "SCHED_CORE"
)

// exitEvent represents a container exitEvent event.
type exitEvent struct {
	ts     time.Time
	cid    string
	execid string
	pid    uint32
	status int
}

// eventsForwarder handles forwarding events from the shim to containerd.
type eventsForwarder struct {
	service   *shimService
	context   context.Context
	publisher shimv2.Publisher
}

// newEventsForwarder creates a new events forwarder
func (s *shimService) newEventsForwarder(ctx context.Context, publisher shimv2.Publisher) *eventsForwarder {
	return &eventsForwarder{
		service:   s,
		context:   ctx,
		publisher: publisher,
	}
}

func getTopic(e any) string {
	log.Debugf("topic event: %v", e)
	switch e.(type) {
	case *events.TaskCreate:
		return cdruntime.TaskCreateEventTopic
	case *events.TaskStart:
		return cdruntime.TaskStartEventTopic
	case *events.TaskOOM:
		return cdruntime.TaskOOMEventTopic
	case *events.TaskExit:
		return cdruntime.TaskExitEventTopic
	case *events.TaskDelete:
		return cdruntime.TaskDeleteEventTopic
	case *events.TaskExecAdded:
		return cdruntime.TaskExecAddedEventTopic
	case *events.TaskExecStarted:
		return cdruntime.TaskExecStartedEventTopic
	case *events.TaskPaused:
		return cdruntime.TaskPausedEventTopic
	case *events.TaskResumed:
		return cdruntime.TaskResumedEventTopic
	case *events.TaskCheckpointed:
		return cdruntime.TaskCheckpointedEventTopic
	default:
		log.Warnf("no topic for event type: %v", e)
	}
	return cdruntime.TaskUnknownTopic
}

// forward listens for events and publishes them to containerd/isulad
func (ef *eventsForwarder) forward() {
	for e := range ef.service.events {
		topic := getTopic(e)
		if topic == cdruntime.TaskUnknownTopic {
			log.Warnf("unknown event type, skipping: %v", e)
			continue
		}

		// Publish the event to containerd
		ctx, cancel := context.WithTimeout(ef.context, timeOut)
		if err := ef.publisher.Publish(ctx, topic, e); err != nil {
			log.Errorf("failed to publish event topic=%s: %v", topic, err)
		} else {
			log.Debugf("Successfully forwarded event topic=%s", topic)
		}
		cancel()
	}
}

// listenAndReportExits listens for exit events on a channel and reports them.
func (s *shimService) listenAndReportExits() {
	for e := range s.ec {
		s.reportExit(e)
		// After reporting main container exit, trigger shim shutdown
		// This allows containerd to automatically clean up the task
		//
		// However, if the container was killed via Kill API, we should NOT
		// trigger shim auto-exit. The shim should continue running to serve
		// subsequent API requests (like Delete).
		if e.execid == "" {
			if s.killedByAPI {
				log.Infof("[SHIM] Main container %s exited via Kill API, keeping shim running for cleanup", e.cid)
				// Reset the flag for future exits
				s.killedByAPI = false
				// Continue listening for events
				continue
			}
			log.Infof("[SHIM] Main container %s exited naturally, shutting down shim", e.cid)
			// Call shutdown in a separate goroutine to avoid blocking
			go func() {
				if s.ss != nil {
					s.ss()
				}
				// Give time for the shutdown signal to be processed
				time.Sleep(100 * time.Millisecond)
				os.Exit(0)
			}()
			// Exit the loop after main container exit
			return
		}
	}
}

// reportExit sends a TaskExit event to containerd.
func (s *shimService) reportExit(e exitEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := e.execid
	if id == "" {
		id = e.cid
	}
	s.send(&events.TaskExit{
		ContainerID: e.cid,
		ID:          id,
		Pid:         e.pid,
		ExitStatus:  uint32(e.status),
		ExitedAt:    timestamppb.New(e.ts),
	})
}

// send places an event on the events channel for forwarding.
func (s *shimService) send(ev any) {
	if s.events != nil {
		s.events <- ev
	}
}
