package nettimeout

import (
	"context"
	"time"
)

// WithDefault applies a package default only when the caller did not already
// provide a deadline. Caller deadlines are authoritative, even when longer.
func WithDefault(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
