package typesafe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

const (
	testAPIKey       = "key"
	contractAPIKey   = "test-key"
	exampleAPIKey    = "example-key"
	billingLabel     = "billing"
	otherLabel       = "other"
	stateField       = "state"
	futureKind       = "future"
	testHeader       = "X-Test"
	acceptHeader     = "Accept"
	jsonContentType  = "application/json"
	retryAfterHeader = "Retry-After"
)

const systemOneJSON = `{
  "model":"jev-latest",
  "answers":{
    "urgent":{"type":"noul","noul":0.92},
    "department":{"type":"choice","choice":"billing","confidence":0.8,"probabilities":{"billing":0.9,"other":0.1}},
    "severity":{"type":"score","score":1.6,"confidence":0.78,
      "legend":{"0":"Calm","1":{"label":"Frustrated"},"2":["Angry"]},
      "probabilities":{"0":0.05,"1":0.3,"2":0.65}}
  },
  "usage":{"input_tokens":312,"output_tokens":48},
  "future_field":true
}`

func TestSystemOneContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		checkSystemOneRequest(t, request)
		writer.Header().Set("X-Typesafe-Request-Id", "req-123")
		_, _ = io.WriteString(writer, systemOneJSON)
	}))
	defer server.Close()

	client := testClient(
		t,
		testConfig(contractAPIKey, server.URL+"/gateway/"),
	)

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State: map[string]any{"document": "I was charged twice."},
		Questions: typesafe.Questions{
			"urgent": typesafe.NoulQuestion{
				Instructions: "Is this urgent?",
				Criteria:     &typesafe.NoulCriteria{True: "Time-sensitive", False: "Can wait"},
			},
			"department": typesafe.ChoiceQuestion{
				Criteria:     map[string]any{billingLabel: nil, otherLabel: map[string]any{"description": "Other"}},
				Instructions: nil,
			},
			"severity": typesafe.ScoreQuestion{
				Instructions: []string{"Rate severity"},
				Criteria:     []any{"Calm", map[string]any{"label": "Frustrated"}, []string{"Angry"}},
			},
		},
		ExtraBody: map[string]any{"future_option": nil},
		Model:     ""})
	if err != nil {
		t.Fatal(err)
	}

	checkSystemOneMetadata(t, response)
	checkSystemOneAnswers(t, response.Answers)

	assertJSON(t, response.Response.Body, systemOneJSON)
}

func TestListModelsContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/models" || request.ContentLength != 0 {
			t.Errorf("unexpected request: %s %s, length %d", request.Method, request.URL.Path, request.ContentLength)
		}

		_, _ = io.WriteString(
			writer,
			`{"models":[{"name":"jev-latest","description":"System One model","release_date":"2026-09-15"}]}`,
		)
	}))
	defer server.Close()

	client := testClient(
		t,
		testConfig(contractAPIKey, server.URL),
	)

	response, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(response.Models) != 1 || response.Models[0].Name != "jev-latest" ||
		response.Models[0].ReleaseDate != "2026-09-15" {
		t.Fatalf("unexpected models: %+v", response.Models)
	}
}

func testClient(tb testing.TB, config typesafe.Config) *typesafe.Client {
	tb.Helper()

	client, err := typesafe.NewClient(config)
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(client.CloseIdleConnections)

	return client
}

func assertJSON(tb testing.TB, got []byte, want string) {
	tb.Helper()

	var actual, expected any

	err := json.Unmarshal(got, &actual)
	if err != nil {
		tb.Fatal(err)
	}

	err = json.Unmarshal([]byte(want), &expected)
	if err != nil {
		tb.Fatal(err)
	}

	if !reflect.DeepEqual(actual, expected) {
		tb.Errorf("JSON = %s, want %s", got, want)
	}
}

func testConfig(apiKey, baseURL string) typesafe.Config {
	var config typesafe.Config

	config.APIKey = apiKey
	config.BaseURL = baseURL

	return config
}

func testTransportConfig(apiKey string, transport http.RoundTripper) typesafe.Config {
	config := testConfig(apiKey, "")
	config.HTTPClient = new(http.Client)
	config.HTTPClient.Transport = transport

	return config
}

func testResponse(status int, headers http.Header, body io.ReadCloser) *http.Response {
	response := new(http.Response)
	response.StatusCode = status
	response.Header = headers
	response.Body = body

	return response
}

func checkSystemOneRequest(tb testing.TB, request *http.Request) {
	tb.Helper()

	if request.Method != http.MethodPost || request.URL.Path != "/gateway/v1/systemone" {
		tb.Errorf("unexpected endpoint: %s %s", request.Method, request.URL.Path)
	}

	for name, want := range map[string]string{
		"Authorization": "Bearer test-key", "Content-Type": jsonContentType, acceptHeader: jsonContentType,
	} {
		if got := request.Header.Get(name); got != want {
			tb.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		tb.Error(err)
	}

	assertJSON(tb, body, `{
	  "state":{"document":"I was charged twice."},"model":"jev-latest",
	  "questions":{
	    "urgent":{"type":"noul","instructions":"Is this urgent?","criteria":{"true":"Time-sensitive","false":"Can wait"}},
	    "department":{"type":"choice","criteria":{"billing":null,"other":{"description":"Other"}}},
	    "severity":{"type":"score","instructions":["Rate severity"],"criteria":["Calm",{"label":"Frustrated"},["Angry"]]}
	  },"future_option":null
	}`)
}

func checkSystemOneMetadata(tb testing.TB, response *typesafe.SystemOneResponse) {
	tb.Helper()

	if response.Model != "jev-latest" || response.Response.RequestID != "req-123" ||
		response.Response.StatusCode != http.StatusOK {
		tb.Fatalf("unexpected response metadata: %+v", response)
	}

	if response.Usage.InputTokens == nil || *response.Usage.InputTokens != 312 || response.Usage.OutputTokens == nil ||
		*response.Usage.OutputTokens != 48 {
		tb.Fatalf("unexpected usage: %+v", response.Usage)
	}
}

func checkSystemOneAnswers(tb testing.TB, answers map[string]typesafe.Answer) {
	tb.Helper()

	if answer, ok := answers["urgent"].(*typesafe.NoulAnswer); !ok || answer.Noul != 0.92 {
		tb.Fatalf("noul answer: %#v", answers["urgent"])
	}

	if answer, ok := answers["department"].(*typesafe.ChoiceAnswer); !ok || answer.Choice != billingLabel ||
		answer.Probabilities[billingLabel] != 0.9 {
		tb.Fatalf("choice answer: %#v", answers["department"])
	}

	if answer, ok := answers["severity"].(*typesafe.ScoreAnswer); !ok || answer.Score != 1.6 ||
		answer.Probabilities["2"] != 0.65 {
		tb.Fatalf("score answer: %#v", answers["severity"])
	}
}

func assertMarshaledJSON(tb testing.TB, value any, want string) {
	tb.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		tb.Fatal(err)
	}

	assertJSON(tb, data, want)
}
