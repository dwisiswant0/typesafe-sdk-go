package typesafe

import (
	"context"
	"log/slog"
	"time"
)

func (c *Client) logStart(ctx context.Context) time.Time {
	if c.logger == nil || !c.logger.Enabled(ctx, slog.LevelDebug) {
		return time.Time{}
	}

	return time.Now()
}

func (c *Client) logAttempt(
	ctx context.Context,
	method, path string,
	attempt int,
	started time.Time,
	meta ResponseMetadata,
) {
	if started.IsZero() {
		return
	}

	c.logger.LogAttrs(ctx, slog.LevelDebug, "typesafe: HTTP attempt",
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("attempt", attempt+1),
		slog.Int("status", meta.StatusCode),
		slog.Duration("duration", time.Since(started)),
		slog.String("request_id", meta.RequestID),
	)
}

func (c *Client) logRetry(ctx context.Context, method, path string, attempt int, delay time.Duration) {
	if c.logger == nil || !c.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}

	c.logger.LogAttrs(ctx, slog.LevelDebug, "typesafe: retry wait",
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("attempt", attempt+1),
		slog.Duration("delay", delay),
	)
}
