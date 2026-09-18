package typesafe_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

func TestQuestionJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		question typesafe.Question
		want     string
	}{
		{"noul defaults", typesafe.NoulQuestion{Instructions: nil, Criteria: nil}, `{"type":"noul"}`},
		{
			"noul explicit null",
			typesafe.NoulQuestion{
				Instructions: json.RawMessage("null"),
				Criteria:     &typesafe.NoulCriteria{True: nil, False: nil},
			},
			`{"type":"noul","instructions":null,"criteria":{}}`,
		},
		{
			"choice",
			typesafe.ChoiceQuestion{
				Instructions: map[string]any{"task": "choose"},
				Criteria:     map[string]any{"a": nil, "b": []any{"B", true, 2}},
			},
			`{"type":"choice","instructions":{"task":"choose"},"criteria":{"a":null,"b":["B",true,2]}}`,
		},
		{
			"empty choice map",
			typesafe.ChoiceQuestion{Criteria: map[string]any{}, Instructions: nil},
			`{"type":"choice","criteria":{}}`,
		},
		{
			"one score level per live schema",
			typesafe.ScoreQuestion{Criteria: []any{"only level"}, Instructions: nil},
			`{"type":"score","criteria":["only level"]}`,
		},
		{
			"raw future type",
			typesafe.RawQuestion(`{"type":"future","extension":null}`),
			`{"type":"future","extension":null}`,
		},
		{
			"raw distinct extra field",
			typesafe.RawQuestion(`{"type":"future","Type":123}`),
			`{"type":"future","Type":123}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertMarshaledJSON(t, test.question, test.want)
		})
	}
}

func TestRawQuestionRequiresExactType(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"TYPE":"noul"}`,
		`{"Type":"choice"}`,
		`{"type":null,"TYPE":"score"}`,
		`{"type":"","Type":"future"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			_, err := json.Marshal(typesafe.RawQuestion(raw))
			if err == nil {
				t.Fatal("raw question without a nonempty exact type key was accepted")
			}
		})
	}
}

func TestInvalidRequestNeverSends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		change   func(*typesafe.SystemOneRequest)
		contains string
	}{
		{"missing state", func(request *typesafe.SystemOneRequest) { request.State = nil }, stateField},
		{"scalar state", func(request *typesafe.SystemOneRequest) { request.State = 1 }, stateField},
		{"boolean state", func(request *typesafe.SystemOneRequest) { request.State = true }, stateField},
		{
			"typed nil state",
			func(request *typesafe.SystemOneRequest) { request.State = map[string]any(nil) },
			stateField,
		},
		{
			"invalid JSON state",
			func(request *typesafe.SystemOneRequest) { request.State = json.RawMessage(`{"x":`) },
			stateField,
		},
		{"unsupported state value", func(request *typesafe.SystemOneRequest) {
			request.State = map[string]any{"x": make(chan int)}
		}, stateField},
		{"empty questions", func(request *typesafe.SystemOneRequest) { request.Questions = nil }, "at least one"},
		{"extra body collision", func(request *typesafe.SystemOneRequest) {
			request.ExtraBody = map[string]any{"model": otherLabel}
		}, "conflicts"},
		{"invalid extra body", func(request *typesafe.SystemOneRequest) {
			request.ExtraBody = map[string]any{futureKind: make(chan int)}
		}, "unsupported"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := minimalRequest()
			test.change(&request)
			assertInvalidRequest(t, request, test.contains)
		})
	}
}

func TestInvalidQuestionNeverSends(t *testing.T) {
	t.Parallel()

	var nilQuestion *typesafe.ScoreQuestion

	tests := []struct {
		name     string
		question typesafe.Question
		contains string
	}{
		{"nil question", nil, "must not be nil"},
		{"typed nil question", nilQuestion, "must not be nil"},
		{"invalid instructions", typesafe.NoulQuestion{Instructions: false, Criteria: nil}, "instructions"},
		{"invalid noul criteria", typesafe.NoulQuestion{
			Instructions: nil, Criteria: &typesafe.NoulCriteria{True: 2, False: nil},
		}, "criteria.true"},
		{"missing choice criteria", typesafe.ChoiceQuestion{Instructions: nil, Criteria: nil}, "criteria"},
		{"invalid choice criteria", typesafe.ChoiceQuestion{
			Instructions: nil, Criteria: map[string]any{"a": false},
		}, "criteria"},
		{"empty score", typesafe.ScoreQuestion{Instructions: nil, Criteria: nil}, "at least one"},
		{"null score level", typesafe.ScoreQuestion{Instructions: nil, Criteria: []any{nil}}, "criteria[0]"},
		{"raw missing type", typesafe.RawQuestion(`{}`), "type"},
		{"raw scalar", typesafe.RawQuestion(`1`), "raw question"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := minimalRequest()
			request.Questions["q"] = test.question
			assertInvalidRequest(t, request, test.contains)
		})
	}
}

func assertInvalidRequest(tb testing.TB, request typesafe.SystemOneRequest, contains string) {
	tb.Helper()

	var calls int

	client := testClient(
		tb,
		testTransportConfig(contractAPIKey, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++

			tb.Error("invalid request reached transport")

			return nil, context.Canceled
		})),
	)

	_, err := client.SystemOne(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), contains) {
		tb.Fatalf("error = %v, want %q", err, contains)
	}

	if calls != 0 {
		tb.Fatalf("transport called %d times", calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
