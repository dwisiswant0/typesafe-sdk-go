package typesafe

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxRetryDelay      = 5 * time.Second
	maxBackoffExponent = 4
)

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError && status < 600
}

func retryDelay(attempt int, headers http.Header) time.Duration {
	if delay, ok := retryAfter(headers, time.Now()); ok && delay <= time.Minute {
		return delay
	}
	// Cap before shifting so large retry counts cannot overflow.
	delay := maxRetryDelay
	if attempt < maxBackoffExponent {
		delay = min(500*time.Millisecond<<attempt, delay)
	}

	return time.Duration(float64(delay) * (1 - 0.25*rand.Float64())) //nolint:gosec // Jitter has no security purpose.
}

func retryAfter(headers http.Header, now time.Time) (time.Duration, bool) {
	if delay, ok := parseDelay(headers.Get("Retry-After-Ms"), time.Millisecond); ok {
		return delay, true
	}

	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if delay, ok := parseDelay(raw, time.Second); ok {
		return delay, true
	}

	when, err := http.ParseTime(raw)
	if err != nil {
		return 0, false
	}

	return max(0, when.Sub(now)), true
}

func parseDelay(raw string, unit time.Duration) (time.Duration, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}

	nanos := value * float64(unit)
	if nanos >= float64(math.MaxInt64) {
		return 0, false
	}

	return time.Duration(nanos), true
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}

	err := ctx.Err()
	if err != nil {
		return err //nolint:wrapcheck // Preserve context error identity for callers.
	}

	return nil
}
