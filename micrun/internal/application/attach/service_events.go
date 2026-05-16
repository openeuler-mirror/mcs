package attach

import (
	"context"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) CloseIO(ctx context.Context, taskHandle ports.Task, closeStdin bool) error {
	if err := requireTask(taskHandle); err != nil {
		return err
	}
	ctx = contextx.OrBackground(ctx)

	wasAttached := taskHandle.SetAttached(false)

	if wasAttached {
		log.Infof("[ATTACH] Container %s detached, isAttached cleared", taskHandle.ID())
	}

	if !closeStdin {
		return nil
	}
	return closeTaskStdin(ctx, taskHandle)
}

func (s *Service) handleIOEvents(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	taskHandle ports.Task,
	events ports.IOEventSubscriber,
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
		s.handleIOEvent(runtime, taskHandle, event)
	}
}

func (s *Service) handleIOEvent(runtime ports.TaskAttachRuntime, taskHandle ports.Task, event ports.IOEvent) {
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
	policy.handler(newIOEventContext(s, runtime, taskHandle, event, policy.plan))
}

type ioEventContext struct {
	service *Service
	runtime ports.TaskAttachRuntime
	task    ports.Task
	event   ports.IOEvent
	plan    ioEventPlan
}

func newIOEventContext(
	service *Service,
	runtime ports.TaskAttachRuntime,
	task ports.Task,
	event ports.IOEvent,
	plan ioEventPlan,
) ioEventContext {
	return ioEventContext{
		service: service,
		runtime: runtime,
		task:    task,
		event:   event,
		plan:    plan,
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
	manager := applyDetachedIOEventMutation(ctx.runtime, ctx.task, "detach")
	detachLoadedIOManager(manager)
}

func handleIOEventReportError(ctx ioEventContext) {
	log.Warnf("[EVENTS] IOError event received for %s: %v", ctx.task.ID(), ctx.event.Err)
}

func (s *Service) stopFromIOEvent(runtime ports.TaskAttachRuntime, taskHandle ports.Task, reason ioStopReason) {
	var manager ports.IOManager
	var shouldStop bool

	mutateTask := func() {
		alreadyStopped := taskHandle.Status() == task.Status_STOPPED
		if !alreadyStopped {
			taskHandle.SetStatus(task.Status_STOPPED)
			taskHandle.SetExitInfo(reason.exitStatus, s.clockNow())
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
	handleIOEventStdinClosed(newIOEventContext(s, nil, taskHandle, ports.IOEvent{}, ioEventPlan{}))
}

func (s *Service) handleDetach(taskHandle ports.Task) {
	handleIOEventDetach(newIOEventContext(s, nil, taskHandle, ports.IOEvent{}, ioEventPlan{}))
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
