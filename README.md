# TypeSafe AI Go SDK

[![Go Reference](https://pkg.go.dev/badge/go.dw1.io/typesafe-sdk-go.svg)](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go)

Go SDK for [TypeSafe AI](https://docs.typesafe.ai/api).

## Quick start

The module uses Go 1.27.1+. Install the SDK with:

```sh
go get go.dw1.io/typesafe-sdk-go@latest
```

Set [`TYPESAFE_API_KEY`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.APIKey) before running the example. Create a client once and reuse it for later requests:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	typesafe "go.dw1.io/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient(typesafe.Config{})
	if err != nil {
		log.Fatal(err)
	}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: map[string]any{"document": "I was charged twice. Please help."},
		Questions: typesafe.Questions{
			"category": typesafe.ChoiceQuestion{
				Instructions: "What is this ticket about?",
				Criteria: map[string]any{"billing": nil, "technical": nil, "other": nil},
			},
		},
	})
	if err != nil {
		log.Print(err)
		return
	}
	
	answer, ok := response.Answers["category"].(*typesafe.ChoiceAnswer)
	if !ok {
		log.Print("missing category or unexpected answer type")
		return
	}

	fmt.Println(answer.Choice, answer.Confidence)
}
```

## Questions and answers

Give each question a name in the [`Questions`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Questions) map. The response uses the same name for its answer. A request can combine all three question types:

```go
questions := typesafe.Questions{
	"urgent": typesafe.NoulQuestion{
		Instructions: "Is this urgent?",
		Criteria: &typesafe.NoulCriteria{True: "Time-sensitive", False: "Can wait"},
	},
	"department": typesafe.ChoiceQuestion{
		Instructions: "Which team should handle this?",
		Criteria: map[string]any{"billing": "Payments and refunds", "technical": nil},
	},
	"frustration": typesafe.ScoreQuestion{
		Instructions: "How frustrated is the customer?",
		Criteria: []any{"Calm", "Frustrated", "Very angry"},
	},
}
```

### Supply state and criteria

State, instructions, and criterion descriptions accept strings, JSON objects, or arrays. Go structs and [`json.RawMessage`](https://pkg.go.dev/encoding/json#RawMessage) values work when they encode to one of these shapes. Nested values can include numbers, booleans, and null.

Instructions are optional. Choice and Noul descriptions can be null; score levels cannot be null. A score rubric must contain at least one level, starting at score zero.

### Read answers

Use a type assertion or type switch to read an answer from [`response.Answers`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#SystemOneResponse.Answers). Known answer types are [`*NoulAnswer`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#NoulAnswer), [`*ChoiceAnswer`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ChoiceAnswer), and [`*ScoreAnswer`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ScoreAnswer).

Scores can fall between integer rubric levels. Probability and legend maps use the API's string keys. Numbers in structured legend values use [`json.Number`](https://pkg.go.dev/encoding/json#Number) to preserve precision.

Token counts use [`*int64`](https://pkg.go.dev/builtin#int64). A [`nil`](https://pkg.go.dev/builtin#nil) pointer means the server omitted the count or returned null. A pointer to zero means the server reported zero tokens.

### Use additional API fields

Use [`RawQuestion`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#RawQuestion) for a complete JSON question with extra fields or a future question type. It requires the exact key `"type"` with a nonempty string value. The server validates the remaining fields.

Use [`SystemOneRequest.ExtraBody`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#SystemOneRequest.ExtraBody) for additional top-level fields, including null values. It cannot replace [`state`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#SystemOneRequest.State), [`model`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#SystemOneRequest.Model), or [`questions`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#SystemOneRequest.Questions).

The SDK retains unknown answer types as [`*UnknownAnswer`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#UnknownAnswer), including their original JSON. Read [`response.Response.Body`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseMetadata.Body) to access response fields that the SDK does not model.

## Configuration and requests

### Client settings

Set [`Config`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config) fields when you create the client. Nonempty string fields override their environment variables. The SDK trims environment values and ignores blank ones. [`BaseURL`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.BaseURL) and [`DefaultModel`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.DefaultModel) fall back to SDK defaults; [`APIKey`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.APIKey) must come from [`Config`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config) or [`TYPESAFE_API_KEY`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.APIKey).

| Setting | Default or fallback |
| --- | --- |
| [`Config.APIKey`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.APIKey) | [`TYPESAFE_API_KEY`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.APIKey); an API key is required |
| [`Config.BaseURL`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.BaseURL) | [`TYPESAFE_BASE_URL`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.BaseURL), then `https://api.typesafe.ai` |
| [`Config.DefaultModel`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.DefaultModel) | [`TYPESAFE_DEFAULT_MODEL`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.DefaultModel), then `jev-latest` |
| [`Config.Timeout`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.Timeout) | 10 seconds per attempt, including response body reads |
| [`Config.MaxRetries`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.MaxRetries) | [`nil`](https://pkg.go.dev/builtin#nil) uses two retries; a pointer to zero disables retries |
| [`Config.MaxResponseBytes`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.MaxResponseBytes) | 16 MiB of decompressed response data |
| [`Config.HTTPClient`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.HTTPClient) | An HTTP client with a reusable transport |
| [`Config.Logger`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.Logger) | [`nil`](https://pkg.go.dev/builtin#nil) disables logging |
| [`Config.Headers`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.Headers) | Additional headers, copied when the client is constructed |

Set the base URL to the API root without `/v1`. A gateway path prefix is supported. Use HTTPS for production credentials. The SDK rejects URLs that contain credentials, query parameters, or fragments.

### Per-request settings

Set [`SystemOneRequest.Model`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#SystemOneRequest.Model) to override the client's model for an evaluation. Both API methods also accept options that apply to one call. This example lists models with a timeout of 5 seconds per attempt, no retries, and an additional header:

```go
models, err := client.ListModels(ctx,
	typesafe.WithTimeout(5*time.Second),
	typesafe.WithMaxRetries(0),
	typesafe.WithHeaders(http.Header{"X-Correlation-ID": {"job-123"}}),
)
```

### Retries and timeouts

The SDK retries connection failures, interrupted response bodies, attempt timeouts, HTTP 408, HTTP 429, and HTTP 500–599, including 529. The default allows three attempts: the initial request and up to two retries.

Retry delays start at 500 ms and double up to 5 seconds. The SDK randomly reduces each delay by up to 25%. A valid `retry-after-ms` header takes precedence over `Retry-After`. The SDK honors server delays up to 60 seconds and uses its calculated backoff for longer delays.

The SDK encodes each request once and sends the same bytes on every attempt. Retrying an evaluation after a connection failure can incur duplicate usage. Disable retries if that is unacceptable for your application.

Use a context deadline to limit requests and retry waits across the operation. The per-attempt timeout does not set an overall retry budget. A custom [`http.Client.Timeout`](https://pkg.go.dev/net/http#Client.Timeout) adds another limit. Canceling the context stops requests and pending retries.

### Headers and redirects

The SDK rejects all HTTP redirects to prevent forwarding credentials or state to another endpoint. This rule also applies when you supply a custom [`HTTPClient`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.HTTPClient).

Custom headers cannot replace the SDK's authentication, JSON, identification, or retry-count headers.

### Logging

Set [`Config.Logger`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.Logger) to a [`*slog.Logger`](https://pkg.go.dev/log/slog#Logger) to enable optional diagnostics. The SDK emits records at [`slog.LevelDebug`](https://pkg.go.dev/log/slog#LevelDebug). Configure the handler to accept that level:

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelDebug,
}))
client, err := typesafe.NewClient(typesafe.Config{Logger: logger})
```

Each HTTP attempt records the method, SDK endpoint path, attempt number (starting at 1), status code, duration, and request ID. Status zero means no response metadata was available. Each retry wait records the method, endpoint path, completed attempt number, and delay. An HTTP attempt record does not indicate that response decoding succeeded.

Records exclude request and response bodies, credentials, full URLs, raw headers other than the request ID, and error messages. A [`nil`](https://pkg.go.dev/builtin#nil) logger is silent and does not use [`slog.Default`](https://pkg.go.dev/log/slog#Default). The application owns the handler, level, and output destination, and reports returned errors. Handlers run synchronously with the caller's context, so a slow handler adds latency to API calls.

### Concurrent use and connections

Share a client across goroutines. Do not modify request maps, slices, or shared custom transports during a call. The SDK copies configuration headers and per-call headers. It does not modify caller request data.

Call [`CloseIdleConnections`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Client.CloseIdleConnections) to release idle connections. Active requests continue. A custom HTTP client's transport and cookie jar remain shared, so closing idle connections also affects that transport.

## Errors and response metadata

Use [`errors.As`](https://pkg.go.dev/errors#As) to inspect an API error and [`errors.Is`](https://pkg.go.dev/errors#Is) to check a wrapped cause:

```go
var apiErr *typesafe.APIError

if errors.As(err, &apiErr) {
	fmt.Println(apiErr.StatusCode, apiErr.RequestID)
	// apiErr.Body and apiErr.Header preserve server details.
}
if errors.Is(err, context.DeadlineExceeded) {
	// The overall deadline or an attempt timeout expired.
}
```

[`APIError`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#APIError) reports a non-2xx HTTP response after retries. Its message includes the request method, endpoint, and status. The message omits the body because a server validation error can echo private input. Read the error's [`Body`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseMetadata.Body) and [`Header`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseMetadata.Header) fields when you need server details.

[`ResponseError`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseError) reports invalid JSON, missing or malformed required fields, an oversized response, or an interrupted body after retries. Use [`errors.Is`](https://pkg.go.dev/errors#Is) to check for [`typesafe.ErrResponseTooLarge`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ErrResponseTooLarge) when a response exceeds the size limit. Transport errors wrap their original causes.

Both successful response types provide [`Response.StatusCode`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseMetadata.StatusCode), [`Response.Header`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseMetadata.Header), [`Response.RequestID`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseMetadata.RequestID), and [`Response.Body`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#ResponseMetadata.Body). The SDK reads and closes each network response body before returning. Errors for oversized bodies retain only the permitted prefix. Oversized responses and malformed successful responses are not retried. Raw headers and bodies can contain sensitive data.

### Run the live API test

Set [`TYPESAFE_API_KEY`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.APIKey), then run the opt-in test below. It sends one evaluation to the configured service and can incur usage:

```sh
# Set TYPESAFE_API_KEY before running this command.
go test -tags=integration -run=TestLiveAPI -v
```

The test also reads [`TYPESAFE_BASE_URL`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.BaseURL) and [`TYPESAFE_DEFAULT_MODEL`](https://pkg.go.dev/go.dw1.io/typesafe-sdk-go#Config.DefaultModel). It skips if no API key is set.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.