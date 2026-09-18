package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

func ExampleClient_SystemOne() {
	// A local server lets this example run without TypeSafe API credentials.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(
			writer,
			`{"model":"jev-latest","answers":{"category":{"type":"choice","choice":"billing",`+
				`"confidence":0.95,"probabilities":{"billing":0.98,"other":0.02}}},`+
				`"usage":{"input_tokens":20,"output_tokens":5}}`,
		)
	}))
	defer server.Close()

	var config typesafe.Config

	config.APIKey = exampleAPIKey
	config.BaseURL = server.URL

	client, err := typesafe.NewClient(config)
	if err != nil {
		fmt.Println(err)

		return
	}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: map[string]any{"document": "I was charged twice. Please help."},
		Questions: typesafe.Questions{
			"category": typesafe.ChoiceQuestion{
				Instructions: "What is this ticket about?",
				Criteria:     map[string]any{billingLabel: nil, otherLabel: nil},
			},
		},
		Model: "", ExtraBody: nil})
	if err != nil {
		fmt.Println(err)

		return
	}

	answer, ok := response.Answers["category"].(*typesafe.ChoiceAnswer)
	if !ok {
		fmt.Println("unexpected answer type")

		return
	}

	fmt.Println(answer.Choice)
	// Output: billing
}

func ExampleClient_ListModels() {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(
			writer,
			`{"models":[{"name":"jev-latest","description":"System One model","release_date":"2026-09-15"}]}`,
		)
	}))
	defer server.Close()

	var config typesafe.Config

	config.APIKey = exampleAPIKey
	config.BaseURL = server.URL

	client, err := typesafe.NewClient(config)
	if err != nil {
		fmt.Println(err)

		return
	}
	defer client.CloseIdleConnections()

	response, err := client.ListModels(context.Background())
	if err != nil {
		fmt.Println(err)

		return
	}

	for _, model := range response.Models {
		fmt.Println(model.Name)
	}
	// Output: jev-latest
}

func ExampleAPIError() {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Typesafe-Request-Id", "request-123")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, `{"detail":"Invalid API key"}`)
	}))
	defer server.Close()

	var config typesafe.Config

	config.APIKey = exampleAPIKey
	config.BaseURL = server.URL

	client, err := typesafe.NewClient(config)
	if err != nil {
		fmt.Println(err)

		return
	}
	defer client.CloseIdleConnections()

	_, err = client.ListModels(context.Background())

	if apiErr, ok := errors.AsType[*typesafe.APIError](err); ok {
		fmt.Println(apiErr.StatusCode, apiErr.RequestID)
	}
	// Output: 401 request-123
}
