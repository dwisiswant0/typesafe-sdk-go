package typesafe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
)

func (c *Client) request(
	ctx context.Context,
	method, path string,
	body []byte,
	options []RequestOption,
) (ResponseMetadata, error) {
	config, err := c.configureRequest(options, body != nil)
	if err != nil {
		return ResponseMetadata{}, err
	}

	for attempt := 0; ; attempt++ {
		err := ctx.Err()
		if err != nil {
			return ResponseMetadata{}, err //nolint:wrapcheck // Preserve context error identity for callers.
		}

		if attempt > 0 {
			config.headers.Set("X-Typesafe-Retry-Count", strconv.Itoa(attempt))
		}

		started := c.logStart(ctx)
		meta, err := c.attempt(ctx, method, path, body, config)

		contextErr := ctx.Err()
		c.logAttempt(ctx, method, path, attempt, started, meta)

		if contextErr != nil {
			return meta, contextErr //nolint:wrapcheck // Preserve context error identity for callers.
		}

		retry, err := responseOutcome(meta, method, path, err)
		if err == nil || attempt >= config.maxRetries || !retry {
			return meta, err
		}

		delay := retryDelay(attempt, meta.Header)
		c.logRetry(ctx, method, path, attempt, delay)

		err = waitRetry(ctx, delay)
		if err != nil {
			return meta, err
		}
	}
}

func (c *Client) configureRequest(options []RequestOption, hasBody bool) (requestConfig, error) {
	config := requestConfig{headers: c.headers.Clone(), timeout: c.timeout, maxRetries: c.maxRetries}

	for _, option := range options {
		if option == nil {
			return requestConfig{}, errNilRequestOption
		}

		err := option(&config)
		if err != nil {
			return requestConfig{}, err
		}
	}

	config.headers.Set("Authorization", "Bearer "+c.apiKey)
	config.headers.Set("Accept", "application/json")
	config.headers.Set("User-Agent", "typesafe-sdk-go")
	config.headers.Set("X-Typesafe-Sdk", "typesafe-sdk-go")
	config.headers.Set("X-Typesafe-Runtime", runtime.Version()+"; "+runtime.GOOS+"/"+runtime.GOARCH)
	config.headers.Del("Content-Type")

	if hasBody {
		config.headers.Set("Content-Type", "application/json")
	}

	config.headers.Del("X-Typesafe-Retry-Count")

	return config, nil
}

func responseOutcome(meta ResponseMetadata, method, path string, err error) (bool, error) {
	if err != nil {
		// Response size limits are deliberate local failures, not transient I/O.
		return !errors.Is(err, ErrResponseTooLarge), err
	}

	if meta.StatusCode >= http.StatusOK && meta.StatusCode < http.StatusMultipleChoices {
		return false, nil
	}

	return retryableStatus(meta.StatusCode), &APIError{ResponseMetadata: meta, Method: method, Path: path}
}

func (c *Client) attempt(
	ctx context.Context,
	method, path string,
	body []byte,
	config requestConfig,
) (ResponseMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, config.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return ResponseMetadata{}, fmt.Errorf("typesafe: create %s %s request: %w", method, path, err)
	}

	req.Header = config.headers.Clone()

	res, err := c.httpClient.Do(req)
	if err != nil {
		return ResponseMetadata{}, fmt.Errorf("typesafe: send %s %s: %w", method, path, err)
	}
	// A close error must not repeat a POST after its response was fully read.
	defer res.Body.Close() //nolint:errcheck // Preserve the completed response.

	meta := ResponseMetadata{
		StatusCode: res.StatusCode,
		Header:     res.Header.Clone(),
		RequestID:  res.Header.Get("X-Typesafe-Request-Id"),
		Body:       nil,
	}

	meta.Body, err = io.ReadAll(io.LimitReader(res.Body, c.maxResponseBytes+1))
	if int64(len(meta.Body)) > c.maxResponseBytes {
		meta.Body = meta.Body[:c.maxResponseBytes]

		return meta, &ResponseError{ResponseMetadata: meta, Err: ErrResponseTooLarge}
	}

	if err != nil {
		return meta, &ResponseError{ResponseMetadata: meta, Err: err}
	}

	return meta, nil
}
