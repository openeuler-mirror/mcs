package contextx

import "context"

// OrBackground returns context.Background when ctx is nil.
func OrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
