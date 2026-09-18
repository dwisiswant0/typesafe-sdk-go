package typesafe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

const benchmarkBilling = "billing"

func BenchmarkMarshalRequest(b *testing.B) {
	for _, count := range []int{1, 16, 128} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			request := benchmarkRequest(count)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				_, err := marshalRequest(request, DefaultModel)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeSystemOne(b *testing.B) {
	for _, count := range []int{1, 16, 128} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			data := benchmarkResponse(b, count)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				result, err := decodeSystemOne(data)
				if err != nil || len(result.Answers) != count {
					b.Fatalf("decode: %v", err)
				}
			}
		})
	}
}

// BenchmarkSystemOne measures request encoding, response parsing, and HTTP over
// loopback. The result excludes TypeSafe service time and internet latency.
func BenchmarkSystemOne(b *testing.B) {
	data := benchmarkResponse(b, 16)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		_, _ = writer.Write(data)
	}))
	defer server.Close()

	var config Config

	config.APIKey = "benchmark-key"
	config.BaseURL = server.URL

	client, err := NewClient(config)
	if err != nil {
		b.Fatal(err)
	}
	defer client.CloseIdleConnections()

	request := benchmarkRequest(16)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, err := client.SystemOne(context.Background(), request)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRequest(count int) SystemOneRequest {
	questions := make(Questions, count)

	for index := range count {
		switch index % 3 {
		case 0:
			questions[strconv.Itoa(index)] = NoulQuestion{Instructions: "Is this urgent?", Criteria: nil}
		case 1:
			questions[strconv.Itoa(index)] = ChoiceQuestion{
				Instructions: "Which team?",
				Criteria:     map[string]any{benchmarkBilling: nil, "other": nil},
			}
		case 2:
			questions[strconv.Itoa(index)] = ScoreQuestion{
				Instructions: "Rate severity",
				Criteria:     []any{"Low", "High"},
			}
		}
	}

	return SystemOneRequest{
		State:     map[string]any{"document": "I was charged twice. Please help."},
		Questions: questions,
		Model:     "",
		ExtraBody: nil,
	}
}

func benchmarkResponse(tb testing.TB, count int) []byte {
	tb.Helper()

	answers := make(map[string]Answer, count)

	for index := range count {
		switch index % 3 {
		case 0:
			answers[strconv.Itoa(index)] = &NoulAnswer{Noul: 0.92}
		case 1:
			answers[strconv.Itoa(index)] = &ChoiceAnswer{
				Choice:        benchmarkBilling,
				Confidence:    0.9,
				Probabilities: map[string]float64{benchmarkBilling: 0.95, "other": 0.05},
			}
		case 2:
			answers[strconv.Itoa(index)] = &ScoreAnswer{
				Score:         0.7,
				Confidence:    0.6,
				Probabilities: map[string]float64{"0": 0.3, "1": 0.7},
				Legend:        map[string]any{"0": "Low", "1": "High"},
			}
		}
	}

	input, output := int64(300), int64(48)

	data, err := json.Marshal(
		SystemOneResponse{
			Model:    DefaultModel,
			Answers:  answers,
			Usage:    Usage{InputTokens: &input, OutputTokens: &output},
			Response: ResponseMetadata{StatusCode: 0, Header: nil, RequestID: "", Body: nil},
		},
	)
	if err != nil {
		tb.Fatal(err)
	}

	return data
}
