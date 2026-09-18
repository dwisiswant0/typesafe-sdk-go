package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	kindNoul   = "noul"
	kindChoice = "choice"
	kindScore  = "score"
)

// Question represents a [NoulQuestion], [ChoiceQuestion], [ScoreQuestion], or [RawQuestion].
// Each question encodes its type in JSON.
type Question interface {
	json.Marshaler
	question()
}

// Questions assigns a name to each question in a request. The map must contain
// at least one question.
type Questions map[string]Question

// NoulCriteria defines the yes and no outcomes. Descriptions accept strings,
// JSON objects, or arrays. A nil field is omitted from the request.
type NoulCriteria struct {
	True  any `json:"true,omitempty"`
	False any `json:"false,omitempty"`
}

// NoulQuestion asks for the probability of a yes answer. [NoulQuestion.Instructions]
// accept a string, JSON object, or array. Nil [NoulQuestion.Instructions] and
// [NoulQuestion.Criteria] are omitted.
type NoulQuestion struct {
	Instructions any           `json:"instructions,omitempty"`
	Criteria     *NoulCriteria `json:"criteria,omitempty"`
}

// MarshalJSON validates the content and encodes the question with type "noul".
func (q NoulQuestion) MarshalJSON() ([]byte, error) {
	instructions, err := optionalContent(q.Instructions)
	if err != nil {
		return nil, fmt.Errorf("instructions: %w", err)
	}

	var criteria any

	if q.Criteria != nil {
		yes, err := optionalContent(q.Criteria.True)
		if err != nil {
			return nil, fmt.Errorf("criteria.true: %w", err)
		}

		noDescription, err := optionalContent(q.Criteria.False)
		if err != nil {
			return nil, fmt.Errorf("criteria.false: %w", err)
		}

		criteria = struct {
			True  json.RawMessage `json:"true,omitempty"`
			False json.RawMessage `json:"false,omitempty"`
		}{True: yes, False: noDescription}
	}

	return marshalJSON(struct {
		Type         string          `json:"type"`
		Instructions json.RawMessage `json:"instructions,omitempty"`
		Criteria     any             `json:"criteria,omitempty"`
	}{kindNoul, instructions, criteria})
}

func (NoulQuestion) question() {}

// ChoiceQuestion asks the model to select a label from [ChoiceQuestion.Criteria].
// The criteria map must be non-nil. Each description accepts a string, JSON object,
// or array; use nil for a label without a description. [ChoiceQuestion.Instructions]
// accept the same values as [NoulQuestion.Instructions].
type ChoiceQuestion struct {
	Instructions any            `json:"instructions,omitempty"`
	Criteria     map[string]any `json:"criteria"`
}

// MarshalJSON validates the content and encodes the question with type "choice".
func (q ChoiceQuestion) MarshalJSON() ([]byte, error) {
	instructions, err := optionalContent(q.Instructions)
	if err != nil {
		return nil, fmt.Errorf("instructions: %w", err)
	}

	if q.Criteria == nil {
		return nil, errChoiceCriteria
	}

	criteria := make(map[string]json.RawMessage, len(q.Criteria))

	for name, value := range q.Criteria {
		data, err := marshalContent(value, true)
		if err != nil {
			return nil, fmt.Errorf("criteria[%q]: %w", name, err)
		}

		criteria[name] = data
	}

	return marshalJSON(struct {
		Type         string                     `json:"type"`
		Instructions json.RawMessage            `json:"instructions,omitempty"`
		Criteria     map[string]json.RawMessage `json:"criteria"`
	}{kindChoice, instructions, criteria})
}

func (ChoiceQuestion) question() {}

// ScoreQuestion asks the model to rate content against an ordered rubric.
// [ScoreQuestion.Criteria] requires at least one level, numbered from zero. Each level must be a
// string, JSON object, or array; nil levels are invalid. These rules follow the
// live OpenAPI schema and the Python SDK. [ScoreQuestion.Instructions] accept the same values
// as [NoulQuestion.Instructions].
type ScoreQuestion struct {
	Instructions any   `json:"instructions,omitempty"`
	Criteria     []any `json:"criteria"`
}

// MarshalJSON validates the rubric and encodes the question with type "score".
func (q ScoreQuestion) MarshalJSON() ([]byte, error) {
	instructions, err := optionalContent(q.Instructions)
	if err != nil {
		return nil, fmt.Errorf("instructions: %w", err)
	}

	if len(q.Criteria) == 0 {
		return nil, errScoreCriteria
	}

	criteria := make([]json.RawMessage, len(q.Criteria))

	for index, value := range q.Criteria {
		data, err := marshalContent(value, false)
		if err != nil {
			return nil, fmt.Errorf("criteria[%d]: %w", index, err)
		}

		criteria[index] = data
	}

	return marshalJSON(struct {
		Type         string            `json:"type"`
		Instructions json.RawMessage   `json:"instructions,omitempty"`
		Criteria     []json.RawMessage `json:"criteria"`
	}{kindScore, instructions, criteria})
}

func (ScoreQuestion) question() {}

// RawQuestion sends a complete JSON question, including extra fields or a future
// question type. It requires an object with the exact key "type" and a nonempty
// string value. The server validates the other fields; the SDK does not apply
// the typed question validation rules.
type RawQuestion json.RawMessage

// MarshalJSON checks the type field and returns the question's original JSON.
func (q RawQuestion) MarshalJSON() ([]byte, error) {
	var object map[string]json.RawMessage

	err := json.Unmarshal(q, &object)
	if err != nil {
		return nil, fmt.Errorf("raw question: %w", err)
	}

	var kind string

	err = json.Unmarshal(object["type"], &kind)
	if err != nil || kind == "" {
		return nil, errRawQuestionType
	}

	return q, nil
}

func (RawQuestion) question() {}

const requestFieldCount = 3

// SystemOneRequest supplies the state to evaluate and the questions to answer.
// [SystemOneRequest.State] accepts any Go value that encodes to a JSON string, object, or array.
type SystemOneRequest struct {
	State     any       `json:"state"`
	Questions Questions `json:"questions"`
	// Model selects the evaluation model. An empty value uses the client's model.
	Model string `json:"model,omitempty"`
	// ExtraBody adds top-level JSON fields and preserves nil values as null.
	// The fields state, questions, and model cannot be replaced.
	ExtraBody map[string]any `json:"-"`
}

func marshalRequest(request SystemOneRequest, defaultModel string) ([]byte, error) {
	state, err := marshalContent(request.State, false)
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}

	if len(request.Questions) == 0 {
		return nil, errQuestionsRequired
	}

	questions := make(map[string]json.RawMessage, len(request.Questions))

	for name, question := range request.Questions {
		data, err := json.Marshal(question)
		if err != nil {
			return nil, fmt.Errorf("question %q: %w", name, err)
		}

		if bytes.Equal(data, []byte("null")) {
			return nil, fmt.Errorf("question %q %w", name, errNilQuestion)
		}

		questions[name] = data
	}

	if request.Model == "" {
		request.Model = defaultModel
	}

	body := make(map[string]any, len(request.ExtraBody)+requestFieldCount)

	for key, value := range request.ExtraBody {
		switch key {
		case "state", "questions", "model":
			return nil, fmt.Errorf("extra body field %q %w", key, errExtraBodyConflict)
		}

		body[key] = value
	}

	body["state"], body["questions"], body["model"] = state, questions, request.Model

	return marshalJSON(body)
}

func optionalContent(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}

	return marshalContent(value, true)
}

func marshalContent(value any, nullable bool) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode content: %w", err)
	}

	if !contentShape(data, nullable) {
		return nil, fmt.Errorf("%w%s", errContentShape, nullSuffix(nullable))
	}

	return data, nil
}

func contentShape(data []byte, nullable bool) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return false
	}

	return data[0] == '"' || data[0] == '{' || data[0] == '[' || (nullable && bytes.Equal(data, []byte("null")))
}

func nullSuffix(nullable bool) string {
	if nullable {
		return ", or null"
	}

	return ""
}

func marshalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}

	return data, nil
}
