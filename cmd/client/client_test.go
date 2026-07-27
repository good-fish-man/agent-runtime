package main

// Clickable gRPC call tests.
//
// These are NOT strict assertions — they exist so you can click "run" on a
// single test function in your IDE (or use `go test -run TestXxx -v ./cmd/client`)
// to fire one real gRPC request against a running server and observe the
// response. Tweak the editable parameters below, then click-run.
//
// Prerequisite: start the server first, e.g. `go run ./cmd/server`.
// If the server is not reachable, the test is skipped (not failed).

import (
	"context"
	"os"
	"testing"
	"time"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"github.com/good-fish-man/agent-runtime/internal/constant"
)

// ============================================================
// Editable request parameters — change these, then click-run a test.
// (Model fields default to the DEFAULT_* env vars; override inline as needed.)
// ============================================================
var key = ""
var modelName = "gpt-4.1-mini"
var baseURL = "https://api.openai.com/v1"
var (
	testAddr    = getenv(constant.EnvGRPCAddr, "localhost:18080")
	testTimeout = 60 * time.Second
	testTraceID = "" // optional trace_id to propagate

	// Model config -> models["default"]. Leave empty to let the server fall
	// back to its own DEFAULT_MODEL env.
	testModel   = getenv(constant.EnvDefaultModel, modelName)
	testAPIKey  = getenv(constant.EnvDefaultAPIKey, key)
	testAPIBase = getenv(constant.EnvDefaultAPIBase, baseURL)

	// Inputs for the different RPCs.
	testPrompt       = "用一句话解释什么是 gRPC"
	testTask         = "帮我列出快速排序的步骤"
	testCheckpointID = "demo-checkpoint"
	testSessionID    = "demo-session"
)

// testModels builds the models map from the editable params, or nil when unset.
func testModels() map[string]*runtimev1.ModelConfig {
	if testModel == "" && testAPIKey == "" && testAPIBase == "" {
		return nil
	}
	return map[string]*runtimev1.ModelConfig{
		constant.ModelRoleDefault: {
			Name:    testModel,
			ApiKey:  testAPIKey,
			ApiBase: testAPIBase,
		},
	}
}

// requireServer dials the server and verifies it is reachable via a quick
// HealthCheck. If not reachable, the calling test is skipped.
func requireServer(t *testing.T) (runtimev1.AgentRuntimeClient, func()) {
	t.Helper()
	conn, cli, err := dial(testAddr)
	if err != nil {
		t.Skipf("cannot create client for %s: %v", testAddr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.HealthCheck(ctx, &runtimev1.HealthCheckRequest{}); err != nil {
		_ = conn.Close()
		t.Skipf("server not reachable at %s (start it with `go run ./cmd/server`): %v", testAddr, err)
	}
	return cli, func() { _ = conn.Close() }
}

func testContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), testTimeout)
}

func requireLiveModel(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_LIVE_MODEL_TESTS") != "1" {
		t.Skip("set RUN_LIVE_MODEL_TESTS=1 and configure model credentials to run live model tests")
	}
}

// ---- unary RPCs ----

func TestHealth(t *testing.T) {
	cli, closeConn := requireServer(t)
	defer closeConn()

	ctx, cancel := testContext()
	defer cancel()
	resp, err := cli.HealthCheck(ctx, &runtimev1.HealthCheckRequest{TraceId: testTraceID})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	t.Logf("HealthCheck response:\n%s", protoString(resp))
}

func TestRun(t *testing.T) {
	requireLiveModel(t)
	cli, closeConn := requireServer(t)
	defer closeConn()

	ctx, cancel := testContext()
	defer cancel()
	resp, err := cli.Run(ctx, &runtimev1.RunRequest{
		Prompt:  testPrompt,
		Models:  testModels(),
		TraceId: testTraceID,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("Run response:\n%s", protoString(resp))
}

func TestAgent(t *testing.T) {
	requireLiveModel(t)
	cli, closeConn := requireServer(t)
	defer closeConn()

	ctx, cancel := testContext()
	defer cancel()
	resp, err := cli.RunAgent(ctx, &runtimev1.AgentRequest{
		Task:    testTask,
		Models:  testModels(),
		TraceId: testTraceID,
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	t.Logf("RunAgent response:\n%s", protoString(resp))
}

func TestResume(t *testing.T) {
	cli, closeConn := requireServer(t)
	defer closeConn()

	ctx, cancel := testContext()
	defer cancel()
	resp, err := cli.Resume(ctx, &runtimev1.ResumeRequest{
		CheckpointId: testCheckpointID,
		TraceId:      testTraceID,
	})
	// Resume is Unimplemented in the current skeleton; log the error to observe it.
	if err != nil {
		t.Logf("Resume returned error (expected while Unimplemented): %v", err)
		return
	}
	t.Logf("Resume response:\n%s", protoString(resp))
}

func TestStop(t *testing.T) {
	cli, closeConn := requireServer(t)
	defer closeConn()

	ctx, cancel := testContext()
	defer cancel()
	resp, err := cli.Stop(ctx, &runtimev1.StopRequest{
		CheckpointId: testCheckpointID,
		SessionId:    testSessionID,
		TraceId:      testTraceID,
	})
	// Stop is Unimplemented in the current skeleton; log the error to observe it.
	if err != nil {
		t.Logf("Stop returned error (expected while Unimplemented): %v", err)
		return
	}
	t.Logf("Stop response:\n%s", protoString(resp))
}

// ---- streaming RPCs ----

func TestRunStream(t *testing.T) {
	requireLiveModel(t)
	cli, closeConn := requireServer(t)
	defer closeConn()

	ctx, cancel := testContext()
	defer cancel()
	stream, err := cli.RunStream(ctx, &runtimev1.RunRequest{
		Prompt:  testPrompt,
		Models:  testModels(),
		TraceId: testTraceID,
	})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	if err := consumeStream(stream); err != nil {
		t.Fatalf("consume RunStream: %v", err)
	}
}

func TestAgentStream(t *testing.T) {
	requireLiveModel(t)
	cli, closeConn := requireServer(t)
	defer closeConn()

	ctx, cancel := testContext()
	defer cancel()
	stream, err := cli.RunAgentStream(ctx, &runtimev1.AgentRequest{
		Task:    testTask,
		Models:  testModels(),
		TraceId: testTraceID,
	})
	if err != nil {
		t.Fatalf("RunAgentStream: %v", err)
	}
	if err := consumeStream(stream); err != nil {
		t.Fatalf("consume RunAgentStream: %v", err)
	}
}
