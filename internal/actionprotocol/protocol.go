package actionprotocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

const Protocol = "athena.agent.v2"

const (
	TypeAction = "ACTION"
	RiskLow    = "LOW"
	RiskMedium = "MEDIUM"
	RiskHigh   = "HIGH"
	Allow      = "ALLOW"
	AskUser    = "ASK_USER"
	Block      = "BLOCK"
)

type Policy struct {
	Risk     string `json:"risk"`
	Decision string `json:"decision"`
}

type Action struct {
	Protocol       string         `json:"protocol"`
	Type           string         `json:"type"`
	TaskID         string         `json:"task_id"`
	ActionID       string         `json:"action_id"`
	SessionID      string         `json:"session_id,omitempty"`
	Sequence       int64          `json:"sequence"`
	IdempotencyKey string         `json:"idempotency_key"`
	Deadline       time.Time      `json:"deadline"`
	Capability     string         `json:"capability"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	Policy         Policy         `json:"policy"`
}

type scope struct {
	taskID   string
	sequence atomic.Int64
}

type scopeKey struct{}

func WithScope(ctx context.Context, taskID string) context.Context {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		taskID = "task-" + randomHex(12)
	}
	return context.WithValue(ctx, scopeKey{}, &scope{taskID: taskID})
}

func New(ctx context.Context, capability, sessionID string, arguments map[string]any, risk, decision string) Action {
	current, _ := ctx.Value(scopeKey{}).(*scope)
	if current == nil {
		current = &scope{taskID: "task-" + randomHex(12)}
	}
	actionID := "action-" + randomHex(12)
	sequence := current.sequence.Add(1)
	if arguments == nil {
		arguments = map[string]any{}
	}
	return Action{
		Protocol: Protocol, Type: TypeAction, TaskID: current.taskID, ActionID: actionID,
		SessionID: sessionID, Sequence: sequence, IdempotencyKey: current.taskID + ":" + actionID,
		Deadline: time.Now().UTC().Add(2 * time.Minute), Capability: capability, Arguments: arguments,
		Policy: Policy{Risk: risk, Decision: decision},
	}
}

func Marshal(action Action) (string, error) {
	if err := action.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(action)
	if err != nil {
		return "", fmt.Errorf("encode action: %w", err)
	}
	return string(data), nil
}

func Parse(content string) (Action, bool) {
	var action Action
	if json.Unmarshal([]byte(strings.TrimSpace(content)), &action) != nil || action.Validate() != nil {
		return Action{}, false
	}
	return action, true
}

func (a Action) Validate() error {
	if a.Protocol != Protocol || a.Type != TypeAction {
		return fmt.Errorf("invalid action protocol or type")
	}
	if a.TaskID == "" || a.ActionID == "" || a.IdempotencyKey == "" || a.Capability == "" || a.Sequence <= 0 || a.Deadline.IsZero() {
		return fmt.Errorf("task_id, action_id, positive sequence, idempotency_key, deadline, and capability are required")
	}
	if a.Policy.Risk != RiskLow && a.Policy.Risk != RiskMedium && a.Policy.Risk != RiskHigh {
		return fmt.Errorf("unsupported risk %q", a.Policy.Risk)
	}
	if a.Policy.Decision != Allow && a.Policy.Decision != AskUser && a.Policy.Decision != Block {
		return fmt.Errorf("unsupported policy decision %q", a.Policy.Decision)
	}
	return nil
}

func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return strings.Repeat("0", size*2)
	}
	return hex.EncodeToString(value)
}
