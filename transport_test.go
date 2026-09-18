package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

func TestRetryStatusesAndBodyReplay(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ status, attempts int }{
		{408, 3}, {429, 3}, {500, 3}, {503, 3}, {529, 3}, {599, 3},
		{400, 1}, {401, 1}, {403, 1}, {404, 1}, {409, 1}, {422, 1}, {600, 1},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			t.Parallel()

			var (
				calls  int
				bodies []string
			)

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				bodies = append(bodies, string(body))

				wantRetry := ""
				if calls > 0 {
					wantRetry = strconv.Itoa(calls)
				}

				if got := request.Header.Get("X-Typesafe-Retry-Count"); got != wantRetry {
					t.Errorf("retry count = %q, want %q", got, wantRetry)
				}

				calls++

				writer.Header().Set("Retry-After-Ms", "0")

				if calls < 3 {
					writer.WriteHeader(test.status)
					_, _ = io.WriteString(writer, `{"detail":"retry"}`)

					return
				}

				_, _ = io.WriteString(writer, systemOneJSON)
			}))
			defer server.Close()

			client := testClient(
				t,
				testConfig(testAPIKey, server.URL),
			)
			state := &countingState{calls: 0}
			request := minimalRequest()
			request.State = state

			_, err := client.SystemOne(context.Background(), request)
			checkRetryReplay(t, test.attempts, calls, state.calls, bodies, err)
		})
	}
}

func TestRetryLimitAndPerRequestOverride(t *testing.T) {
	t.Parallel()

	var calls int

	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		return testResponse(
			503,
			http.Header{retryAfterHeader: {"0"}},
			io.NopCloser(strings.NewReader("unavailable")),
		), nil
	})
	zero := 0
	client := testClient(
		t,
		typesafe.Config{
			APIKey:           testAPIKey,
			MaxRetries:       &zero,
			HTTPClient:       &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0},
			BaseURL:          "",
			DefaultModel:     "",
			Timeout:          0,
			Headers:          nil,
			MaxResponseBytes: 0,
			Logger:           nil,
		},
	)

	for _, test := range []struct {
		options []typesafe.RequestOption
		want    int
	}{{nil, 1}, {[]typesafe.RequestOption{typesafe.WithMaxRetries(3)}, 4}, {nil, 1}} {
		calls = 0
		_, err := client.ListModels(context.Background(), test.options...)

		var apiErr *typesafe.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable || calls != test.want {
			t.Fatalf("attempts=%d, error=%v", calls, err)
		}
	}
}

func TestRetryConnectionErrorAndPerAttemptTimeout(t *testing.T) {
	t.Parallel()

	for _, timeout := range []bool{false, true} {
		t.Run(strconv.FormatBool(timeout), func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				calls := 0
				cause := io.ErrClosedPipe
				client := testClient(
					t,
					testTransportConfig(testAPIKey, roundTripFunc(func(request *http.Request) (*http.Response, error) {
						calls++

						if timeout {
							<-request.Context().Done()

							return nil, request.Context().Err()
						}

						return nil, cause
					})),
				)
				start := time.Now()
				_, err := client.ListModels(context.Background(), typesafe.WithTimeout(time.Second))

				if calls != 3 {
					t.Fatalf("attempts = %d", calls)
				}

				if timeout {
					if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) < 3*time.Second ||
						time.Since(start) > 5*time.Second {
						t.Fatalf("timeout error = %v, duration=%v", err, time.Since(start))
					}
				} else if !errors.Is(err, cause) {
					t.Fatalf("connection cause lost: %v", err)
				}
			})
		})
	}
}

func TestCancellationStopsRetries(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := testClient(
			t,
			testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++

				return testResponse(
					429,
					http.Header{retryAfterHeader: {"60"}},
					io.NopCloser(strings.NewReader("busy")),
				), nil
			})),
		)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)

		go func() { _, err := client.ListModels(ctx); done <- err }()

		synctest.Wait()

		if calls != 1 {
			t.Fatalf("attempts before cancellation=%d", calls)
		}

		cancel()

		err := <-done
		if err != context.Canceled { //nolint:err113,errorlint // Check exact context error identity.
			t.Fatalf("error=%v", err)
		}

		if calls != 1 {
			t.Fatal("request retried after cancellation")
		}

		_, err = client.SystemOne(ctx, minimalRequest())

		if err != context.Canceled { //nolint:err113,errorlint // Check exact context error identity.
			t.Fatalf("pre-canceled error=%v", err)
		}

		if calls != 1 {
			t.Fatal("pre-canceled request reached transport")
		}
	})
}

func TestOverallDeadlineIncludesRetries(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := testClient(
			t,
			testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++

				return testResponse(
					529,
					http.Header{retryAfterHeader: {"60"}},
					io.NopCloser(strings.NewReader("busy")),
				), nil
			})),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.ListModels(ctx)
		if err != context.DeadlineExceeded || calls != 1 { //nolint:err113,errorlint // Check exact context error identity.
			t.Fatalf("attempts=%d, error=%v", calls, err)
		}
	})
}

func TestResponseBodyTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", jsonContentType)
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, `{"models":`)

		err := http.NewResponseController(writer).Flush()
		if err != nil {
			t.Error(err)

			return
		}

		<-request.Context().Done()
	}))
	defer server.Close()

	client := testClient(
		t,
		testConfig(testAPIKey, server.URL),
	)

	_, err := client.ListModels(
		context.Background(),
		typesafe.WithTimeout(50*time.Millisecond),
		typesafe.WithMaxRetries(0),
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body timeout error = %v", err)
	}
}

func TestBodiesClosedOnSuccessFailureAndRetry(t *testing.T) {
	t.Parallel()

	for _, status := range []int{200, 401, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			calls, closed := 0, 0
			client := testClient(
				t,
				testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++

					return testResponse(status, http.Header{retryAfterHeader: {"0"}}, &closeTracker{
						Reader: strings.NewReader(`{"models":[]}`),
						closed: &closed, closeErr: nil,
					}), nil
				})),
			)
			_, _ = client.ListModels(context.Background())

			if calls != closed {
				t.Fatalf("attempts=%d, bodies closed=%d", calls, closed)
			}
		})
	}
}

func TestInterruptedResponseBodyRetries(t *testing.T) {
	t.Parallel()

	calls, closed := 0, 0

	client := testClient(
		t,
		testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++

			var reader io.Reader = strings.NewReader(`{"models":[]}`)
			if calls == 1 {
				reader = io.MultiReader(strings.NewReader(`{"models":`), errorReader{})
			}

			return testResponse(
				200,
				http.Header{retryAfterHeader: {"0"}},
				&closeTracker{Reader: reader, closed: &closed, closeErr: nil},
			), nil
		})),
	)

	_, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if calls != 2 || closed != 2 {
		t.Fatalf("attempts=%d, bodies closed=%d", calls, closed)
	}
}

func TestHTTPConnectionReuse(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writer.Header().Set(retryAfterHeader, "0")
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, "busy")

			return
		}

		_, _ = io.WriteString(writer, `{"models":[]}`)
	}))
	defer server.Close()

	client := testClient(
		t,
		testConfig(testAPIKey, server.URL),
	)

	var reused atomic.Int32

	var trace httptrace.ClientTrace

	trace.GotConn = func(info httptrace.GotConnInfo) {
		if info.Reused {
			reused.Add(1)
		}
	}
	ctx := httptrace.WithClientTrace(context.Background(), &trace)

	for range 2 {
		_, err := client.ListModels(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}

	if calls.Load() != 3 || reused.Load() != 2 {
		t.Fatalf("requests=%d, reused connections=%d", calls.Load(), reused.Load())
	}
}

type countingState struct{ calls int }

func (s *countingState) MarshalJSON() ([]byte, error) {
	s.calls++

	return []byte(fmt.Sprintf(`{"snapshot":%d}`, s.calls)), nil
}

type closeTracker struct {
	io.Reader

	closed   *int
	closeErr error
}

func (b *closeTracker) Close() error {
	*b.closed++

	return b.closeErr
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestCompletedResponseIgnoresCloseError(t *testing.T) {
	t.Parallel()

	calls, closed := 0, 0
	client := testClient(t, testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		return testResponse(http.StatusOK, nil, &closeTracker{
			Reader: strings.NewReader(systemOneJSON), closed: &closed, closeErr: io.ErrClosedPipe,
		}), nil
	})))

	_, err := client.SystemOne(context.Background(), minimalRequest())
	if err != nil {
		t.Fatal(err)
	}

	if calls != 1 || closed != 1 {
		t.Fatalf("attempts=%d, bodies closed=%d", calls, closed)
	}
}

func checkRetryReplay(tb testing.TB, attempts, calls, encodings int, bodies []string, err error) {
	tb.Helper()

	if attempts == 3 && err != nil {
		tb.Fatal(err)
	}

	if attempts == 1 && err == nil {
		tb.Fatal("expected status error")
	}

	if calls != attempts {
		tb.Fatalf("attempts = %d, want %d", calls, attempts)
	}

	if encodings != 1 {
		tb.Fatalf("state encoded %d times", encodings)
	}

	for _, body := range bodies {
		if body != bodies[0] {
			tb.Fatal("retry changed request body")
		}
	}
}

func TestCanceledContextNeverSends(t *testing.T) {
	t.Parallel()
	client := testClient(t, testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("canceled request reached transport")

		return nil, context.Canceled
	})))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.SystemOne(ctx, minimalRequest())
	if err != context.Canceled { //nolint:err113,errorlint // Check exact context error identity.
		t.Fatalf("SystemOne cancellation error = %v", err)
	}

	_, err = client.ListModels(ctx)
	if err != context.Canceled { //nolint:err113,errorlint // Check exact context error identity.
		t.Fatalf("ListModels cancellation error = %v", err)
	}
}

func TestCancellationDuringAttemptPreservesContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := testClient(t, testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
		cancel()

		return nil, context.Canceled
	})))

	_, err := client.ListModels(ctx)
	if err != context.Canceled { //nolint:err113,errorlint // Check exact context error identity.
		t.Fatalf("cancellation error = %v", err)
	}
}
