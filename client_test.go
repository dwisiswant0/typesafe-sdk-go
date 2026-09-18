package typesafe_test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

func TestConfigEnvironmentPrecedence(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", " env-key ")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", " env-model ")

	var auth, model string

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		auth = request.Header.Get("Authorization")

		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"model":"`+model+`"`) {
			t.Errorf("request body = %s, want model %q", body, model)
		}

		_, _ = io.WriteString(writer, systemOneJSON)
	}))
	defer server.Close()

	t.Setenv("TYPESAFE_BASE_URL", " "+server.URL+"/ ")

	explicit := testConfig("explicit-key", server.URL)
	explicit.DefaultModel = "config-model"

	//nolint:paralleltest // These subtests inherit process-wide environment changes from t.Setenv.
	for _, test := range []struct {
		name                              string
		config                            typesafe.Config
		requestModel, wantAuth, wantModel string
	}{
		{"environment", testConfig("", ""), "", "Bearer env-key", "env-model"},
		{"explicit configuration", explicit, "", "Bearer explicit-key", "config-model"},
		{"request model", testConfig("", ""), "request-model", "Bearer env-key", "request-model"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model = test.wantModel
			client := testClient(t, test.config)
			request := minimalRequest()

			request.Model = test.requestModel

			_, err := client.SystemOne(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}

			if auth != test.wantAuth {
				t.Errorf("Authorization = %q, want %q", auth, test.wantAuth)
			}

			if request.Model != test.requestModel {
				t.Fatal("request model was modified")
			}
		})
	}
}

func TestInvalidConfig(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("TYPESAFE_BASE_URL", "")

	negative := -1
	tests := []struct {
		name   string
		change func(*typesafe.Config)
	}{
		{"missing key", func(config *typesafe.Config) { config.APIKey = "" }},
		{"blank key", func(config *typesafe.Config) { config.APIKey = " \t " }},
		{"key injection", func(config *typesafe.Config) { config.APIKey = "secret\r\ninjected:value" }},
		{"relative URL", func(config *typesafe.Config) { config.BaseURL = "/relative" }},
		{"URL scheme", func(config *typesafe.Config) { config.BaseURL = "ftp://example.com" }},
		// The dummy credentials exercise rejection of URL user information.
		{
			"URL credentials",
			func(config *typesafe.Config) { config.BaseURL = "https://user:secret@example.com" },
		},
		{"URL query", func(config *typesafe.Config) { config.BaseURL = "https://example.com?secret=x" }},
		{"URL fragment", func(config *typesafe.Config) { config.BaseURL = "https://example.com#fragment" }},
		{"missing host", func(config *typesafe.Config) { config.BaseURL = "https://" }},
		{"invalid port", func(config *typesafe.Config) { config.BaseURL = "https://example.com:bad" }},
		{"negative timeout", func(config *typesafe.Config) { config.Timeout = -time.Second }},
		{"negative retries", func(config *typesafe.Config) { config.MaxRetries = &negative }},
		{"negative body limit", func(config *typesafe.Config) { config.MaxResponseBytes = -1 }},
		{"overflow body limit", func(config *typesafe.Config) { config.MaxResponseBytes = math.MaxInt64 }},
		{"header name", func(config *typesafe.Config) { config.Headers = http.Header{"Bad Name": {"x"}} }},
		{"header value", func(config *typesafe.Config) { config.Headers = http.Header{testHeader: {"secret\r\n"}} }},
		{"duplicate headers", func(config *typesafe.Config) {
			config.Headers = http.Header{testHeader: {"a"}, "x-test": {"b"}}
		}},
	}

	//nolint:paralleltest // These subtests inherit process-wide environment changes from t.Setenv.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig(testAPIKey, "")
			test.change(&config)

			_, err := typesafe.NewClient(config)
			if err == nil {
				t.Fatal("expected configuration error")
			}

			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("configuration error disclosed value: %v", err)
			}
		})
	}
}

func TestHeadersAreCopiedAndProtected(t *testing.T) {
	t.Parallel()

	headers := http.Header{
		"X-Custom":               {"client"},
		"authorization":          {otherLabel},
		"content-type":           {"text/plain"},
		"x-typesafe-retry-count": {"99"},
	}
	callHeaders := map[string][]string{
		"x-custom":   {"request"},
		acceptHeader: {"text/plain"},
		"User-Agent": {"override"},
	}
	option := typesafe.WithHeaders(http.Header(callHeaders))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		want := map[string]string{
			"X-Custom":               "request",
			"Authorization":          "Bearer key",
			"Content-Type":           jsonContentType,
			acceptHeader:             jsonContentType,
			"User-Agent":             "typesafe-sdk-go",
			"X-TypeSafe-Retry-Count": "",
		}
		for name, value := range want {
			if request.Header.Get(name) != value {
				t.Errorf("%s = %q, want %q", name, request.Header.Get(name), value)
			}
		}

		_, _ = io.WriteString(writer, systemOneJSON)
	}))
	defer server.Close()

	client := testClient(
		t,
		typesafe.Config{
			APIKey:           testAPIKey,
			BaseURL:          server.URL,
			Headers:          headers,
			DefaultModel:     "",
			Timeout:          0,
			MaxRetries:       nil,
			MaxResponseBytes: 0,
			Logger:           nil,
			HTTPClient:       nil,
		},
	)
	headers["X-Custom"][0] = "mutated"
	callHeaders["x-custom"][0] = "mutated"

	_, err := client.SystemOne(context.Background(), minimalRequest(), option)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRequestOptionsNeverSend(t *testing.T) {
	t.Parallel()

	client := testClient(
		t,
		testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("invalid options reached transport")

			return nil, context.Canceled
		})),
	)
	for _, option := range []typesafe.RequestOption{
		nil,
		typesafe.WithTimeout(0),
		typesafe.WithTimeout(-1),
		typesafe.WithMaxRetries(-1),
		typesafe.WithHeaders(http.Header{testHeader: {"secret\n"}}),
	} {
		_, err := client.ListModels(context.Background(), option)
		if err == nil {
			t.Fatal("expected option error")
		}
	}
}

func TestRedirectsDoNotForwardCredentialsOrBody(t *testing.T) {
	t.Parallel()

	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			var targetCalls atomic.Int32

			target := httptest.NewServer(
				http.HandlerFunc(
					func(writer http.ResponseWriter, _ *http.Request) {
						targetCalls.Add(1)
						writer.WriteHeader(http.StatusOK)
					},
				),
			)
			defer target.Close()

			origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Location", target.URL)
				writer.WriteHeader(status)
			}))
			defer origin.Close()

			custom := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				t.Error("SDK invoked caller redirect policy")

				return nil
			}, Transport: nil, Jar: nil, Timeout: 0}
			client := testClient(
				t,
				typesafe.Config{
					APIKey:           "private-key",
					BaseURL:          origin.URL,
					HTTPClient:       custom,
					DefaultModel:     "",
					Timeout:          0,
					MaxRetries:       nil,
					Headers:          nil,
					MaxResponseBytes: 0,
					Logger:           nil,
				},
			)
			_, err := client.SystemOne(context.Background(), minimalRequest())

			var apiErr *typesafe.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("error = %v", err)
			}

			if targetCalls.Load() != 0 {
				t.Fatal("redirect target received request")
			}

			if custom.CheckRedirect == nil {
				t.Fatal("caller client modified")
			}
		})
	}
}

func TestAPIErrorPreservesMetadataWithoutLoggingBody(t *testing.T) {
	t.Parallel()

	for _, status := range []int{400, 401, 403, 404, 422, 429, 500, 529} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("X-Typesafe-Request-Id", "failed-id")
				writer.Header().Set(retryAfterHeader, "1")
				writer.WriteHeader(status)
				_, _ = io.WriteString(writer, `{"detail":[{"msg":"bad value","input":"private-input"}]}`)
			}))
			defer server.Close()

			client := testClient(
				t,
				testConfig(testAPIKey, server.URL),
			)
			_, err := client.ListModels(context.Background(), typesafe.WithMaxRetries(0))

			var apiErr *typesafe.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.RequestID != "failed-id" ||
				apiErr.Header.Get(retryAfterHeader) != "1" {
				t.Fatalf("error = %#v", err)
			}

			if !strings.Contains(string(apiErr.Body), "private-input") ||
				strings.Contains(err.Error(), "private-input") {
				t.Fatalf("body preservation or error redaction failed: %v", err)
			}
		})
	}
}

func TestResponseBodyLimit(t *testing.T) {
	t.Parallel()

	for _, status := range []int{200, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				writer.WriteHeader(status)
				_, _ = io.WriteString(writer, strings.Repeat("x", 33))
			}))
			defer server.Close()

			client := testClient(
				t,
				typesafe.Config{
					APIKey:           testAPIKey,
					BaseURL:          server.URL,
					MaxResponseBytes: 32,
					Logger:           nil,
					DefaultModel:     "",
					Timeout:          0,
					MaxRetries:       nil,
					Headers:          nil,
					HTTPClient:       nil,
				},
			)
			_, err := client.ListModels(context.Background())

			var responseErr *typesafe.ResponseError
			if !errors.Is(err, typesafe.ErrResponseTooLarge) || !errors.As(err, &responseErr) ||
				len(responseErr.Body) != 32 ||
				responseErr.StatusCode != status {
				t.Fatalf("error = %#v", err)
			}

			if calls.Load() != 1 {
				t.Fatalf("oversized response retried %d times", calls.Load())
			}
		})
	}
}

func TestConcurrentClientReuse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(
			func(writer http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(writer, systemOneJSON) },
		),
	)
	defer server.Close()

	client := testClient(
		t,
		testConfig(testAPIKey, server.URL),
	)
	request := minimalRequest()
	option := typesafe.WithHeaders(http.Header{testHeader: {"concurrent"}})

	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			_, err := client.SystemOne(context.Background(), request, option)
			if err != nil {
				t.Error(err)
			}
		})
	}

	group.Wait()
}

func minimalRequest() typesafe.SystemOneRequest {
	return typesafe.SystemOneRequest{
		State:     stateField,
		Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: nil, Criteria: nil}},
		Model:     "",
		ExtraBody: nil,
	}
}
