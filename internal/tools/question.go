package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const AskUserQuestionToolName = "AskUserQuestion"

// ========== AskUserQuestionTool ==========

// QuestionOption represents a single choice option
type QuestionOption struct {
	Label       string `json:"label"`                 // Display label
	Description string `json:"description,omitempty"` // Option description
}

// QuestionInput for ask user question tool
type ClarificationQuestion struct {
	Question    string           `json:"question"`         // The question to ask
	Options     []QuestionOption `json:"options"`          // Answer options
	Header      string           `json:"header,omitempty"` // Short header for the question
	MultiSelect bool             `json:"multi_select"`     // Allow multiple selections
}

// QuestionInput groups related blocking questions into one user interaction.
type QuestionInput struct {
	Intro     string                  `json:"intro,omitempty"`
	Questions []ClarificationQuestion `json:"questions,omitempty"`

	// Legacy single-question fields are accepted for existing agents and old
	// conversation context. Tool metadata only advertises the batch format.
	Question    string           `json:"question,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
	Header      string           `json:"header,omitempty"`
	MultiSelect bool             `json:"multi_select,omitempty"`
}

func (q *QuestionInput) normalize() {
	if len(q.Questions) == 0 && q.Question != "" {
		q.Questions = []ClarificationQuestion{{
			Question:    q.Question,
			Options:     q.Options,
			Header:      q.Header,
			MultiSelect: q.MultiSelect,
		}}
	}
	q.Question = ""
	q.Options = nil
	q.Header = ""
	q.MultiSelect = false
}

// QuestionOutput for ask user question result
type QuestionOutput struct {
	Answer  string   `json:"answer"`  // Selected option label
	Answers []string `json:"answers"` // Multiple selected labels (if multi_select)
	Success bool     `json:"success"` // Whether an answer was received
}

// AskUserQuestionTool asks the user multiple choice questions
type AskUserQuestionTool struct{}

func NewAskUserQuestionTool() *AskUserQuestionTool {
	return &AskUserQuestionTool{}
}

func init() {
	GlobalRegistry.Register(ToolMeta{
		Name:           AskUserQuestionToolName,
		Desc:           "Pause the current run and ask the user 1-3 related preference or clarification questions in one interactive card. Use only for information the user must decide, not facts that can be researched.",
		IsReadOnly:     false,
		MaxResultChars: 1000,
		DefaultRisk:    "low",
		Creator: func(basePath string) interface{} {
			return NewAskUserQuestionTool()
		},
	})
}

func (t *AskUserQuestionTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: AskUserQuestionToolName,
		Desc: "Pause the current run and ask the user 1-3 related preference or clarification questions in one interactive card. The tool returns directly to the user, so call it only after completing any research needed for this turn.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"intro": {
				Type:     schema.String,
				Desc:     "Optional concise summary of relevant findings and why these choices are needed; it remains in conversation history",
				Required: false,
			},
			"questions": {
				Type:     schema.Array,
				Desc:     "One to three independent questions",
				Required: true,
				ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
					"question":     {Type: schema.String, Desc: "Question shown to the user", Required: true},
					"header":       {Type: schema.String, Desc: "Short category label, at most 12 characters", Required: false},
					"multi_select": {Type: schema.Boolean, Desc: "Whether multiple options may be selected", Required: false},
					"options": {Type: schema.Array, Desc: "Two to four answer options", Required: true, ElemInfo: &schema.ParameterInfo{
						Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
							"label":       {Type: schema.String, Desc: "Short answer label", Required: true},
							"description": {Type: schema.String, Desc: "Impact or tradeoff", Required: false},
						},
					}},
				}},
			},
		}),
	}, nil
}

func (t *AskUserQuestionTool) ValidateInput(ctx context.Context, input string) *ValidationResult {
	var questionInput QuestionInput
	if err := json.Unmarshal([]byte(input), &questionInput); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	questionInput.normalize()
	if len(questionInput.Questions) < 1 || len(questionInput.Questions) > 3 {
		return &ValidationResult{Valid: false, Message: "questions must contain 1 to 3 items", ErrorCode: 2}
	}
	for index, question := range questionInput.Questions {
		if question.Question == "" {
			return &ValidationResult{Valid: false, Message: fmt.Sprintf("question %d text is required", index+1), ErrorCode: 3}
		}
		if len(question.Options) < 2 || len(question.Options) > 4 {
			return &ValidationResult{Valid: false, Message: fmt.Sprintf("question %d must have 2 to 4 options", index+1), ErrorCode: 4}
		}
	}
	return &ValidationResult{Valid: true}
}

func (t *AskUserQuestionTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	var questionInput QuestionInput
	if err := json.Unmarshal([]byte(input), &questionInput); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	questionInput.normalize()

	result, _ := json.Marshal(questionInput)
	return string(result), nil
}
