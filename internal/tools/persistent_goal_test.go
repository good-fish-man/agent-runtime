package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

func TestPersistentGoalCreateToolPostsBoundedDeclarativeGraph(t *testing.T) {
	var received map[string]any
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get(constant.HeaderAthenaInternalToken) != "test-token" {
			t.Errorf("missing internal token")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"code":0,"message":"ok","data":{"goal":{"goal_id":"goal-1"},"tasks":[{"task_id":"research"}]}}`)), Header: make(http.Header)}, nil
	})
	t.Setenv(constant.EnvRuntimeClientGoalURL, "https://control.test/internal/goals")
	t.Setenv(constant.EnvInternalServiceToken, "test-token")

	provider := NewPersistentGoalCreateTool("user-1", "agent-1", "session-1")
	provider.client = &http.Client{Transport: transport}
	result, err := provider.InvokableRun(context.Background(), `{
		"objective":"Prepare a sourced trip plan",
		"success_criteria":["A five-day plan cites official sources"],
		"tasks":[
			{"task_id":"research","depth":1,"specialist":"RESEARCH","objective":"Collect evidence"},
			{"task_id":"synthesis","depth":2,"specialist":"SYNTHESIS","objective":"Build itinerary","depends_on":["research"]}
		]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if received["user_id"] != "user-1" || received["agent_id"] != "agent-1" || received["conversation_id"] != "session-1" {
		t.Fatalf("identity was not bound by runtime context: %+v", received)
	}
	if result == "" {
		t.Fatal("tool returned an empty observation")
	}
}

func TestPersistentGoalCreateToolRejectsUnboundedGraph(t *testing.T) {
	provider := NewPersistentGoalCreateTool("user-1", "agent-1", "session-1")
	if _, err := provider.InvokableRun(context.Background(), `{"objective":"work forever","success_criteria":["done"],"tasks":[]}`); err == nil {
		t.Fatal("empty unbounded plan was accepted")
	}
}
