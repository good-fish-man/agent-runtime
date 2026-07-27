package dispatcher

import (
	"context"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/contextcompressor"
	"github.com/good-fish-man/agent-runtime/internal/contextcompressor/compactors"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/log"

	"github.com/cloudwego/eino/schema"
)

// buildCompactService constructs the context-compression integration service,
// backed by the client's chat model and a heuristic tokenizer. A user-supplied
// MaxTotalTokens (RunOptions) overrides the model-derived auto threshold.
func (d *Dispatcher) buildCompactService() *contextcompressor.IntegrationService {
	tokenizer := contextcompressor.NewDefaultTokenizerImpl(4.0)
	proxy := &contextcompressor.ChatModelProxy{GenerateFunc: d.compressGenerate}

	var opts []contextcompressor.Option
	if d.req.Options != nil && d.req.Options.MaxTotalTokens > 0 {
		opts = append(opts, contextcompressor.WithCustomThreshold(d.req.Options.MaxTotalTokens))
	}
	compactor := contextcompressor.NewCompactor(proxy, tokenizer, opts...)
	return contextcompressor.NewIntegrationService(compactor)
}

// compressGenerate adapts the compressor's ChatModel contract onto the eino
// chat model, producing the summary text used during compaction.
func (d *Dispatcher) compressGenerate(ctx context.Context, messages []compactors.Message) (string, error) {
	schemaMsgs := make([]*schema.Message, 0, len(messages))
	for i := range messages {
		schemaMsgs = append(schemaMsgs, &schema.Message{
			Role:    schemaRole(messages[i].Type, messages[i].Role),
			Content: textOfCompactors(messages[i].Content),
		})
	}
	out, err := d.client.Model().Generate(ctx, schemaMsgs)
	if err != nil {
		return "", err
	}
	if out == nil {
		return "", nil
	}
	return out.Content, nil
}

// maybeCompact compresses the prior conversation when it exceeds the configured
// threshold, returning the (possibly rebuilt) message list. On any error it
// logs and returns the original messages unchanged.
func (d *Dispatcher) maybeCompact(ctx context.Context, msgs []eino.ChatMessage) []eino.ChatMessage {
	if d.compact == nil || !d.compact.IsEnabled() || len(msgs) == 0 {
		return msgs
	}
	ccMsgs := chatToCC(msgs)
	if !d.compact.ShouldCompact(ccMsgs) {
		return msgs
	}
	result, err := d.compact.Compact(ctx, ccMsgs)
	if err != nil || result == nil {
		if err != nil {
			log.Warnf("[Dispatcher] compaction failed, using original messages: %v", err)
		}
		return msgs
	}
	post := contextcompressor.BuildPostCompactMessages(result)
	if len(post) == 0 {
		return msgs
	}
	log.Infof("[Dispatcher] compacted %d messages -> %d (pre=%d post=%d tokens)",
		len(ccMsgs), len(post), result.PreCompactTokens, result.PostCompactTokens)
	return ccToChat(post)
}

// ---- conversions ----

func chatToCC(msgs []eino.ChatMessage) []contextcompressor.Message {
	out := make([]contextcompressor.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, *contextcompressor.NewTextMessage(msgTypeOfRole(m.Role), m.Content))
	}
	return out
}

func ccToChat(msgs []contextcompressor.Message) []eino.ChatMessage {
	out := make([]eino.ChatMessage, 0, len(msgs))
	for i := range msgs {
		text := textOfCC(msgs[i].Content)
		if text == "" {
			continue
		}
		out = append(out, eino.ChatMessage{Role: roleOfMsgType(msgs[i].Type), Content: text})
	}
	return out
}

func msgTypeOfRole(role string) contextcompressor.MessageType {
	switch role {
	case "system":
		return contextcompressor.MessageTypeSystem
	case "assistant":
		return contextcompressor.MessageTypeAssistant
	case "tool":
		return contextcompressor.MessageTypeTool
	default:
		return contextcompressor.MessageTypeUser
	}
}

func roleOfMsgType(t contextcompressor.MessageType) string {
	switch t {
	case contextcompressor.MessageTypeSystem:
		return "system"
	case contextcompressor.MessageTypeAssistant:
		return "assistant"
	case contextcompressor.MessageTypeTool:
		return "tool"
	default:
		return "user"
	}
}

func textOfCC(blocks []contextcompressor.ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" && blk.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

func textOfCompactors(blocks []compactors.ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" && blk.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

func schemaRole(msgType string, role string) schema.RoleType {
	r := role
	if r == "" {
		r = msgType
	}
	switch r {
	case "system":
		return schema.System
	case "assistant":
		return schema.Assistant
	case "tool":
		return schema.Tool
	default:
		return schema.User
	}
}
