package actionprotocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	protocolv4 "github.com/good-fish-man/athena-protocol/protocol/v4"
)

const (
	Protocol   = protocolv4.Protocol
	TypeAction = protocolv4.TypeAction
	RiskLow    = protocolv4.RiskLow
	RiskMedium = protocolv4.RiskMedium
	RiskHigh   = protocolv4.RiskHigh
	Allow      = protocolv4.Allow
	AskUser    = protocolv4.AskUser
	Block      = protocolv4.Block
)

type Policy = protocolv4.Policy
type Action = protocolv4.Action
type ExpectedObservation = protocolv4.ExpectedObservation

type scope struct {
	taskID   string
	sequence atomic.Int64
}

type scopeKey struct{}

func WithScope(ctx context.Context, taskID string) context.Context {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		taskID = protocolv4.NewID("task")
	}
	return context.WithValue(ctx, scopeKey{}, &scope{taskID: taskID})
}

func New(ctx context.Context, capability, sessionID string, arguments map[string]any, risk, decision string) Action {
	current, _ := ctx.Value(scopeKey{}).(*scope)
	if current == nil {
		current = &scope{taskID: protocolv4.NewID("task")}
	}
	sequence := current.sequence.Add(1)
	actionID := protocolv4.NewID("action")
	stepID := protocolv4.NewID("step")
	if arguments == nil {
		arguments = map[string]any{}
	}
	now := time.Now().UTC()
	operation := capability
	if separator := strings.LastIndex(capability, "."); separator >= 0 && separator+1 < len(capability) {
		operation = capability[separator+1:]
	}
	return Action{
		Protocol: Protocol, Type: TypeAction, TaskID: current.taskID, StepID: stepID, ActionID: actionID,
		SessionID: sessionID, Sequence: sequence, Revision: 1,
		IdempotencyKey: current.taskID + ":" + stepID + ":" + actionID,
		IssuedAt:       now, Deadline: now.Add(2 * time.Minute), Capability: capability, Operation: operation,
		Arguments: arguments, Policy: Policy{Risk: risk, Decision: decision},
		ExpectedObservation: ExpectedObservation{Kind: capability, TimeoutMS: int64((2 * time.Minute) / time.Millisecond)},
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
