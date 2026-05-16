package log

import (
	"bytes"
	"fmt"

	"github.com/sirupsen/logrus"
)

// contextHook adds the current namespace and container ID to all log entries.
type contextHook struct{}

func (h *contextHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *contextHook) Fire(entry *logrus.Entry) error {
	addContextFields(entry.Data)
	return nil
}

// containerdFormatter formats logs in containerd-compatible format.
// Format: time=<timestamp> level=<level> msg=<message> id=<id> namespace=<namespace>
type containerdFormatter struct{}

func (f *containerdFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b *bytes.Buffer

	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	timestamp := entry.Time.Format("2006-01-02T15:04:05.000000000Z")
	b.WriteString("time=\"")
	b.WriteString(timestamp)
	b.WriteString("\" ")

	b.WriteString("level=")
	b.WriteString(entry.Level.String())
	b.WriteString(" ")

	b.WriteString("msg=")
	b.WriteString(formatContainerdValue(entry.Message))

	writeContainerdField(b, IDKey, entry.Data[IDKey])
	writeContainerdField(b, NamespaceKey, entry.Data[NamespaceKey])
	for key, value := range entry.Data {
		if key != NamespaceKey && key != IDKey {
			writeContainerdField(b, key, value)
		}
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

func writeContainerdField(b *bytes.Buffer, key string, value any) {
	if value == nil {
		return
	}
	b.WriteString(" ")
	b.WriteString(key)
	b.WriteString("=")
	b.WriteString(formatContainerdValue(fmt.Sprintf("%v", value)))
}
