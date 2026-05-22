package container

import (
	"context"

	"micrun/internal/support/contextx"
)

func queryContext(ctx context.Context) context.Context {
	return contextx.OrBackground(ctx)
}

func activeContainerContext(ctx context.Context) (context.Context, error) {
	ctx = queryContext(ctx)
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	return ctx, nil
}
