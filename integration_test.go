//go:build integration

package typesafe_test

import (
	"context"
	"os"
	"testing"
	"time"

	typesafe "go.dw1.io/typesafe-sdk-go" //nolint:depguard // External tests must import the SDK they verify.
)

// TestLiveAPI sends one evaluation to the configured service and can incur usage.
// Run it explicitly with: go test -tags=integration -run TestLiveAPI -v
func TestLiveAPI(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("TYPESAFE_API_KEY is required for the live API test")
	}
	client := testClient(
		t,
		testConfig("", ""),
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	models, err := client.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models.Models) == 0 {
		t.Fatal("account has no available models")
	}
	result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: "I was charged twice. Please help.",
		Questions: typesafe.Questions{
			"billing": typesafe.NoulQuestion{Instructions: "Is this about billing?", Criteria: nil},
			"category": typesafe.ChoiceQuestion{
				Instructions: "Choose the topic",
				Criteria:     map[string]any{"billing": nil, "other": nil},
			},
			"urgency": typesafe.ScoreQuestion{
				Instructions: "Rate urgency",
				Criteria:     []any{"Can wait", "Needs attention now"},
			},
		},
		Model: "", ExtraBody: nil})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"billing": "noul", "category": "choice", "urgency": "score"} {
		answer, ok := result.Answers[name]
		if !ok || answer.Type() != want {
			t.Errorf("answer %q has unexpected type", name)
		}
	}
}
