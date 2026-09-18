package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

func TestMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, body string
		models     bool
	}{
		{"empty", "", false}, {"non JSON", "<html>bad gateway</html>", false}, {"null", "null", false},
		{"array", "[]", false}, {"trailing JSON", systemOneJSON + `{}`, false},
		{"missing model", `{"answers":{},"usage":{}}`, false},
		{"null model", `{"model":null,"answers":{},"usage":{}}`, false},
		{"missing answers", `{"model":"m","usage":{}}`, false},
		{"null answers", `{"model":"m","answers":null,"usage":{}}`, false},
		{"empty answers", `{"model":"m","answers":{},"usage":{}}`, false},
		{"null usage", `{"model":"m","answers":{},"usage":null}`, false},
		{"bad usage", `{"model":"m","answers":{},"usage":{"input_tokens":1.5}}`, false},
		{"null answer", answerBody(`null`), false},
		{"missing type", answerBody(`{"noul":0}`), false},
		{"empty type", answerBody(`{"type":""}`), false},
		{"missing noul", answerBody(`{"type":"noul"}`), false},
		{"null noul", answerBody(`{"type":"noul","noul":null}`), false},
		{"string noul", answerBody(`{"type":"noul","noul":"0.5"}`), false},
		{"missing choice", answerBody(`{"type":"choice","confidence":0,"probabilities":{}}`), false},
		{"missing confidence", answerBody(`{"type":"choice","choice":"a","probabilities":{}}`), false},
		{"null probabilities", answerBody(`{"type":"choice","choice":"a","confidence":0,"probabilities":null}`), false},
		{
			"null probability",
			answerBody(`{"type":"choice","choice":"a","confidence":0,"probabilities":{"a":null}}`),
			false,
		},
		{
			"bad probability",
			answerBody(`{"type":"choice","choice":"a","confidence":0,"probabilities":{"a":"1"}}`),
			false,
		},
		{"missing score", answerBody(`{"type":"score","confidence":0,"legend":{},"probabilities":{}}`), false},
		{"missing legend", answerBody(`{"type":"score","score":0,"confidence":0,"probabilities":{}}`), false},
		{
			"null legend level",
			answerBody(`{"type":"score","score":0,"confidence":0,"legend":{"0":null},"probabilities":{}}`),
			false,
		},
		{
			"boolean legend level",
			answerBody(`{"type":"score","score":0,"confidence":0,"legend":{"0":false},"probabilities":{}}`),
			false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			checkMalformedResponse(t, test.body, test.models)
		})
	}
}

func TestMalformedModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, body string
		models     bool
	}{
		{
			"missing models",
			`{}`,
			true,
		}, {
			"null models",
			`{"models":null}`,
			true,
		}, {
			"null model item",
			`{"models":[null]}`,
			true,
		},
		{"missing model name", `{"models":[{"description":"d","release_date":"2026-01-01"}]}`, true},
		{"missing model description", `{"models":[{"name":"m","release_date":"2026-01-01"}]}`, true},
		{"invalid release date shape", `{"models":[{"name":"m","description":"d","release_date":123}]}`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkMalformedResponse(t, test.body, test.models)
		})
	}
}

func TestZeroValuesAndForwardCompatibility(t *testing.T) {
	t.Parallel()

	body := `{"model":"m","answers":{
	  "noul":{"type":"noul","noul":0,"new_field":true},
	  "choice":{"type":"choice","choice":"","confidence":0,"probabilities":{"":0}},
	  "score":{"type":"score","score":0,"confidence":0,"probabilities":{"0":0},"legend":{"0":{"large":9007199254740993}}},
	  "future":{"type":"future","value":9007199254740993}
	},"usage":{"input_tokens":0},"extension":true}`
	client := testClient(
		t,
		testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testResponse(200, nil, io.NopCloser(strings.NewReader(body))), nil
		})),
	)

	result, err := client.SystemOne(context.Background(), minimalRequest())
	if err != nil {
		t.Fatal(err)
	}

	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 0 || result.Usage.OutputTokens != nil {
		t.Fatalf("usage = %+v", result.Usage)
	}

	checkKnownAnswers(t, result.Answers)

	future, ok := result.Answers[futureKind].(*typesafe.UnknownAnswer)
	if !ok || future.Type() != futureKind {
		t.Fatalf("unknown answer = %#v", result.Answers[futureKind])
	}

	if string(future.Raw) != `{"type":"future","value":9007199254740993}` {
		t.Fatalf("unknown answer = %s", future.Raw)
	}
	// Buffer ownership must not tie a future answer to the raw response bytes.
	for i := range result.Response.Body {
		result.Response.Body[i] = 'x'
	}

	if !json.Valid(future.Raw) {
		t.Fatal("mutating raw response corrupted unknown answer")
	}
}

func TestAnswerJSONIncludesDiscriminator(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		answer any
		want   string
	}{
		{"noul pointer", &typesafe.NoulAnswer{Noul: 0.2}, `{"type":"noul","noul":0.2}`},
		{"noul value", typesafe.NoulAnswer{Noul: 0.2}, `{"type":"noul","noul":0.2}`},
		{
			"choice pointer",
			&typesafe.ChoiceAnswer{Choice: "a", Confidence: 1, Probabilities: map[string]float64{"a": 1}},
			`{"type":"choice","choice":"a","confidence":1,"probabilities":{"a":1}}`,
		},
		{
			"choice value",
			typesafe.ChoiceAnswer{Choice: "a", Confidence: 1, Probabilities: map[string]float64{"a": 1}},
			`{"type":"choice","choice":"a","confidence":1,"probabilities":{"a":1}}`,
		},
		{
			"score pointer",
			&typesafe.ScoreAnswer{
				Score:         0,
				Confidence:    1,
				Probabilities: map[string]float64{"0": 1},
				Legend:        map[string]any{"0": "Only"},
			},
			`{"type":"score","score":0,"confidence":1,"legend":{"0":"Only"},"probabilities":{"0":1}}`,
		},
		{
			"score value",
			typesafe.ScoreAnswer{
				Score:         0,
				Confidence:    1,
				Probabilities: map[string]float64{"0": 1},
				Legend:        map[string]any{"0": "Only"},
			},
			`{"type":"score","score":0,"confidence":1,"legend":{"0":"Only"},"probabilities":{"0":1}}`,
		},
		{
			"unknown pointer",
			&typesafe.UnknownAnswer{Kind: futureKind, Raw: json.RawMessage(`{"type":"future","value":true}`)},
			`{"type":"future","value":true}`,
		},
		{
			"unknown value",
			typesafe.UnknownAnswer{Kind: futureKind, Raw: json.RawMessage(`{"type":"future","value":true}`)},
			`{"type":"future","value":true}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertMarshaledJSON(t, test.answer, test.want)
		})
	}
}

func answerBody(answer string) string {
	return `{"model":"m","answers":{"q":` + answer + `},"usage":{}}`
}

func checkKnownAnswers(tb testing.TB, answers map[string]typesafe.Answer) {
	tb.Helper()

	for _, name := range []string{"noul", "choice", "score"} {
		answer, ok := answers[name]
		if !ok || answer.Type() != name {
			tb.Fatalf("incorrect answer tag for %q", name)
		}
	}

	score, isScore := answers["score"].(*typesafe.ScoreAnswer)
	if !isScore {
		tb.Fatalf("score answer has type %T", answers["score"])
	}

	legend, isLegend := score.Legend["0"].(map[string]any)
	if !isLegend {
		tb.Fatalf("legend has type %T", score.Legend["0"])
	}

	large, ok := legend["large"].(json.Number)
	if !ok || large.String() != "9007199254740993" {
		tb.Fatal("legend number lost precision")
	}
}

func checkMalformedResponse(tb testing.TB, body string, models bool) {
	tb.Helper()

	var calls int

	client := testClient(
		tb,
		testTransportConfig(testAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++

			return testResponse(
				200,
				http.Header{"X-Typesafe-Request-Id": {"bad-json"}},
				io.NopCloser(strings.NewReader(body)),
			), nil
		})),
	)

	var err error
	if models {
		_, err = client.ListModels(context.Background())
	} else {
		_, err = client.SystemOne(context.Background(), minimalRequest())
	}

	var responseErr *typesafe.ResponseError
	if !errors.As(err, &responseErr) || responseErr.StatusCode != http.StatusOK ||
		string(responseErr.Body) != body ||
		responseErr.RequestID != "bad-json" {
		tb.Fatalf("error = %#v", err)
	}

	if calls != 1 {
		tb.Errorf("malformed response retried: %d attempts", calls)
	}
}
