package typesafe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

const (
	logMessageField = "msg"
	logAttemptField = "attempt"
	logStatusField  = "status"
)

func TestLoggingAttemptsAndRetries(t *testing.T) {
	t.Parallel()

	var (
		output bytes.Buffer
		calls  int
	)

	config := testTransportConfig("private-api-key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		status := http.StatusServiceUnavailable
		if calls == 2 {
			status = http.StatusOK
		}

		return testResponse(status, http.Header{
			"X-Typesafe-Request-Id": {"req-log"},
			"Retry-After-Ms":        {"0"},
			"Set-Cookie":            {"private-cookie"},
		}, io.NopCloser(strings.NewReader(systemOneJSON))), nil
	}))
	config.BaseURL = "https://private-host.example/private-prefix"
	config.Headers = http.Header{"X-Private": {"private-header"}}
	config.Logger = loggingLogger(&output, slog.LevelDebug)
	request := minimalRequest()
	request.State = map[string]any{"private-state": "private-input"}

	response, err := testClient(t, config).SystemOne(context.Background(), request)
	if err != nil || response == nil || calls != 2 {
		t.Fatalf("response=%v, calls=%d, error=%v", response, calls, err)
	}

	records := loggingRecords(t, &output)
	if len(records) != 3 {
		t.Fatalf("got %d log records, want 3: %s", len(records), output.String())
	}

	checkLoggingFields(t, records[0], map[string]any{
		logMessageField: "typesafe: HTTP attempt", "level": "DEBUG", "method": http.MethodPost,
		"path": "/v1/systemone", logAttemptField: float64(1), logStatusField: float64(503), "request_id": "req-log",
	})
	checkLoggingFields(t, records[1], map[string]any{
		logMessageField: "typesafe: retry wait", "level": "DEBUG", "method": http.MethodPost,
		"path": "/v1/systemone", logAttemptField: float64(1), "delay": float64(0),
	})
	checkLoggingFields(t, records[2], map[string]any{
		logMessageField: "typesafe: HTTP attempt", logAttemptField: float64(2), logStatusField: float64(200),
	})

	for _, index := range []int{0, 2} {
		duration, ok := records[index]["duration"].(float64)
		if !ok || duration < 0 {
			t.Errorf("invalid duration in record: %v", records[index])
		}
	}

	checkLoggingExcludes(t, &output,
		"private-api-key", "private-host", "private-prefix", "private-header", "private-cookie",
		"private-state", "private-input", "answers", "probabilities", "Set-Cookie",
	)
}

func TestLoggingTransportError(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	cause := fmt.Errorf("private-transport-detail: %w", io.ErrClosedPipe)
	config := testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	}))
	config.Logger = loggingLogger(&output, slog.LevelDebug)

	_, err := testClient(t, config).ListModels(context.Background(), typesafe.WithMaxRetries(0))
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want transport cause", err)
	}

	records := loggingRecords(t, &output)
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	checkLoggingFields(t, records[0], map[string]any{logStatusField: float64(0), logAttemptField: float64(1)})

	checkLoggingExcludes(t, &output, "private-transport-detail")
}

func TestLoggingDisabled(t *testing.T) { //nolint:paralleltest // Changing slog.Default requires a serial test.
	var output bytes.Buffer

	logger := loggingLogger(&output, slog.LevelDebug)
	previous := slog.Default()

	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, logger := range []*slog.Logger{nil, loggingLogger(&output, slog.LevelInfo)} {
		config := testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testResponse(http.StatusServiceUnavailable, http.Header{"Retry-After-Ms": {"0"}},
				io.NopCloser(strings.NewReader("private-response"))), nil
		}))
		config.Logger = logger
		_, err := testClient(t, config).ListModels(context.Background(), typesafe.WithMaxRetries(1))

		_, ok := errors.AsType[*typesafe.APIError](err)
		if !ok {
			t.Fatalf("error = %v, want APIError", err)
		}
	}

	if output.Len() != 0 {
		t.Fatalf("logging should be disabled: %s", output.String())
	}
}

func TestLoggingBeforeTransport(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	config := testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected HTTP attempt")

		return nil, io.ErrUnexpectedEOF
	}))
	config.Logger = loggingLogger(&output, slog.LevelDebug)
	client := testClient(t, config)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}

	var invalid typesafe.SystemOneRequest

	_, err = client.SystemOne(context.Background(), invalid)
	if err == nil {
		t.Fatal("expected validation error")
	}

	_, err = client.ListModels(context.Background(), nil)
	if err == nil {
		t.Fatal("expected request option error")
	}

	if output.Len() != 0 {
		t.Fatalf("unexpected records before transport: %s", output.String())
	}
}

func TestLoggingContextAndCompletedResponse(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(http.StatusOK, nil, io.NopCloser(strings.NewReader(`{"models":[]}`))), nil
	}))
	config.Logger = slog.New(&loggingContextHandler{
		Handler: loggingLogger(&output, slog.LevelDebug).Handler(),
		beforeHandle: func(logContext context.Context) {
			if logContext != ctx {
				t.Error("logger did not receive caller context")
			}

			cancel()
		},
	})

	response, err := testClient(t, config).ListModels(ctx)
	if err != nil || response == nil {
		t.Fatalf("logging changed completed response: response=%v, error=%v", response, err)
	}

	if ctx.Err() == nil {
		t.Fatal("logger was not called")
	}
}

type loggingContextHandler struct {
	slog.Handler

	beforeHandle func(context.Context)
}

func (handler *loggingContextHandler) Handle(ctx context.Context, record slog.Record) error {
	handler.beforeHandle(ctx)

	err := handler.Handler.Handle(ctx, record)
	if err != nil {
		return fmt.Errorf("handle test log record: %w", err)
	}

	return nil
}

func TestLoggingConcurrentRequests(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	config := testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(http.StatusOK, nil, io.NopCloser(strings.NewReader(`{"models":[]}`))), nil
	}))
	config.Logger = loggingLogger(&output, slog.LevelDebug)
	client := testClient(t, config)

	var group sync.WaitGroup

	const requests = 16
	for range requests {
		group.Go(func() {
			_, err := client.ListModels(context.Background())
			if err != nil {
				t.Error(err)
			}
		})
	}

	group.Wait()

	if records := loggingRecords(t, &output); len(records) != requests {
		t.Fatalf("got %d records, want %d", len(records), requests)
	}
}

func loggingLogger(writer io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		AddSource: false, Level: level, ReplaceAttr: nil,
	}))
}

func loggingRecords(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any

	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))

	for {
		var record map[string]any

		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return records
		}

		if err != nil {
			t.Fatal(err)
		}

		records = append(records, record)
	}
}

func checkLoggingFields(t *testing.T, record, want map[string]any) {
	t.Helper()

	for key, value := range want {
		if record[key] != value {
			t.Errorf("log %s = %v, want %v", key, record[key], value)
		}
	}
}

func checkLoggingExcludes(t *testing.T, output *bytes.Buffer, excluded ...string) {
	t.Helper()

	for _, value := range excluded {
		if strings.Contains(output.String(), value) {
			t.Errorf("log contains excluded data %q", value)
		}
	}
}

func BenchmarkListModelsLogging(b *testing.B) {
	for _, test := range []struct {
		name   string
		logger *slog.Logger
	}{
		{"disabled", nil},
		{"filtered", loggingLogger(io.Discard, slog.LevelInfo)},
		{"debug", loggingLogger(io.Discard, slog.LevelDebug)},
	} {
		b.Run(test.name, func(b *testing.B) {
			config := testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return testResponse(http.StatusOK, nil, io.NopCloser(strings.NewReader(`{"models":[]}`))), nil
			}))
			config.Logger = test.logger
			client := testClient(b, config)
			b.ReportAllocs()

			for b.Loop() {
				_, err := client.ListModels(context.Background())
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
