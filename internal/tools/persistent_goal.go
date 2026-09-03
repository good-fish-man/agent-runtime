package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	orchestrationv2 "github.com/good-fish-man/athena-protocol/protocol/orchestration/v2"
)

const PersistentGoalCreateToolName = "PersistentGoalCreate"

type PersistentGoalTaskInput struct {
	TaskID               string   `json:"task_id"`
	ParentTaskID         string   `json:"parent_task_id,omitempty"`
	Depth                int      `json:"depth"`
	Specialist           string   `json:"specialist"`
	Objective            string   `json:"objective"`
	DependsOn            []string `json:"depends_on,omitempty"`
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	WorldSliceRefs       []string `json:"world_slice_refs,omitempty"`
}

type PersistentGoalCreateInput struct {
	Objective       string                    `json:"objective"`
	Constraints     []string                  `json:"constraints,omitempty"`
	SuccessCriteria []string                  `json:"success_criteria"`
	Deadline        string                    `json:"deadline,omitempty"`
	Tasks           []PersistentGoalTaskInput `json:"tasks"`
}

type PersistentGoalCreateTool struct {
	userID    string
	agentID   string
	sessionID string
	client    *http.Client
}

func NewPersistentGoalCreateTool(userID, agentID, sessionID string) *PersistentGoalCreateTool {
	return &PersistentGoalCreateTool{userID: userID, agentID: agentID, sessionID: sessionID, client: http.DefaultClient}
}

func (t *PersistentGoalCreateTool) Info(context.Context) (*schema.ToolInfo, error) {
	stringArray := func(description string, required bool) *schema.ParameterInfo {
		return &schema.ParameterInfo{Type: schema.Array, Desc: description, Required: required, ElemInfo: &schema.ParameterInfo{Type: schema.String}}
	}
	return &schema.ToolInfo{
		Name: PersistentGoalCreateToolName,
		Desc: "Create a durable, resumable goal only when the user explicitly asks for long-running, cross-day, cross-device, or background work. Submit a finite declarative specialist graph. Never generate executable code, hidden agents, credentials, or unbounded loops.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"objective":        {Type: schema.String, Desc: "The complete user goal, preserved as one objective", Required: true},
			"constraints":      stringArray("Explicit user constraints and safety boundaries", false),
			"success_criteria": stringArray("Observable conditions that prove the goal is complete", true),
			"deadline":         {Type: schema.String, Desc: "Optional RFC3339 deadline", Required: false},
			"tasks": {
				Type: schema.Array, Desc: "A finite acyclic graph with at most 64 specialist tasks", Required: true,
				ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
					"task_id":               {Type: schema.String, Desc: "Stable ID referenced by depends_on", Required: true},
					"parent_task_id":        {Type: schema.String, Desc: "Optional parent task ID", Required: false},
					"depth":                 {Type: schema.Integer, Desc: "Graph depth from 1 to 4", Required: true},
					"specialist":            {Type: schema.String, Desc: "RESEARCH, BROWSER, DESKTOP, FILE, or SYNTHESIS", Required: true},
					"objective":             {Type: schema.String, Desc: "Bounded outcome for this specialist", Required: true},
					"depends_on":            stringArray("Task IDs that must complete first", false),
					"required_capabilities": stringArray("Registered capability IDs required for execution", false),
					"world_slice_refs":      stringArray("Only world-state keys this specialist may receive", false),
				}},
			},
		}),
	}, nil
}

func (t *PersistentGoalCreateTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	if strings.TrimSpace(t.userID) == "" || strings.TrimSpace(t.agentID) == "" {
		return "", fmt.Errorf("persistent goals require an authenticated user and selected agent")
	}
	var request PersistentGoalCreateInput
	if err := json.Unmarshal([]byte(input), &request); err != nil {
		return "", fmt.Errorf("invalid persistent goal input: %w", err)
	}
	if err := validatePersistentGoalInput(request); err != nil {
		return "", err
	}
	criteria := make([]map[string]any, 0, len(request.SuccessCriteria))
	for _, description := range request.SuccessCriteria {
		criteria = append(criteria, map[string]any{"description": strings.TrimSpace(description), "required": true})
	}
	tasks := make([]map[string]any, 0, len(request.Tasks))
	for _, item := range request.Tasks {
		tasks = append(tasks, map[string]any{
			"task_id": item.TaskID, "parent_task_id": item.ParentTaskID, "depth": item.Depth,
			"specialist": item.Specialist, "objective": item.Objective, "depends_on": item.DependsOn,
			"required_capabilities": item.RequiredCapabilities, "world_slice_refs": item.WorldSliceRefs,
		})
	}
	payload := map[string]any{
		"user_id": t.userID, "agent_id": t.agentID, "conversation_id": t.sessionID,
		"objective": strings.TrimSpace(request.Objective), "constraints": request.Constraints,
		"success_criteria": criteria, "tasks": tasks,
	}
	if request.Deadline != "" {
		payload["deadline"] = request.Deadline
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode persistent goal: %w", err)
	}
	endpoint := strings.TrimSpace(os.Getenv(constant.EnvRuntimeClientGoalURL))
	if endpoint == "" {
		endpoint = constant.DefaultRuntimeClientInternalGoalURL
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build persistent goal request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set(constant.HeaderAthenaInternalToken, strings.TrimSpace(os.Getenv(constant.EnvInternalServiceToken)))
	response, err := t.client.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("create persistent goal: %w", err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if readErr != nil {
		return "", fmt.Errorf("read persistent goal response: %w", readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("create persistent goal returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return "", fmt.Errorf("decode persistent goal response: %w", err)
	}
	result, err := json.Marshal(map[string]any{
		"status": "planned", "goal": json.RawMessage(envelope.Data),
		"next": "The supervisor may execute only the bounded graph and will checkpoint before pause, restart, or user input.",
	})
	if err != nil {
		return "", fmt.Errorf("encode persistent goal result: %w", err)
	}
	return string(result), nil
}

func validatePersistentGoalInput(request PersistentGoalCreateInput) error {
	if strings.TrimSpace(request.Objective) == "" || len(request.SuccessCriteria) == 0 {
		return fmt.Errorf("persistent goal objective and success criteria are required")
	}
	for _, criterion := range request.SuccessCriteria {
		if strings.TrimSpace(criterion) == "" {
			return fmt.Errorf("persistent goal success criteria cannot be empty")
		}
	}
	if len(request.Tasks) == 0 || len(request.Tasks) > orchestrationv2.MaxGraphNodes {
		return fmt.Errorf("persistent goal requires 1..%d tasks", orchestrationv2.MaxGraphNodes)
	}
	if request.Deadline != "" {
		if _, err := time.Parse(time.RFC3339, request.Deadline); err != nil {
			return fmt.Errorf("persistent goal deadline must use RFC3339: %w", err)
		}
	}
	seen := make(map[string]struct{}, len(request.Tasks))
	for _, item := range request.Tasks {
		id := strings.TrimSpace(item.TaskID)
		if id == "" || strings.TrimSpace(item.Objective) == "" || item.Depth < 1 || item.Depth > orchestrationv2.MaxGraphDepth {
			return fmt.Errorf("every specialist task requires an id, objective, and depth from 1 to %d", orchestrationv2.MaxGraphDepth)
		}
		switch item.Specialist {
		case orchestrationv2.SpecialistResearch, orchestrationv2.SpecialistBrowser, orchestrationv2.SpecialistDesktop, orchestrationv2.SpecialistFile, orchestrationv2.SpecialistSynthesis:
		default:
			return fmt.Errorf("task %s uses an unsupported specialist %q", id, item.Specialist)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate specialist task id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}
