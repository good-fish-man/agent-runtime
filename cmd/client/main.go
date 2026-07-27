// Command client is a gRPC test client for the Agent Runtime. It provides a
// simple CLI to call each AgentRuntime RPC so you can drive and observe the
// service locally (including streaming responses).
//
// Usage:
//
//	go run ./cmd/client <subcommand> [flags]
//
// Subcommands: health, run, run-stream, agent, agent-stream, resume, stop.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"github.com/good-fish-man/agent-runtime/internal/constant"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var jsonMarshal = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false, Multiline: true, Indent: "  "}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func usage() {
	fmt.Fprint(os.Stderr, `agent-runtime gRPC test client

Usage:
  client <subcommand> [flags]

Subcommands:
  health         Call HealthCheck (verify connectivity)
  run            Call Run (single-turn, non-streaming)
  run-stream     Call RunStream (single-turn, streaming)
  agent          Call RunAgent (natural-language task, non-streaming)
  agent-stream   Call RunAgentStream (natural-language task, streaming)
  resume         Call Resume (currently Unimplemented server-side)
  stop           Call Stop (currently Unimplemented server-side)

Run "client <subcommand> -h" to see flags for a subcommand.
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]

	var err error
	switch sub {
	case "health":
		err = cmdHealth(args)
	case "run":
		err = cmdRun(args)
	case "run-stream":
		err = cmdRunStream(args)
	case "agent":
		err = cmdAgent(args)
	case "agent-stream":
		err = cmdAgentStream(args)
	case "resume":
		err = cmdResume(args)
	case "stop":
		err = cmdStop(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", sub)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// commonFlags holds connection and model settings shared by every subcommand.
type commonFlags struct {
	addr    string
	timeout time.Duration
	traceID string
	model   string
	apiKey  string
	apiBase string
}

func bindCommon(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.addr, "addr", getenv(constant.EnvGRPCAddr, "localhost:18080"), "gRPC server address (env GRPC_ADDR)")
	fs.DurationVar(&c.timeout, "timeout", 60*time.Second, "request timeout")
	fs.StringVar(&c.traceID, "trace-id", "", "optional trace_id to propagate")
	fs.StringVar(&c.model, "model", os.Getenv(constant.EnvDefaultModel), "model name -> models[\"default\"] (env DEFAULT_MODEL)")
	fs.StringVar(&c.apiKey, "api-key", os.Getenv(constant.EnvDefaultAPIKey), "model API key (env DEFAULT_API_KEY)")
	fs.StringVar(&c.apiBase, "api-base", os.Getenv(constant.EnvDefaultAPIBase), "model API base URL (env DEFAULT_API_BASE)")
}

// models builds the models map from the common model flags, or nil when unset.
func (c *commonFlags) models() map[string]*runtimev1.ModelConfig {
	if c.model == "" && c.apiKey == "" && c.apiBase == "" {
		return nil
	}
	return map[string]*runtimev1.ModelConfig{
		constant.ModelRoleDefault: {
			Name:    c.model,
			ApiKey:  c.apiKey,
			ApiBase: c.apiBase,
		},
	}
}

func dial(addr string) (*grpc.ClientConn, runtimev1.AgentRuntimeClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, runtimev1.NewAgentRuntimeClient(conn), nil
}

// protoString renders a proto message as indented protojson (best-effort).
func protoString(msg proto.Message) string {
	b, err := jsonMarshal.Marshal(msg)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(b)
}

func printProto(msg proto.Message) {
	fmt.Println(protoString(msg))
}

// ---- unary subcommands ----

func cmdHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	var c commonFlags
	bindCommon(fs, &c)
	service := fs.String("service", "", "optional service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	conn, cli, err := dial(c.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	resp, err := cli.HealthCheck(ctx, &runtimev1.HealthCheckRequest{Service: *service, TraceId: c.traceID})
	if err != nil {
		return err
	}
	printProto(resp)
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var c commonFlags
	bindCommon(fs, &c)
	prompt := fs.String("prompt", "", "prompt text (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prompt == "" {
		return errors.New("-prompt is required")
	}
	conn, cli, err := dial(c.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	resp, err := cli.Run(ctx, &runtimev1.RunRequest{
		Prompt:  *prompt,
		Models:  c.models(),
		TraceId: c.traceID,
	})
	if err != nil {
		return err
	}
	printProto(resp)
	return nil
}

func cmdAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	var c commonFlags
	bindCommon(fs, &c)
	task := fs.String("task", "", "task text (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *task == "" {
		return errors.New("-task is required")
	}
	conn, cli, err := dial(c.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	resp, err := cli.RunAgent(ctx, &runtimev1.AgentRequest{
		Task:    *task,
		Models:  c.models(),
		TraceId: c.traceID,
	})
	if err != nil {
		return err
	}
	printProto(resp)
	return nil
}

func cmdResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	var c commonFlags
	bindCommon(fs, &c)
	checkpoint := fs.String("checkpoint-id", "", "checkpoint_id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	conn, cli, err := dial(c.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	resp, err := cli.Resume(ctx, &runtimev1.ResumeRequest{CheckpointId: *checkpoint, TraceId: c.traceID})
	if err != nil {
		return err
	}
	printProto(resp)
	return nil
}

func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	var c commonFlags
	bindCommon(fs, &c)
	checkpoint := fs.String("checkpoint-id", "", "checkpoint_id")
	session := fs.String("session-id", "", "session_id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	conn, cli, err := dial(c.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	resp, err := cli.Stop(ctx, &runtimev1.StopRequest{CheckpointId: *checkpoint, SessionId: *session, TraceId: c.traceID})
	if err != nil {
		return err
	}
	printProto(resp)
	return nil
}

// ---- streaming subcommands ----

func cmdRunStream(args []string) error {
	fs := flag.NewFlagSet("run-stream", flag.ContinueOnError)
	var c commonFlags
	bindCommon(fs, &c)
	prompt := fs.String("prompt", "", "prompt text (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prompt == "" {
		return errors.New("-prompt is required")
	}
	conn, cli, err := dial(c.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	stream, err := cli.RunStream(ctx, &runtimev1.RunRequest{
		Prompt:  *prompt,
		Models:  c.models(),
		TraceId: c.traceID,
	})
	if err != nil {
		return err
	}
	return consumeStream(stream)
}

func cmdAgentStream(args []string) error {
	fs := flag.NewFlagSet("agent-stream", flag.ContinueOnError)
	var c commonFlags
	bindCommon(fs, &c)
	task := fs.String("task", "", "task text (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *task == "" {
		return errors.New("-task is required")
	}
	conn, cli, err := dial(c.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	stream, err := cli.RunAgentStream(ctx, &runtimev1.AgentRequest{
		Task:    *task,
		Models:  c.models(),
		TraceId: c.traceID,
	})
	if err != nil {
		return err
	}
	return consumeStream(stream)
}

// consumeStream reads StreamEvents until EOF, printing a readable view of each
// event so you can watch deltas arrive in real time.
func consumeStream(stream grpc.ServerStreamingClient[runtimev1.StreamEvent]) error {
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		printEvent(ev)
	}
}

func printEvent(ev *runtimev1.StreamEvent) {
	switch p := ev.GetPayload().(type) {
	case *runtimev1.StreamEvent_Meta:
		fmt.Printf("[meta] seq=%d protocol=%s started_at=%s\n",
			ev.GetSeq(), p.Meta.GetStreamProtocol(), p.Meta.GetStartedAt().AsTime().Format(time.RFC3339))
	case *runtimev1.StreamEvent_Delta:
		fmt.Print(p.Delta.GetText())
	case *runtimev1.StreamEvent_ToolCall:
		fmt.Printf("\n[tool_call] id=%s tool=%s\n", p.ToolCall.GetId(), p.ToolCall.GetTool())
	case *runtimev1.StreamEvent_ToolResult:
		fmt.Printf("\n[tool_result] id=%s tool=%s success=%v\n", p.ToolResult.GetId(), p.ToolResult.GetTool(), p.ToolResult.GetSuccess())
	case *runtimev1.StreamEvent_Interrupted:
		fmt.Printf("\n[interrupted] checkpoint=%s pending=%d\n", p.Interrupted.GetCheckpointId(), len(p.Interrupted.GetPendingApprovals()))
	case *runtimev1.StreamEvent_Error:
		fmt.Printf("\n[error] code=%s retryable=%v message=%s\n", p.Error.GetCode(), p.Error.GetRetryable(), p.Error.GetMessage())
	case *runtimev1.StreamEvent_Done:
		fmt.Printf("\n[done] finish_reason=%s tokens(prompt=%d completion=%d total=%d) latency_ms=%d\n",
			p.Done.GetFinishReason(), p.Done.GetPromptTokens(), p.Done.GetCompletionTokens(),
			p.Done.GetTotalTokens(), p.Done.GetMetadata().GetLatencyMs())
	default:
		fmt.Printf("[event] seq=%d %v\n", ev.GetSeq(), ev)
	}
}
