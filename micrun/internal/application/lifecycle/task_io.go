package lifecycle

import (
	"context"
	"io"
	"reflect"

	"micrun/internal/ports"
	"micrun/internal/support/channels"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

type taskIOStreams struct {
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
}

func openTaskIOStreams(ctx context.Context, sandbox ports.Sandbox, taskHandle ports.Task) (taskIOStreams, error) {
	stdin, stdout, stderr, err := sandbox.IOStream(ctx, taskHandle.ID(), taskHandle.ID())
	if err != nil {
		return taskIOStreams{}, err
	}
	return taskIOStreams{stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (streams taskIOStreams) closeForTask(taskID string) {
	closeTaskIO(taskID, "stdin", streams.stdin)
	closeTaskIOReaders(taskID, streams.stdout, streams.stderr)
}

func closeTaskIOReaders(taskID string, stdout, stderr io.Reader) {
	if sameTaskIOReader(stdout, stderr) {
		closeTaskIOReader(taskID, "stdout/stderr", stdout)
		return
	}
	closeTaskIOReader(taskID, "stdout", stdout)
	closeTaskIOReader(taskID, "stderr", stderr)
}

func closeTaskIOReader(taskID, streamName string, reader io.Reader) {
	closer, ok := reader.(io.Closer)
	if !ok {
		return
	}
	closeTaskIO(taskID, streamName, closer)
}

func closeTaskIO(taskID, streamName string, closer io.Closer) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		log.Errorf("failed to close %s stream for %s: %v", streamName, taskID, err)
	}
}

func sameTaskIOReader(left, right io.Reader) bool {
	if validation.IsNil(left) || validation.IsNil(right) {
		return false
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() || !leftValue.Type().Comparable() {
		return false
	}
	return leftValue.Interface() == rightValue.Interface()
}

func cleanupTaskIOAfterStartFailure(runtime ports.TaskLifecycleRuntime, taskHandle ports.Task) {
	stdin := snapshotTaskAndUnsetStdinPipe(runtime, taskHandle)
	closeTaskIO(taskHandle.ID(), "stdin", stdin)
}

func recordTaskStdin(runtime ports.TaskLifecycleRuntime, taskHandle ports.Task, stdin io.WriteCloser) {
	setTaskStdinPipe(runtime, taskHandle, stdin)
}

func taskHasAttachPaths(taskHandle ports.Task) bool {
	return taskHandle.StdinPath() != "" || taskHandle.StdoutPath() != "" || taskHandle.StderrPath() != ""
}

func completeTaskWithoutAttach(taskHandle ports.Task) {
	channels.Close(taskHandle.StdinCloser())
	if taskHandle.IsCriSandbox() || !taskHandle.CanBeSandbox() {
		return
	}
	signalTaskIOExit(taskHandle)
}
