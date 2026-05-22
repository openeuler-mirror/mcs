package channels

import "testing"

func TestCloseNilChannel(t *testing.T) {
	Close(nil)
}

func TestCloseOpenChannel(t *testing.T) {
	ch := make(chan struct{})
	Close(ch)

	select {
	case <-ch:
	default:
		t.Fatal("expected channel to be closed")
	}
}

func TestCloseAlreadyClosedChannel(t *testing.T) {
	ch := make(chan struct{})
	close(ch)

	Close(ch)
}
