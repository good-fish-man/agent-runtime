package server

import (
	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"github.com/good-fish-man/agent-runtime/internal/dispatcher"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/types"
	"google.golang.org/protobuf/types/known/structpb"
)

// newRunDispatcher builds a Dispatcher for a RunRequest, mapping the gRPC
// request into the orchestration-facing types.RunRequest.
func (s *Server) newRunDispatcher(client *eino.Client, req *runtimev1.RunRequest, memInstruction string) *dispatcher.Dispatcher {
	tr := toTypesRunRequest(req)
	return dispatcher.New(client, tr, projectDir(req.GetContext()), memInstruction, s.cfg.Dispatch)
}

// newAgentDispatcher builds a Dispatcher for an AgentRequest. Autonomous task
// requests carry no rich orchestration config, so only built-in and retrieval
// tools are wired.
func (s *Server) newAgentDispatcher(client *eino.Client, req *runtimev1.AgentRequest, memInstruction string) *dispatcher.Dispatcher {
	tr := &types.RunRequest{Models: make(map[string]types.ModelConfig)}
	if req.GetContext() != nil {
		tr.Context = req.GetContext().AsMap()
		tr.Prompt = systemPromptFromContext(tr.Context)
	}
	for role, model := range req.GetModels() {
		if model == nil {
			continue
		}
		tr.Models[role] = types.ModelConfig{
			Provider: model.GetProvider(), Name: model.GetName(), APIKey: model.GetApiKey(), APIBase: model.GetApiBase(),
			Temperature: model.GetTemperature(), MaxTokens: int(model.GetMaxTokens()), TopP: model.GetTopP(),
			ExtraFields: protoExtraFields(model),
		}
	}
	for _, configured := range req.GetCapabilities() {
		tr.Capabilities = append(tr.Capabilities, types.CapabilityConfig{ID: configured.GetId(), Config: structMap(configured.GetConfig())})
	}
	for _, visual := range req.GetVisualInputs() {
		if visual == nil {
			continue
		}
		tr.VisualInputs = append(tr.VisualInputs, types.VisualInput{
			ID: visual.GetId(), MIMEType: visual.GetMimeType(), Data: append([]byte(nil), visual.GetData()...),
			SHA256: visual.GetSha256(), Purpose: visual.GetPurpose(), Detail: visual.GetDetail(),
		})
	}
	return dispatcher.New(client, tr, projectDir(req.GetContext()), memInstruction, s.cfg.Dispatch)
}

// toTypesRunRequest maps the subset of gRPC RunRequest fields consumed by the
// dispatcher and prompt builder into types.RunRequest.
func toTypesRunRequest(req *runtimev1.RunRequest) *types.RunRequest {
	if req == nil {
		return &types.RunRequest{}
	}
	tr := &types.RunRequest{Models: make(map[string]types.ModelConfig)}
	if req.GetContext() != nil {
		tr.Context = req.GetContext().AsMap()
		tr.Prompt = systemPromptFromContext(tr.Context)
	}
	for role, model := range req.GetModels() {
		if model == nil {
			continue
		}
		tr.Models[role] = types.ModelConfig{
			Provider: model.GetProvider(), Name: model.GetName(), APIKey: model.GetApiKey(), APIBase: model.GetApiBase(),
			Temperature: model.GetTemperature(), MaxTokens: int(model.GetMaxTokens()), TopP: model.GetTopP(),
			ExtraFields: protoExtraFields(model),
		}
	}

	for _, kb := range req.GetKnowledgeBases() {
		tr.KnowledgeBases = append(tr.KnowledgeBases, types.KnowledgeBaseConfig{
			ID:           kb.GetId(),
			Name:         kb.GetName(),
			RetrievalURL: kb.GetRetrievalUrl(),
			Token:        kb.GetToken(),
			TopK:         int(kb.GetTopK()),
		})
	}

	for _, s := range req.GetSkills() {
		tr.Skills = append(tr.Skills, types.Skill{
			ID:             s.GetId(),
			Name:           s.GetName(),
			Description:    s.GetDescription(),
			Instruction:    s.GetInstruction(),
			Scope:          s.GetScope(),
			Trigger:        s.GetTrigger(),
			EntryScript:    s.GetEntryScript(),
			FilePath:       s.GetFilePath(),
			Inputs:         s.GetInputs(),
			Outputs:        s.GetOutputs(),
			RiskLevel:      s.GetRiskLevel().String(),
			OutputPatterns: s.GetOutputPatterns(),
		})
	}

	for _, m := range req.GetMcps() {
		tr.MCPs = append(tr.MCPs, types.MCPConfig{
			Name:      m.GetName(),
			Transport: m.GetTransport(),
			Command:   m.GetCommand(),
			Args:      m.GetArgs(),
			Env:       m.GetEnv(),
			Endpoint:  m.GetEndpoint(),
			Headers:   m.GetHeaders(),
			RiskLevel: m.GetRiskLevel().String(),
		})
	}

	for _, a := range req.GetA2A() {
		tr.A2A = append(tr.A2A, types.A2AAgentConfig{
			Name:      a.GetName(),
			Endpoint:  a.GetEndpoint(),
			Headers:   a.GetHeaders(),
			RiskLevel: a.GetRiskLevel().String(),
		})
	}

	for _, configured := range req.GetCapabilities() {
		tr.Capabilities = append(tr.Capabilities, types.CapabilityConfig{ID: configured.GetId(), Config: structMap(configured.GetConfig())})
	}

	for _, ia := range req.GetInternalAgents() {
		tr.InternalAgents = append(tr.InternalAgents, types.InternalAgentConfig{
			ID:     ia.GetId(),
			Name:   ia.GetName(),
			Prompt: ia.GetPrompt(),
		})
	}

	for _, sa := range req.GetSubAgents() {
		var capabilityIDs []string
		for _, configured := range sa.GetCapabilities() {
			if id := configured.GetId(); id != "" {
				capabilityIDs = append(capabilityIDs, id)
			}
		}
		var subModel *types.ModelConfig
		if m := sa.GetModel(); m != nil {
			subModel = &types.ModelConfig{
				Provider: m.GetProvider(), Name: m.GetName(), APIKey: m.GetApiKey(), APIBase: m.GetApiBase(),
				Temperature: m.GetTemperature(), MaxTokens: int(m.GetMaxTokens()), TopP: m.GetTopP(),
			}
		}
		var subSkills []types.Skill
		for _, skill := range sa.GetSkills() {
			subSkills = append(subSkills, types.Skill{
				ID: skill.GetId(), Name: skill.GetName(), Description: skill.GetDescription(),
				Instruction: skill.GetInstruction(), Scope: skill.GetScope(), Trigger: skill.GetTrigger(),
				EntryScript: skill.GetEntryScript(), FilePath: skill.GetFilePath(), Inputs: skill.GetInputs(),
				Outputs: skill.GetOutputs(), RiskLevel: skill.GetRiskLevel().String(), OutputPatterns: skill.GetOutputPatterns(),
			})
		}
		tr.SubAgents = append(tr.SubAgents, types.SubAgentConfig{
			ID:            sa.GetId(),
			Name:          sa.GetName(),
			Description:   sa.GetDescription(),
			Prompt:        sa.GetPrompt(),
			Model:         subModel,
			Capabilities:  capabilityIDs,
			Skills:        subSkills,
			MaxIterations: int(sa.GetMaxIterations()),
			TimeoutMs:     int(sa.GetTimeoutMs()),
		})
	}

	for _, f := range req.GetFiles() {
		tr.Files = append(tr.Files, types.FileConfig{
			Name:        f.GetName(),
			VirtualPath: f.GetVirtualPath(),
			Size:        f.GetSize(),
			Type:        f.GetType(),
		})
	}
	for _, visual := range req.GetVisualInputs() {
		if visual == nil {
			continue
		}
		tr.VisualInputs = append(tr.VisualInputs, types.VisualInput{
			ID: visual.GetId(), MIMEType: visual.GetMimeType(), Data: append([]byte(nil), visual.GetData()...),
			SHA256: visual.GetSha256(), Purpose: visual.GetPurpose(), Detail: visual.GetDetail(),
		})
	}

	if opt := req.GetOptions(); opt != nil {
		to := &types.RunOptions{
			MaxIterations:  int(opt.GetMaxIterations()),
			MaxTotalTokens: int(opt.GetMaxTotalTokens()),
		}
		if rs := opt.GetResponseSchema(); rs != nil {
			to.ResponseSchema = &types.ResponseSchemaConfig{
				Type:     rs.GetType(),
				Version:  rs.GetVersion(),
				Strict:   rs.GetStrict(),
				Fallback: rs.GetFallback(),
			}
			if rs.GetSchema() != nil {
				to.ResponseSchema.Schema = rs.GetSchema().AsMap()
			}
		}
		tr.Options = to
	}

	return tr
}

func protoExtraFields(model *runtimev1.ModelConfig) map[string]any {
	if model == nil || model.GetExtraFields() == nil {
		return nil
	}
	return model.GetExtraFields().AsMap()
}

func structMap(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func systemPromptFromContext(ctx map[string]any) string {
	if ctx == nil {
		return ""
	}
	for _, key := range []string{"system_prompt", "systemPrompt"} {
		if v, ok := ctx[key].(string); ok && v != "" {
			delete(ctx, key)
			return v
		}
		delete(ctx, key)
	}
	return ""
}
