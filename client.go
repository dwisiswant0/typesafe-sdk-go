package typesafe

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the API root used when no base URL is set. It excludes /v1.
	DefaultBaseURL = "https://api.typesafe.ai"
	// DefaultModel is the model used when neither configuration nor a request sets one.
	DefaultModel = "jev-latest"
	// DefaultTimeout limits one request attempt, including the response body read.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxRetries is the retry limit after the initial request.
	DefaultMaxRetries = 2
	// DefaultMaxResponseBytes limits the decompressed response body held in memory.
	DefaultMaxResponseBytes int64 = 16 << 20
)

// Config sets client credentials, defaults, and HTTP behavior. Empty string fields
// use their environment variables. Only [Config.BaseURL] and [Config.DefaultModel]
// have SDK defaults; an API key is required. Creating a client does not send a request.
type Config struct {
	// APIKey authenticates requests. It falls back to TYPESAFE_API_KEY.
	// An API key is required.
	APIKey string
	// BaseURL sets the API root. It falls back to TYPESAFE_BASE_URL,
	// then [DefaultBaseURL].
	BaseURL string
	// DefaultModel sets the model for requests that omit [SystemOneRequest.Model].
	// It falls back to TYPESAFE_DEFAULT_MODEL, then the [DefaultModel] constant.
	DefaultModel string
	// Timeout limits each attempt. Zero uses [DefaultTimeout]; negative values
	// are invalid.
	Timeout time.Duration
	// MaxRetries limits retries after the initial request. Nil uses
	// [DefaultMaxRetries]; a pointer to zero disables retries.
	MaxRetries *int
	// Headers adds request headers. [NewClient] copies the map and its values.
	Headers http.Header
	// MaxResponseBytes limits the response body size. Zero uses
	// [DefaultMaxResponseBytes]; other values must be positive.
	MaxResponseBytes int64
	// HTTPClient provides the transport, cookie jar, and an optional extra timeout.
	// The SDK copies the client fields. [http.Client.Transport] and [http.Client.Jar]
	// remain shared and owned by the caller. The SDK disables redirects regardless
	// of the supplied policy.
	HTTPClient *http.Client
	// Logger receives debug records for HTTP attempts and retry delays. Nil disables
	// logging. The caller owns the logger, its handler, and its output destination.
	// Records exclude bodies, credentials, full URLs, and raw error messages.
	Logger *slog.Logger
}

// Client sends requests to the TypeSafe API and can be shared across goroutines.
// Use [NewClient] to construct it; the zero value is not usable. Do not modify
// request maps or shared transports during a call.
type Client struct {
	apiKey           string
	baseURL          string
	defaultModel     string
	httpClient       *http.Client
	logger           *slog.Logger
	headers          http.Header
	timeout          time.Duration
	maxRetries       int
	maxResponseBytes int64
}

// NewClient creates a client after resolving defaults and validating configuration.
// Custom headers cannot replace the SDK's authentication, JSON, or identification
// headers.
func NewClient(config Config) (*Client, error) {
	key := configString(config.APIKey, "TYPESAFE_API_KEY", "")

	err := validateAPIKey(key)
	if err != nil {
		return nil, err
	}

	baseURL, err := parseBaseURL(configString(config.BaseURL, "TYPESAFE_BASE_URL", DefaultBaseURL))
	if err != nil {
		return nil, err
	}

	if config.Timeout < 0 {
		return nil, errNegativeTimeout
	}

	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}

	retries := DefaultMaxRetries
	if config.MaxRetries != nil {
		retries = *config.MaxRetries
	}

	if retries < 0 {
		return nil, errNegativeRetries
	}

	if !validResponseLimit(config.MaxResponseBytes) {
		return nil, errResponseLimit
	}

	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = DefaultMaxResponseBytes
	}

	headers, err := copyHeaders(config.Headers)
	if err != nil {
		return nil, err
	}

	return &Client{
		apiKey:           key,
		baseURL:          baseURL,
		defaultModel:     configString(config.DefaultModel, "TYPESAFE_DEFAULT_MODEL", DefaultModel),
		httpClient:       clientHTTP(config.HTTPClient),
		logger:           config.Logger,
		headers:          headers,
		timeout:          config.Timeout,
		maxRetries:       retries,
		maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

func validateAPIKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errAPIKeyRequired
	}

	if !validHeaderValue(key) {
		return errInvalidAPIKey
	}

	return nil
}

func parseBaseURL(base string) (string, error) {
	parsedURL, err := url.Parse(base)
	if err != nil {
		return "", errMalformedBaseURL
	}

	if !validBaseURL(parsedURL) {
		return "", errInvalidBaseURL
	}

	return strings.TrimRight(parsedURL.String(), "/"), nil
}

func validBaseURL(base *url.URL) bool {
	return (base.Scheme == "http" || base.Scheme == "https") && base.Hostname() != "" &&
		base.User == nil && base.RawQuery == "" && !base.ForceQuery && base.Fragment == "" && base.Opaque == ""
}

func clientHTTP(custom *http.Client) *http.Client {
	client := new(http.Client)
	if custom != nil {
		*client = *custom
	} else if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		client.Transport = transport.Clone()
	}

	// Redirecting a POST can disclose both the bearer key and evaluated content.
	// Treat every redirect as an API error and leave endpoint changes to callers.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return client
}

// CloseIdleConnections releases idle HTTP connections without interrupting
// active requests. A custom [Config.HTTPClient] shares its transport, so this call also closes
// that transport's idle connections.
func (c *Client) CloseIdleConnections() { c.httpClient.CloseIdleConnections() }

// SystemOne answers the named questions about the supplied state. Canceling ctx
// stops requests and pending retries. Request options affect only this call.
func (c *Client) SystemOne(
	ctx context.Context,
	request SystemOneRequest,
	options ...RequestOption,
) (*SystemOneResponse, error) {
	err := ctx.Err()
	if err != nil {
		return nil, err //nolint:wrapcheck // Preserve context error identity for callers.
	}

	body, err := marshalRequest(request, c.defaultModel)
	if err != nil {
		return nil, fmt.Errorf("typesafe: encode systemone request: %w", err)
	}

	meta, err := c.request(ctx, http.MethodPost, "/v1/systemone", body, options)
	if err != nil {
		return nil, err
	}

	result, err := decodeSystemOne(meta.Body)
	if err != nil {
		return nil, &ResponseError{ResponseMetadata: meta, Err: err}
	}

	result.Response = meta

	return result, nil
}

// ListModels returns the models and aliases available to the authenticated account.
func (c *Client) ListModels(ctx context.Context, options ...RequestOption) (*ListModelsResponse, error) {
	meta, err := c.request(ctx, http.MethodGet, "/v1/models", nil, options)
	if err != nil {
		return nil, err
	}

	result, err := decodeModels(meta.Body)
	if err != nil {
		return nil, &ResponseError{ResponseMetadata: meta, Err: err}
	}

	result.Response = meta

	return result, nil
}

func configString(value, env, fallback string) string {
	if value != "" {
		return value
	}

	if value = strings.TrimSpace(os.Getenv(env)); value != "" {
		return value
	}

	return fallback
}

// RequestOption configures one API call. [WithHeaders], [WithTimeout], and
// [WithMaxRetries] create options. Later options override earlier settings.
type RequestOption func(*requestConfig) error

type requestConfig struct {
	headers    http.Header
	timeout    time.Duration
	maxRetries int
}

// WithHeaders adds headers to one call and replaces matching client header values.
// It copies headers when the option is created. Custom headers cannot override
// SDK-controlled headers. Invalid headers cause the API method to return an error.
func WithHeaders(headers http.Header) RequestOption {
	cloned, err := copyHeaders(headers)

	return func(config *requestConfig) error {
		if err != nil {
			return err
		}

		for name, values := range cloned {
			config.headers[name] = append([]string(nil), values...)
		}

		return nil
	}
}

// WithTimeout limits each attempt in one call and requires a positive duration.
// A context deadline can set a shorter limit across all attempts and retry waits.
func WithTimeout(timeout time.Duration) RequestOption {
	return func(config *requestConfig) error {
		if timeout <= 0 {
			return errRequestTimeout
		}

		config.timeout = timeout

		return nil
	}
}

// WithMaxRetries limits retries after the initial request. Use zero to disable them.
func WithMaxRetries(retries int) RequestOption {
	return func(config *requestConfig) error {
		if retries < 0 {
			return errRequestRetries
		}

		config.maxRetries = retries

		return nil
	}
}

func copyHeaders(headers http.Header) (http.Header, error) {
	cloned := make(http.Header, len(headers))

	for name, values := range headers {
		if !validHeaderName(name) {
			return nil, errInvalidHeaderName
		}

		canonical := http.CanonicalHeaderKey(name)
		if _, ok := cloned[canonical]; ok {
			return nil, fmt.Errorf("%w %q with different casing", errDuplicateHeader, canonical)
		}

		for _, value := range values {
			if !validHeaderValue(value) {
				return nil, fmt.Errorf("%w %q", errInvalidHeaderValue, canonical)
			}
		}

		cloned[canonical] = append([]string(nil), values...)
	}

	return cloned, nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}

	for i := range len(name) {
		b := name[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", rune(b)) {
			continue
		}

		return false
	}

	return true
}

func validHeaderValue(value string) bool {
	for i := range len(value) {
		if value[i] == 127 || (value[i] < 32 && value[i] != '\t') {
			return false
		}
	}

	return true
}

func validResponseLimit(limit int64) bool {
	return limit >= 0 && limit < math.MaxInt64
}
