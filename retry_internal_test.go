package typesafe

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"
)

const (
	retryAfterHeader       = "Retry-After"
	retryAfterMillisHeader = "Retry-After-Ms"
)

func TestRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		headers http.Header
		want    time.Duration
		valid   bool
	}{
		{"absent", http.Header{}, 0, false},
		{"seconds", http.Header{retryAfterHeader: {"2"}}, 2 * time.Second, true},
		{"fractional seconds", http.Header{retryAfterHeader: {"0.25"}}, 250 * time.Millisecond, true},
		{
			"milliseconds win",
			http.Header{retryAfterHeader: {"10"}, retryAfterMillisHeader: {"100"}},
			100 * time.Millisecond,
			true,
		},
		{"zero", http.Header{retryAfterMillisHeader: {"0"}}, 0, true},
		{"negative", http.Header{retryAfterHeader: {"-1"}}, 0, false},
		{"NaN fallback", http.Header{retryAfterMillisHeader: {"NaN"}, retryAfterHeader: {"1"}}, time.Second, true},
		{"infinite", http.Header{retryAfterHeader: {"Inf"}}, 0, false},
		{"overflow", http.Header{retryAfterHeader: {"1e30"}}, 0, false},
		{
			"future date",
			http.Header{retryAfterHeader: {now.Add(10 * time.Second).Format(http.TimeFormat)}},
			10 * time.Second,
			true,
		},
		{"past date", http.Header{retryAfterHeader: {now.Add(-time.Second).Format(http.TimeFormat)}}, 0, true},
		{"bad date", http.Header{retryAfterHeader: {"tomorrow"}}, 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, valid := retryAfter(test.headers, now)
			if got != test.want || valid != test.valid {
				t.Fatalf("retryAfter = %v, %v; want %v, %v", got, valid, test.want, test.valid)
			}
		})
	}
}

func TestRetryBackoffIsBounded(t *testing.T) {
	t.Parallel()

	for _, attempt := range []int{0, 1, 2, 3, 4, 63, 1000000} {
		maximum := 5 * time.Second
		if attempt < 4 {
			maximum = 500 * time.Millisecond << attempt
		}

		for range 20 {
			delay := retryDelay(attempt, http.Header{retryAfterHeader: {"999999"}})
			if delay < maximum*3/4 || delay > maximum {
				t.Fatalf("attempt %d: delay %v outside [%v, %v]", attempt, delay, maximum*3/4, maximum)
			}
		}
	}

	if got := retryDelay(0, http.Header{retryAfterHeader: {"60"}}); got != time.Minute {
		t.Fatalf("server delay = %v", got)
	}
}

func TestWaitRetryCancellation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		go func() { done <- waitRetry(ctx, time.Minute) }()

		synctest.Wait()
		cancel()

		err := <-done
		if err != context.Canceled { //nolint:err113,errorlint // Check exact context error identity.
			t.Fatalf("error = %v", err)
		}
	})
}
