package attach

import (
	"context"
	"time"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) CloseIO(ctx context.Context, runtime ports.TaskAttachRuntime, taskHandle ports.Task, closeStdin bool) error {
	if err := requireTask(taskHandle); err != nil {
		return err
	}
	ctx = contextx.OrBackground(ctx)

	var wasAttached bool
	withTaskLock(runtime, func() {
		wasAttached = taskHandle.SetAttached(false)
	})

	if wasAttached {
		log.Infof("[ATTACH] Container %s detached, isAttached cleared", taskHandle.ID())
	}

	if !closeStdin {
		return nil
	}
	return closeTaskStdin(ctx, runtime, taskHandle)
}

func (s *Service) handleIOEvents(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	taskHandle ports.Task,
	events ports.IOEventSubscriber,
	stream ports.IOEventStream,
) {
	if err := requireTask(taskHandle); err != nil {
		log.Warnf("[EVENTS] Start IO event handler: %v", err)
		return
	}

	log.Debugf("[EVENTS] Starting IO event handler for %s", taskHandle.ID())
	if events == nil {
		log.Warnf("[EVENTS] IO event handler aborted for %s: no event stream", taskHandle.ID())
		return
	}
	pump := newIOEventPump(ctx, events)

	for {
		event, ok := pump.next()
		if !ok {
			return
		}
		// Events queued on the previous bus keep draining after a session
		// restart closed it. Acting on a stale detach/stop event here would
		// tear down the freshly restarted session, so stop consuming as soon
		// as this subscription no longer tracks the active bus.
		if stream != nil && !stream.Current() {
			log.Infof("[EVENTS] Dropping stale IO event for %s from a pre-restart event bus", taskHandle.ID())
			return
		}
		s.handleIOEvent(runtime, taskHandle, event, stream)
	}
}

func (s *Service) handleIOEvent(runtime ports.TaskAttachRuntime, taskHandle ports.Task, event ports.IOEvent, stream ports.IOEventStream) {
	if err := requireTask(taskHandle); err != nil {
		log.Warnf("[EVENTS] Handle IO event: %v", err)
		return
	}
	taskID := taskHandle.ID()
	policy, ok := s.resolveIOEventPolicy(taskID, event)
	if !ok {
		if event.ContainerID == taskID {
			log.Tracef("[EVENTS] Ignoring unmatched IO event for task=%s type=%v", taskID, event.Type)
		}
		return
	}
	policy.handler(newIOEventContext(s, runtime, taskHandle, event, policy.plan, stream))
}

type ioEventContext struct {
	service *Service
	runtime ports.TaskAttachRuntime
	task    ports.Task
	event   ports.IOEvent
	plan    ioEventPlan
	stream  ports.IOEventStream
}

// staleUnderLock reports whether the event's stream no longer tracks the
// session's active bus. It is called twice on the detach path: once
// unlocked as a fast pre-check, and once inside the task-locked mutation
// as the authoritative re-check — an EnsureAttach restart swaps the bus
// without the detach handler holding the task lock, so only the in-lock
// re-check can stop a detach queued on the old bus from tearing down the
// freshly restarted session.
func (ctx ioEventContext) staleUnderLock() bool {
	return ctx.stream != nil && !ctx.stream.Current()
}

func newIOEventContext(
	service *Service,
	runtime ports.TaskAttachRuntime,
	task ports.Task,
	event ports.IOEvent,
	plan ioEventPlan,
	stream ports.IOEventStream,
) ioEventContext {
	return ioEventContext{
		service: service,
		runtime: runtime,
		task:    task,
		event:   event,
		plan:    plan,
		stream:  stream,
	}
}

func handleIOEventStopTask(ctx ioEventContext) {
	reason, ok := ctx.plan.stopReasonValue()
	if !ok {
		log.Warnf("[EVENTS] Unexpected stop event policy for %s without stop reason", ctx.task.ID())
		return
	}
	ctx.service.stopFromIOEvent(ctx.runtime, ctx.task, reason)
}

func handleIOEventStdinClosed(ctx ioEventContext) {
	log.Infof("[EVENTS] Stdin closed for %s, stopping IO session (container continues)", ctx.task.ID())
	manager := applyDetachedIOEventMutation(ctx.runtime, ctx.task, "stdin closed")
	stopLoadedIOManager(manager)
}

func handleIOEventDetach(ctx ioEventContext) {
	log.Infof("[EVENTS] Detach detected for %s, preserving FIFOs for reattach", ctx.task.ID())
	if ctx.staleUnderLock() {
		log.Infof("[EVENTS] Dropping stale detach for %s: event bus superseded by a session restart", ctx.task.ID())
		return
	}
	var manager ports.IOManager
	dropped := false
	applyIOEventTaskMutation(ctx.runtime, ctx.task, "detach", func() {
		if ctx.staleUnderLock() {
			dropped = true
			return
		}
		ctx.task.SetAttached(false)
		manager, _ = loadIOManager(ctx.task)
	})
	if dropped {
		log.Infof("[EVENTS] Dropping stale detach for %s: event bus superseded by a session restart", ctx.task.ID())
		return
	}
	detachLoadedIOManager(manager)
}

func handleIOEventReportError(ctx ioEventContext) {
	log.Warnf("[EVENTS] IOError event received for %s: %v", ctx.task.ID(), ctx.event.Err)
}

// attachEventApplier is the optional task capability that guards attach
// events against stale cross-type reordering (see shimContainer.
// ApplyAttachEvent). Tasks without it fall back to the raw setter.
type attachEventApplier interface {
	ApplyAttachEvent(attached bool, at time.Time) bool
}

func applyAttachEvent(task ports.Task, attached bool, at time.Time) {
	if ap, ok := task.(attachEventApplier); ok {
		if !ap.ApplyAttachEvent(attached, at) {
			log.Debugf("[ATTACH] dropping stale attach event (%v) for %s", attached, task.ID())
		}
		return
	}
	task.SetAttached(attached)
}

func handleIOEventClientAttached(ctx ioEventContext) {
	withTaskLockIfAvailable(ctx.runtime, func() {
		applyAttachEvent(ctx.task, true, ctx.event.Timestamp)
	})
	log.Infof("[ATTACH] Live client attached for %s", ctx.task.ID())
}

func handleIOEventClientDetached(ctx ioEventContext) {
	withTaskLockIfAvailable(ctx.runtime, func() {
		applyAttachEvent(ctx.task, false, ctx.event.Timestamp)
	})
	log.Infof("[ATTACH] No live stdin writer for %s", ctx.task.ID())
}

func (s *Service) stopFromIOEvent(runtime ports.TaskAttachRuntime, taskHandle ports.Task, reason ioStopReason) {
	var manager ports.IOManager
	var shouldStop bool

	mutateTask := func() {
		alreadyStopped := taskHandle.Status() == task.Status_STOPPED
		if !alreadyStopped {
			taskHandle.SetStatus(task.Status_STOPPED)
			// Preserve Kill pre-writes (SetExitInfo without Status=STOPPED).
			// Overwriting them with an IO-fabricated 0/130 would disagree
			// with the signal the API requested (e.g. 137 for SIGKILL).
			if taskHandle.ExitTime().IsZero() {
				taskHandle.SetExitInfo(reason.exitStatus, s.clockNow())
			}
			manager, _ = loadIOManager(taskHandle)
			taskHandle.SetIOManager(nil)
		}

		shouldStop = !alreadyStopped
	}

	applyIOEventTaskMutation(runtime, taskHandle, reason.name, mutateTask)
	if !shouldStop {
		return
	}

	log.Infof("[EVENTS] Stopping %s from IO %s with exit status %d", taskHandle.ID(), reason.name, reason.exitStatus)
	taskHandle.IOExit()
	stopLoadedIOManager(manager)
}

type ioEventHandler func(ioEventContext)

func (s *Service) handleStdinClosed(taskHandle ports.Task) {
	handleIOEventStdinClosed(newIOEventContext(s, nil, taskHandle, ports.IOEvent{}, ioEventPlan{}, nil))
}

func (s *Service) handleDetach(taskHandle ports.Task) {
	handleIOEventDetach(newIOEventContext(s, nil, taskHandle, ports.IOEvent{}, ioEventPlan{}, nil))
}

func applyDetachedIOEventMutation(runtime ports.TaskAttachRuntime, taskHandle ports.Task, action string) ports.IOManager {
	var manager ports.IOManager
	applyIOEventTaskMutation(runtime, taskHandle, action, func() {
		taskHandle.SetAttached(false)
		manager, _ = loadIOManager(taskHandle)
	})
	return manager
}

func applyIOEventTaskMutation(runtime ports.TaskAttachRuntime, taskHandle ports.Task, action string, mutation func()) {
	if withTaskLockIfAvailable(runtime, mutation) {
		return
	}
	log.Warnf("[EVENTS] %s event for %s without runtime lock", action, taskHandle.ID())
	mutation()
}

func (s *Service) resolveIOEventPolicy(taskID string, event ports.IOEvent) (ioEventPolicy, bool) {
	if s == nil {
		return ioEventPolicy{}, false
	}
	return s.eventProfile.resolveTaskIOEvent(taskID, event)
}
