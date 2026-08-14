// Command server starts the Agent Runtime: a gRPC service plus a light HTTP/SSE
// gateway that mirrors the runner endpoints (/run, /agent, /healthz).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"github.com/good-fish-man/agent-runtime/internal/admin"
	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/config"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/database"
	"github.com/good-fish-man/agent-runtime/internal/dispatcher"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/memory"
	"github.com/good-fish-man/agent-runtime/internal/operations"
	"github.com/good-fish-man/agent-runtime/internal/provider"
	"github.com/good-fish-man/agent-runtime/internal/research"
	researchevidence "github.com/good-fish-man/agent-runtime/internal/research/evidence"
	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
	"github.com/good-fish-man/agent-runtime/internal/server"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	log "github.com/good-fish-man/logx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func main() {
	configPath := config.ResolvePath("")
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	for {
		restart, err := serve(configPath, quit)
		if err != nil {
			log.Fatalf("runtime service failed: %v", err)
		}
		if !restart {
			return
		}
		log.Infof("restarting agent-runtime with updated configuration...")
	}
}

func serve(configPath string, quit <-chan os.Signal) (bool, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return false, fmt.Errorf("load config: %w", err)
	}
	pluginConfig := provider.Config{
		Enabled: cfg.Plugins.Enabled, Directory: cfg.Plugins.Dir, RegistryPath: cfg.Plugins.RegistryPath,
		TrustStorePath: cfg.Plugins.TrustStorePath, AuditPath: cfg.Plugins.AuditPath,
		RuntimeVersion: constant.Version, RequireSignature: cfg.Plugins.RequireSignature,
	}
	reloadPlugins := func(ctx context.Context) (any, error) {
		_, report, loadErr := provider.LoadAndRegister(ctx, capability.GlobalRegistry, pluginConfig)
		if loadErr != nil {
			return nil, loadErr
		}
		return report, nil
	}
	pluginReportValue, pluginErr := reloadPlugins(context.Background())
	pluginReport, _ := pluginReportValue.(provider.LoadReport)
	if pluginErr != nil {
		log.Errorf("external capability providers disabled: %v", pluginErr)
	} else {
		log.Infof("capability provider registry loaded active=%d rejected=%d", len(pluginReport.Loaded), len(pluginReport.Rejected))
		for identity, reason := range pluginReport.Rejected {
			log.Warnf("capability provider rejected provider=%s reason=%s", identity, reason)
		}
	}

	var researchRunner research.Runner
	if cfg.Research.Enabled {
		cacheDir := cfg.Research.CacheDir
		if err := researchevidence.ValidateCacheDir(cacheDir); err != nil {
			log.Warnf("persistent research cache disabled: %v", err)
			cacheDir = ""
		}
		protocol := research.DefaultProtocol()
		protocol.MaxSearches = cfg.Research.MaxQueries
		protocol.MaxFetches = cfg.Research.MaxPages
		protocol.MaxResearchRounds = cfg.Research.MaxRounds
		protocol.ResultsPerSearch = cfg.Research.ResultsPerQuery
		protocol.MaxExecutionTime = time.Duration(cfg.Research.TimeoutSec) * time.Second
		protocol.ResearchCacheTTL = time.Duration(cfg.Research.CacheTTLMin) * time.Minute
		protocol.NewsCacheTTL = time.Duration(cfg.Research.NewsCacheTTLMin) * time.Minute
		researchRunner = research.NewResearchAgentWithConfig(research.AgentConfig{
			Protocol: protocol, CacheDir: cacheDir, Providers: cfg.Research.Providers,
			Resilience: searchsystem.ResilienceConfig{
				Timeout:          time.Duration(cfg.Research.ProviderTimeoutSec) * time.Second,
				FailureThreshold: cfg.Research.ProviderFailureThreshold,
				OpenDuration:     time.Duration(cfg.Research.CircuitOpenSec) * time.Second,
			},
		})
		log.Infof("research agent enabled providers=%v cache_dir=%s", cfg.Research.Providers, cacheDir)
	}

	srvCfg := server.Config{
		DefaultModel: eino.ModelConfig{
			Provider: cfg.Server.Model.Provider,
			Name:     cfg.Server.Model.Name,
			APIKey:   cfg.Server.Model.APIKey,
			APIBase:  cfg.Server.Model.APIBase,
		},
		Dispatch: dispatcher.Config{
			SandboxImage:             cfg.Sandbox.DefaultImage,
			SandboxPptxImage:         cfg.Sandbox.PptxImage,
			SandboxWorkdir:           cfg.Sandbox.Workdir,
			SandboxTimeoutMs:         cfg.Sandbox.TimeoutMs,
			SkillsDir:                cfg.Skills.Dir,
			SkillsConfigPath:         cfg.Skills.ConfigPath,
			SkillsGlobalDir:          cfg.Skills.GlobalDir,
			ResearchRunner:           researchRunner,
			DisableResearch:          !cfg.Research.Enabled,
			ResearchModelPlanning:    cfg.Research.ModelPlanning,
			ResearchSemanticVerify:   cfg.Research.SemanticVerification,
			ResearchAdvisorTimeout:   time.Duration(cfg.Research.AdvisorTimeoutSec) * time.Second,
			ResearchMaxAdvisorClaims: cfg.Research.MaxAdvisorClaims,
		},
	}

	// Optionally bring up the memory module (DB-backed). Failures degrade
	// gracefully: the runtime still serves completions without memory.
	if cfg.Memory.Enabled && cfg.DB.Enabled {
		db, err := database.New(cfg.DB)
		if err != nil {
			log.Warnf("memory disabled: database connection failed: %v", err)
		} else {
			store := memory.NewMemStore(db)
			if cfg.Memory.AutoMigrate {
				if err := store.AutoMigrate(); err != nil {
					log.Errorf("memory auto-migrate failed: %v", err)
				}
			}
			srvCfg.Store = store
			srvCfg.InjectMemory = cfg.Memory.InjectIntoPrompt
			srvCfg.Reviewer = memory.NewBackgroundReviewer(memory.ReviewConfig{
				Enabled:   cfg.Memory.BackgroundReview,
				MaxMemory: cfg.Memory.MaxReviewMemory,
			}, store)
			log.Infof("memory module enabled (postgres)")
		}
	}

	srv := server.New(srvCfg)
	gate := operations.NewGate(operations.Config{
		MaxInflight: cfg.Operations.MaxInflight, MaxQueue: cfg.Operations.MaxQueue,
		AdmissionWait:  time.Duration(cfg.Operations.AdmissionWaitMS) * time.Millisecond,
		RequestTimeout: time.Duration(cfg.Operations.RequestTimeoutSec) * time.Second,
	}, runtimeInstanceID())

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(server.UnaryTraceInterceptor(), gate.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(server.StreamTraceInterceptor(), gate.StreamInterceptor()),
	)
	runtimev1.RegisterAgentRuntimeServer(grpcServer, srv)

	lis, err := net.Listen("tcp", cfg.Server.GRPCAddr)
	if err != nil {
		return false, fmt.Errorf("grpc listen %s: %w", cfg.Server.GRPCAddr, err)
	}
	log.Go(func() {
		log.Infof("gRPC server listening on %s", cfg.Server.GRPCAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc(constant.RouteHealth, makeHealthHandler(srv))
	mux.HandleFunc(constant.RouteReady, makeOperationsHealthHandler(gate, false))
	mux.HandleFunc(constant.RouteMetrics, makeOperationsHealthHandler(gate, true))
	mux.Handle(constant.RouteRun, gate.WrapHTTP(makeRunHandler(srv)))
	mux.Handle(constant.RouteAgent, gate.WrapHTTP(makeAgentHandler(srv)))
	mux.HandleFunc(constant.RouteCapabilities, makeCapabilitiesHandler())
	mux.HandleFunc("/generated/", tools.GeneratedImageHandler)
	restartCh := make(chan struct{}, 1)
	admin.NewHandler(configPath, cfg.Skills.ConfigPath, restartCh).WithPluginReload(reloadPlugins).Register(mux)

	httpServer := &http.Server{Addr: cfg.Server.HTTPAddr, Handler: requestLogger(mux), ReadHeaderTimeout: 10 * time.Second}
	log.Infof("HTTP gateway listening on %s", cfg.Server.HTTPAddr)
	serverErr := make(chan error, 1)
	log.Go(func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	})

	restart := false
	select {
	case <-quit:
	case <-restartCh:
		restart = true
	case err := <-serverErr:
		grpcServer.Stop()
		return false, fmt.Errorf("http serve: %w", err)
	}

	log.Infof("shutting down agent-runtime...")
	gate.Drain()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Errorf("HTTP graceful shutdown failed: %v", err)
	}
	grpcStopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcStopped)
	}()
	select {
	case <-grpcStopped:
	case <-ctx.Done():
		grpcServer.Stop()
	}
	return restart, nil
}

func runtimeInstanceID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func makeOperationsHealthHandler(gate *operations.Gate, includeSLO bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		health := gate.Health(constant.Version)
		w.Header().Set("Content-Type", "application/json")
		if health.Status == "UNHEALTHY" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		payload := map[string]any{"health": health}
		if includeSLO {
			payload["slo"] = gate.SLO()
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			writeError(w, fmt.Errorf("encode operations health: %w", err))
		}
	}
}

var (
	jsonMarshal   = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}
	jsonUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}
)

func writeProtoJSON(w http.ResponseWriter, msg proto.Message) {
	b, err := jsonMarshal.Marshal(msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func readProtoJSON(r *http.Request, msg proto.Message) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return jsonUnmarshal.Unmarshal(body, msg)
}

// httpStatus maps a gRPC status code to an HTTP status code.
func httpStatus(err error) int {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, err error) {
	if sink, ok := w.(interface{ SetError(error) }); ok {
		sink.SetError(err)
	}
	msg := err.Error()
	if st, ok := status.FromError(err); ok {
		msg = st.Message()
	}
	http.Error(w, msg, httpStatus(err))
}

func makeHealthHandler(srv *server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		traceID := traceIDFromHTTP(r)
		writeTraceHeaders(w, traceID)
		ctx := contextWithHTTPTrace(r.Context(), traceID)
		resp, err := srv.HealthCheck(ctx, &runtimev1.HealthCheckRequest{TraceId: traceID})
		if err != nil {
			writeError(w, err)
			return
		}
		writeProtoJSON(w, resp)
	}
}

func makeCapabilitiesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"capabilities": capability.GlobalRegistry.List()}); err != nil {
			writeError(w, fmt.Errorf("encode capabilities: %w", err))
		}
	}
}

func makeRunHandler(srv *server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		req := &runtimev1.RunRequest{}
		if err := readProtoJSON(r, req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}
		traceID := traceIDFromHTTP(r)
		if req.TraceId == "" {
			req.TraceId = traceID
		}
		writeTraceHeaders(w, req.TraceId)
		ctx := contextWithHTTPTrace(r.Context(), req.TraceId)
		if req.GetOptions().GetStream() {
			serveSSE(w, r, func(send func(*runtimev1.StreamEvent) error) error {
				return srv.RunStream(req, sseStream{ctx: ctx, send: send})
			})
			return
		}
		resp, err := srv.Run(ctx, req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeProtoJSON(w, resp)
	}
}

func makeAgentHandler(srv *server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		req := &runtimev1.AgentRequest{}
		if err := readProtoJSON(r, req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}
		traceID := traceIDFromHTTP(r)
		if req.TraceId == "" {
			req.TraceId = traceID
		}
		writeTraceHeaders(w, req.TraceId)
		ctx := contextWithHTTPTrace(r.Context(), req.TraceId)
		if req.GetStream() {
			serveSSE(w, r, func(send func(*runtimev1.StreamEvent) error) error {
				return srv.RunAgentStream(req, sseStream{ctx: ctx, send: send})
			})
			return
		}
		resp, err := srv.RunAgent(ctx, req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeProtoJSON(w, resp)
	}
}

func serveSSE(w http.ResponseWriter, r *http.Request, run func(send func(*runtimev1.StreamEvent) error) error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	emittedError := false
	send := func(ev *runtimev1.StreamEvent) error {
		if ev != nil && ev.GetError() != nil {
			emittedError = true
		}
		b, err := jsonMarshal.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName(ev), b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := run(send); err != nil {
		log.ErrorwCtx(r.Context(), "http stream failed", "method", r.Method, "path", r.URL.EscapedPath(), "error_chain", log.FormatError(err))
		if emittedError {
			return
		}
		traceID, _ := r.Context().Value(log.ReqIDKey).(string)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: {\"message\":%q,\"trace_id\":%q}\n\n", constant.EventError, err.Error(), traceID)
		flusher.Flush()
	}
}

func eventName(ev *runtimev1.StreamEvent) string {
	switch ev.GetPayload().(type) {
	case *runtimev1.StreamEvent_Meta:
		return constant.EventMeta
	case *runtimev1.StreamEvent_Delta:
		return constant.EventDelta
	case *runtimev1.StreamEvent_ToolCall:
		return constant.EventToolCall
	case *runtimev1.StreamEvent_ToolResult:
		return constant.EventTool
	case *runtimev1.StreamEvent_Interrupted:
		return constant.EventInterrupted
	case *runtimev1.StreamEvent_Error:
		return constant.EventError
	case *runtimev1.StreamEvent_Done:
		return constant.EventDone
	default:
		return constant.EventMessage
	}
}

// sseStream adapts an SSE send func to the gRPC server-streaming interface so the
// same server methods (RunStream/RunAgentStream) back both transports.
type sseStream struct {
	grpc.ServerStream
	ctx  context.Context
	send func(*runtimev1.StreamEvent) error
}

func (s sseStream) Context() context.Context             { return s.ctx }
func (s sseStream) Send(ev *runtimev1.StreamEvent) error { return s.send(ev) }
