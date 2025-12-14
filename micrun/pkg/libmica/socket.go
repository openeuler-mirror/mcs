// TODO: using containerd socket utils
package libmica

import (
	"errors"
	"fmt"
	defs "micrun/definitions"
	log "micrun/logger"
	"net"
	"os"
	"strings"
	"time"
)

// TODO: seperate into mick_socket.go

// Types
// micaSocket handles Unix domain socket communication with mica daemon
type micaSocket struct {
	socketPath string
	conn       net.Conn
}

// Constructors
func newMicaSocket(socketPath string) *micaSocket {
	return &micaSocket{socketPath: socketPath}
}

// Helper functions
func validSocketPath(socketPath string) bool {
	if st, err := os.Stat(socketPath); err != nil {
		return false
	} else {
		return st.Mode()&os.ModeSocket != 0
	}
}

// micaSocket methods
func (ms *micaSocket) connect() error {
	conn, err := net.Dial("unix", ms.socketPath)
	if err != nil {
		log.Error("Failed to connect to MicaSocket", "error: ", err)
		return err
	}
	ms.conn = conn
	return nil
}

func (ms *micaSocket) close() error {
	if ms.conn != nil {
		return ms.conn.Close()
	}
	return nil
}

func (ms *micaSocket) tx(data []byte) error {
	if ms.conn == nil {
		return errors.New("socket not connected")
	}
	_, err := ms.conn.Write(data)
	return err
}

func (ms *micaSocket) rx() (string, error) {
	if ms.conn == nil {
		return "", errors.New("socket not connected")
	}

	ms.conn.SetReadDeadline(time.Now().Add(defs.MicaSocketTimout))

	responseBuffer := ""
	buf := make([]byte, defs.MicaSocketBufSize)

	for {
		n, err := ms.conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return "", errors.New("timeout while waiting for micad response")
			}
			return "", err
		}

		if n == 0 {
			break
		}

		responseBuffer += string(buf[:n])

		if strings.Contains(responseBuffer, defs.MicaFailed) {
			parts := strings.Split(responseBuffer, defs.MicaFailed)
			msg := strings.TrimSpace(parts[0])
			if msg != "" {
				log.Error(msg)
			}
			return defs.MicaFailed, nil
		} else if strings.Contains(responseBuffer, defs.MicaSuccess) {
			parts := strings.Split(responseBuffer, defs.MicaSuccess)
			msg := strings.TrimSpace(parts[0])
			if msg != "" {
				log.Info(msg)
			}
			return defs.MicaSuccess, nil
		}
	}

	return "", errors.New("unexpected response format")
}

// TODO: We need to manually fetch information from managed clients
// Because mica daemon print clients information by its own format, which is not
// compatible with containerd
func (ms *micaSocket) handleMsg(msg []byte) error {

	if err := ms.connect(); err != nil {
		return fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer func() {
		ms.close()
	}()

	if err := ms.tx(msg); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	response, err := ms.rx()
	if err != nil {
		return fmt.Errorf("failed to receive response: %w", err)
	}

	switch response {
	case defs.MicaSuccess:
		return nil
	case defs.MicaFailed:
		return fmt.Errorf("mica daemon reported failure")
	default:
		return fmt.Errorf("unexpected response format from mica daemon: %s, communication might broken?", response)
	}
}
