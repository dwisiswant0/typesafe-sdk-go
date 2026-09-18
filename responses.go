package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// ResponseMetadata contains the final HTTP status, headers, request ID, and body.
// The SDK reads and closes the network body before returning. [ResponseMetadata.Body]
// owns the buffered response bytes. Headers and bodies can contain sensitive data.
type ResponseMetadata struct {
	StatusCode int
	Header     http.Header
	RequestID  string
	Body       []byte
}

// Answer holds a pointer to [NoulAnswer], [ChoiceAnswer], [ScoreAnswer], or [UnknownAnswer].
type Answer interface {
	Type() string
	answer()
}

// NoulAnswer reports the probability of yes on a scale from zero to one.
//
//nolint:recvcheck // Values marshal to JSON; only pointers implement [Answer].
type NoulAnswer struct {
	Noul float64 `json:"noul"`
}

// Type returns "noul".
func (*NoulAnswer) Type() string { return kindNoul }

// MarshalJSON encodes the answer with type "noul".
func (a NoulAnswer) MarshalJSON() ([]byte, error) {
	type fields NoulAnswer

	return marshalJSON(struct {
		fields

		Type string `json:"type"`
	}{fields: fields(a), Type: kindNoul})
}

func (*NoulAnswer) answer() {}

// ChoiceAnswer reports the selected label, its confidence, and each label's
// probability.
//
//nolint:recvcheck // Values marshal to JSON; only pointers implement [Answer].
type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Type returns "choice".
func (*ChoiceAnswer) Type() string { return kindChoice }

// MarshalJSON encodes the answer with type "choice".
func (a ChoiceAnswer) MarshalJSON() ([]byte, error) {
	type fields ChoiceAnswer

	return marshalJSON(struct {
		fields

		Type string `json:"type"`
	}{fields: fields(a), Type: kindChoice})
}

func (*ChoiceAnswer) answer() {}

// ScoreAnswer reports the expected score, confidence, rubric, and probabilities.
// [ScoreAnswer.Score] is a probability-weighted value and can fall between integer levels.
// Map keys preserve the API's string level indices. [ScoreAnswer.Legend] values are
// strings, maps, or slices; nested numbers use [json.Number].
//
//nolint:recvcheck // Values marshal to JSON; only pointers implement [Answer].
type ScoreAnswer struct {
	Score         float64            `json:"score"`
	Confidence    float64            `json:"confidence"`
	Legend        map[string]any     `json:"legend"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Type returns "score".
func (*ScoreAnswer) Type() string { return kindScore }

// MarshalJSON encodes the answer with type "score".
func (a ScoreAnswer) MarshalJSON() ([]byte, error) {
	type fields ScoreAnswer

	return marshalJSON(struct {
		fields

		Type string `json:"type"`
	}{fields: fields(a), Type: kindScore})
}

func (*ScoreAnswer) answer() {}

// UnknownAnswer retains an answer whose type this SDK version does not recognize.
//
//nolint:recvcheck // Values marshal to JSON; only pointers implement [Answer].
type UnknownAnswer struct {
	Kind string
	Raw  json.RawMessage
}

// Type returns the type reported by the API.
func (a *UnknownAnswer) Type() string { return a.Kind }

// MarshalJSON returns the original answer JSON, including fields the SDK does not model.
func (a UnknownAnswer) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(a.Raw)
	if err != nil {
		return nil, fmt.Errorf("encode unknown answer: %w", err)
	}

	return data, nil
}

func (*UnknownAnswer) answer() {}

// Usage reports token counts. A nil pointer means the server omitted the count
// or returned null. A pointer to zero means zero tokens. Older API responses can
// omit counts, so nil and zero remain distinct.
//
//nolint:tagliatelle // The API requires snake_case token-count fields.
type Usage struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
}

// SystemOneResponse holds the named answers, model, and token usage for an evaluation.
type SystemOneResponse struct {
	Model    string            `json:"model"`
	Answers  map[string]Answer `json:"answers"`
	Usage    Usage             `json:"usage"`
	Response ResponseMetadata  `json:"-"`
}

// Model describes a model or alias available to the account. [Model.ReleaseDate] keeps
// the string returned by the API.
type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"` //nolint:tagliatelle // Preserve the API field name.
}

// ListModelsResponse lists the models available to the authenticated account.
type ListModelsResponse struct {
	Models   []Model          `json:"models"`
	Response ResponseMetadata `json:"-"`
}

func decodeSystemOne(data []byte) (*SystemOneResponse, error) {
	object, err := decodeObject(data)
	if err != nil {
		return nil, err
	}

	result := new(SystemOneResponse)

	err = requiredField(object, "model", &result.Model)
	if err != nil {
		return nil, err
	}

	err = requiredField(object, "usage", &result.Usage)
	if err != nil {
		return nil, err
	}

	var answers map[string]json.RawMessage

	err = requiredField(object, "answers", &answers)
	if err != nil {
		return nil, err
	}

	if len(answers) == 0 {
		return nil, errAnswersRequired
	}

	result.Answers = make(map[string]Answer, len(answers))

	for name, raw := range answers {
		answer, err := decodeAnswer(raw)
		if err != nil {
			return nil, fmt.Errorf("answers[%q]: %w", name, err)
		}

		result.Answers[name] = answer
	}

	return result, nil
}

//nolint:ireturn // The discriminator selects one of several [Answer] implementations.
func decodeAnswer(data []byte) (Answer, error) {
	object, err := decodeObject(data)
	if err != nil {
		return nil, err
	}

	var kind string

	err = requiredField(object, "type", &kind)
	if err != nil {
		return nil, err
	}

	switch kind {
	case kindNoul:
		answer := new(NoulAnswer)

		err := requiredField(object, kindNoul, &answer.Noul)
		if err != nil {
			return nil, err
		}

		return answer, nil
	case kindChoice:
		return decodeChoice(object)
	case kindScore:
		return decodeScore(object)
	case "":
		return nil, errAnswerTypeRequired
	default:
		return &UnknownAnswer{Kind: kind, Raw: bytes.Clone(data)}, nil
	}
}

func decodeChoice(object map[string]json.RawMessage) (*ChoiceAnswer, error) {
	answer := new(ChoiceAnswer)

	err := requiredField(object, kindChoice, &answer.Choice)
	if err != nil {
		return nil, err
	}

	err = requiredField(object, "confidence", &answer.Confidence)
	if err != nil {
		return nil, err
	}

	answer.Probabilities, err = decodeProbabilities(object)

	return answer, err
}

func decodeScore(object map[string]json.RawMessage) (*ScoreAnswer, error) {
	answer := new(ScoreAnswer)

	err := requiredField(object, kindScore, &answer.Score)
	if err != nil {
		return nil, err
	}

	err = requiredField(object, "confidence", &answer.Confidence)
	if err != nil {
		return nil, err
	}

	answer.Legend, err = decodeLegend(object)
	if err != nil {
		return nil, err
	}

	answer.Probabilities, err = decodeProbabilities(object)

	return answer, err
}

func decodeLegend(object map[string]json.RawMessage) (map[string]any, error) {
	var rawLegend map[string]json.RawMessage

	err := requiredField(object, "legend", &rawLegend)
	if err != nil {
		return nil, err
	}

	legend := make(map[string]any, len(rawLegend))

	for key, raw := range rawLegend {
		if !contentShape(raw, false) {
			return nil, fmt.Errorf("legend[%q]: %w", key, errLegendShape)
		}

		var value any

		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()

		err := decoder.Decode(&value)
		if err != nil {
			return nil, fmt.Errorf("legend[%q]: %w", key, err)
		}

		legend[key] = value
	}

	return legend, nil
}

func decodeProbabilities(object map[string]json.RawMessage) (map[string]float64, error) {
	var raw map[string]*float64

	err := requiredField(object, "probabilities", &raw)
	if err != nil {
		return nil, err
	}

	values := make(map[string]float64, len(raw))

	for key, value := range raw {
		if value == nil {
			return nil, fmt.Errorf("probabilities[%q] %w", key, errNullProbability)
		}

		values[key] = *value
	}

	return values, nil
}

func decodeModels(data []byte) (*ListModelsResponse, error) {
	object, err := decodeObject(data)
	if err != nil {
		return nil, err
	}

	var models []json.RawMessage

	err = requiredField(object, "models", &models)
	if err != nil {
		return nil, err
	}

	result := new(ListModelsResponse)
	result.Models = make([]Model, len(models))

	for index, raw := range models {
		model, err := decodeModel(raw)
		if err != nil {
			return nil, fmt.Errorf("models[%d]: %w", index, err)
		}

		result.Models[index] = model
	}

	return result, nil
}

func decodeModel(data []byte) (Model, error) {
	object, err := decodeObject(data)
	if err != nil {
		return Model{}, err
	}

	var model Model

	err = requiredField(object, "name", &model.Name)
	if err != nil {
		return Model{}, err
	}

	err = requiredField(object, "description", &model.Description)
	if err != nil {
		return Model{}, err
	}

	err = requiredField(object, "release_date", &model.ReleaseDate)
	if err != nil {
		return Model{}, err
	}

	return model, nil
}

func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage

	err := json.Unmarshal(data, &object)
	if err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}

	if object == nil {
		return nil, errExpectedObject
	}

	return object, nil
}

func requiredField(object map[string]json.RawMessage, name string, target any) error {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%s %w", name, errRequiredField)
	}

	err := json.Unmarshal(raw, target)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}
