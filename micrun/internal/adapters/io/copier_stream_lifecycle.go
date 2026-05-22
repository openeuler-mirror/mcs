package io

import (
	"io"

	"micrun/internal/support/sys"
)

func (c *Copier) closeFIFOs() error {
	return joinCloseErrors(
		closeStream("stdin FIFO", &c.stdinFifo),
		closeStream("stdout FIFO", &c.stdoutFIFO),
		closeStream("stderr FIFO", &c.stderrFIFO),
	)
}

func (c *Copier) closeTTYs() error {
	err := joinCloseErrors(
		closeTTYOutputReaders(&c.ttyOut, &c.ttyErr),
		closeStream("TTY stdin", &c.ttyIn),
	)
	c.ttyOut = nil
	c.ttyErr = nil
	c.ttyIn = nil
	return err
}

func closeTTYOutputReaders(stdout, stderr *io.Reader) []error {
	if stdout == nil || stderr == nil {
		return nil
	}

	if _, hasFD := sameTTYOutputFD(*stdout, *stderr); hasFD {
		if _, ok := (*stdout).(io.Closer); ok {
			return closeStreamIfCloseable("TTY stdout", stdout, nil)
		}
		return closeStreamIfCloseable("TTY stderr", stderr, nil)
	}

	if sameIOHandle(*stdout, *stderr) {
		errs := closeStreamIfCloseable("TTY output", stdout, nil)
		resetPointer(stderr)
		return errs
	}

	return mergeCloseErrors(
		closeStreamIfCloseable("TTY stdout", stdout, nil),
		closeStreamIfCloseable("TTY stderr", stderr, nil),
	)
}

func fdOf(value any) (int, bool) {
	return sys.FDOf(value)
}
