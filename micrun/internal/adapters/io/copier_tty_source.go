package io

import (
	"io"

	"golang.org/x/sys/unix"
	"micrun/internal/support/logger"
	"micrun/internal/support/sys"
	"micrun/internal/support/validation"
)

type ttyReadSource struct {
	reader io.Reader
	fd     int
}

func newTTYReadSource(name, containerID string, reader io.Reader) ttyReadSource {
	source := ttyReadSource{
		fd: -1,
	}
	if reader == nil || validation.IsNil(reader) {
		log.Warnf("[IO] %s: TTY reader is nil for %s", name, containerID)
		return source
	}
	source.reader = reader
	if fd, ok := sys.FDOf(reader); ok {
		source.fd = fd
		return source
	}
	log.Debugf("[IO] %s: TTY does not support Fd(), falling back to busy polling for %s", name, containerID)
	return source
}

func (s ttyReadSource) read(buf []byte) (int, error) {
	if s.fd >= 0 {
		return unix.Read(s.fd, buf)
	}
	if s.reader == nil {
		return 0, io.ErrClosedPipe
	}
	return s.reader.Read(buf)
}
