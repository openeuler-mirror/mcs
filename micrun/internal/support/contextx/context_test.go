package contextx

import (
	"context"
	"testing"
)

func TestOrBackground(t *testing.T) {
	if got := OrBackground(nil); got == nil {
		t.Fatal("OrBackground(nil) returned nil")
	}

	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	if got := OrBackground(ctx); got != ctx {
		t.Fatal("OrBackground should preserve non-nil context")
	}
}
