package typesafe

import (
	"errors"
	"fmt"
	"math"
	"net/http"
)

var (
	errAPIKeyRequired   = errors.New("typesafe: API key is required; set Config.APIKey or TYPESAFE_API_KEY")
	errInvalidAPIKey    = errors.New("typesafe: API key contains invalid HTTP header characters")
	errMalformedBaseURL = errors.New("typesafe: BaseURL must be an absolute HTTP or HTTPS URL")
	errInvalidBaseURL   = errors.New(
		"typesafe: BaseURL must be an absolute HTTP or HTTPS URL without credentials, query, or fragment",
	)
	errNegativeTimeout    = errors.New("typesafe: Timeout must not be negative")
	errNegativeRetries    = errors.New("typesafe: MaxRetries must not be negative")
	errRequestTimeout     = errors.New("typesafe: request timeout must be positive")
	errRequestRetries     = errors.New("typesafe: request MaxRetries must not be negative")
	errInvalidHeaderName  = errors.New("typesafe: invalid HTTP header name")
	errNilRequestOption   = errors.New("typesafe: request option must not be nil")
	errChoiceCriteria     = errors.New("choice criteria must be a non-nil map")
	errScoreCriteria      = errors.New("score criteria requires at least one level")
	errRawQuestionType    = errors.New("raw question requires a nonempty string type")
	errQuestionsRequired  = errors.New("at least one question is required")
	errAnswersRequired    = errors.New("answers must contain at least one answer")
	errAnswerTypeRequired = errors.New("type must not be empty")
	errExpectedObject     = errors.New("expected a JSON object")
	errDuplicateHeader    = errors.New("typesafe: duplicate HTTP header")
	errInvalidHeaderValue = errors.New("typesafe: invalid value for HTTP header")
	errNilQuestion        = errors.New("must not be nil")
	errExtraBodyConflict  = errors.New("conflicts with a request field")
	errContentShape       = errors.New("expected a string, JSON object, or array")
	errLegendShape        = errors.New("expected a string, object, or array")
	errNullProbability    = errors.New("must not be null")
	errRequiredField      = errors.New("is required and must not be null")
	errResponseLimit      = fmt.Errorf(
		"typesafe: MaxResponseBytes must be between 1 and %d, or zero for the default",
		int64(math.MaxInt64-1),
	)
)

// ErrResponseTooLarge reports that a body exceeds [Config.MaxResponseBytes].
// The enclosing [ResponseError] retains the HTTP metadata and the body prefix
// up to the configured limit.
var ErrResponseTooLarge = errors.New("typesafe: response body exceeds configured limit")

// APIError reports a non-2xx HTTP response after retries. Use [errors.As] to read
// [ResponseMetadata.StatusCode], [ResponseMetadata.Header], [ResponseMetadata.RequestID],
// and [ResponseMetadata.Body]. The error message omits the body because a server
// validation error can contain sensitive request data.
type APIError struct {
	ResponseMetadata

	Method string
	Path   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("typesafe: %s %s: HTTP %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
}

// ResponseError reports a decoding failure, an oversized body, or a body read
// failure. A read failure retains the partial body and HTTP metadata. Use
// [errors.Is] or [errors.As] to inspect the cause. HTTP status failures use [APIError].
type ResponseError struct {
	ResponseMetadata

	Err error
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("typesafe: read response (HTTP %d): %v", e.StatusCode, e.Err)
}

// Unwrap returns the cause: a decoding error, a size limit, or a body read failure.
func (e *ResponseError) Unwrap() error { return e.Err }
