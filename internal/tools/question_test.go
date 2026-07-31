package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAskUserQuestionAcceptsQuestionBatch(t *testing.T) {
	input := QuestionInput{
		Intro: "Five days are available.",
		Questions: []ClarificationQuestion{
			{Question: "Transport?", Options: []QuestionOption{{Label: "Drive"}, {Label: "Rail"}}},
			{Question: "Pace?", Options: []QuestionOption{{Label: "Relaxed"}, {Label: "Full"}}},
		},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewAskUserQuestionTool()
	if validation := tool.ValidateInput(context.Background(), string(data)); !validation.Valid {
		t.Fatalf("valid batch rejected: %s", validation.Message)
	}
	output, err := tool.InvokableRun(context.Background(), string(data))
	if err != nil {
		t.Fatal(err)
	}
	var decoded QuestionInput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Questions) != 2 || decoded.Intro != input.Intro {
		t.Fatalf("unexpected output: %+v", decoded)
	}
}

func TestAskUserQuestionRejectsTooManyQuestions(t *testing.T) {
	question := ClarificationQuestion{Question: "Choose", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}}
	input := QuestionInput{Questions: []ClarificationQuestion{question, question, question, question}}
	data, _ := json.Marshal(input)
	if validation := NewAskUserQuestionTool().ValidateInput(context.Background(), string(data)); validation.Valid {
		t.Fatal("batch with four questions was accepted")
	}
}

func TestAskUserQuestionNormalizesLegacyInput(t *testing.T) {
	input := `{"question":"Transport?","options":[{"label":"Drive"},{"label":"Rail"}],"header":"Travel"}`
	tool := NewAskUserQuestionTool()
	if validation := tool.ValidateInput(context.Background(), input); !validation.Valid {
		t.Fatalf("legacy question rejected: %s", validation.Message)
	}
	output, err := tool.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["question"]; exists {
		t.Fatalf("legacy fields leaked into normalized output: %s", output)
	}
	questions, ok := decoded["questions"].([]any)
	if !ok || len(questions) != 1 {
		t.Fatalf("unexpected normalized output: %s", output)
	}
}
