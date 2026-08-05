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

	"github.com/good-fish-man/agent-runtime/internal/constant"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const ScheduledTaskCreateToolName = "ScheduledTaskCreate"

type ScheduledTaskCreateInput struct {
	Name     string         `json:"name"`
	TaskType string         `json:"task_type"`
	Cron     string         `json:"cron"`
	Timezone string         `json:"timezone"`
	Prompt   string         `json:"prompt"`
	Criteria map[string]any `json:"criteria,omitempty"`
}

type scheduledTaskEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type ScheduledTaskCreateTool struct{ userID, agentID, sessionID, timezone string }

func NewScheduledTaskCreateTool(userID, agentID, sessionID, timezone string) *ScheduledTaskCreateTool {
	return &ScheduledTaskCreateTool{userID: userID, agentID: agentID, sessionID: sessionID, timezone: timezone}
}

func (t *ScheduledTaskCreateTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: ScheduledTaskCreateToolName, Desc: "Create a durable background monitoring task for ticket availability, product stock, or hospital appointment slots. Monitoring is read-only. Purchases, reservations, appointments, CAPTCHA, queues, and payments always require the user to continue interactively.", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"name":      {Type: schema.String, Desc: "Short task name", Required: true},
		"task_type": {Type: schema.String, Desc: "ticket, product, appointment, or monitor", Required: true},
		"cron":      {Type: schema.String, Desc: "Five-field cron expression in the user's timezone", Required: true},
		"timezone":  {Type: schema.String, Desc: "IANA timezone, for example Asia/Shanghai", Required: false},
		"prompt":    {Type: schema.String, Desc: "Complete monitoring instructions including platform, item/date/location, acceptable price and notification condition. For medical appointments, use only a hospital, department, clinician, and date range explicitly chosen by the user; never select care based on symptoms.", Required: true},
		"criteria":  {Type: schema.Object, Desc: "Structured non-sensitive matching criteria", Required: false},
	})}, nil
}

func (t *ScheduledTaskCreateTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	if t.userID == "" || t.agentID == "" {
		return "", fmt.Errorf("scheduled tasks require an authenticated user and selected agent")
	}
	var req ScheduledTaskCreateInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if req.Timezone == "" {
		req.Timezone = t.timezone
	}
	if req.Timezone == "" {
		req.Timezone = "Local"
	}
	payload := map[string]any{"user_id": t.userID, "agent_id": t.agentID, "session_id": t.sessionID, "name": req.Name, "task_type": req.TaskType, "cron": req.Cron, "timezone": req.Timezone, "prompt": req.Prompt, "criteria": req.Criteria}
	body, _ := json.Marshal(payload)
	endpoint := strings.TrimSpace(os.Getenv(constant.EnvRuntimeClientInternalURL))
	if endpoint == "" {
		endpoint = constant.DefaultRuntimeClientInternalScheduledTaskURL
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(constant.HeaderAthenaInternalToken, strings.TrimSpace(os.Getenv(constant.EnvInternalServiceToken)))
	res, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("create scheduled task: %w", err)
	}
	defer res.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("create scheduled task returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var envelope scheduledTaskEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return "", fmt.Errorf("decode scheduled task response: %w", err)
	}
	result, _ := json.Marshal(map[string]any{"status": "scheduled", "task": envelope.Data, "safety": "Monitoring only. Final action requires user confirmation in an interactive browser."})
	return string(result), nil
}
