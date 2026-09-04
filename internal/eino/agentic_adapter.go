package eino

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const agenticMessageExtraKey = "athena.responses.agentic_message"

// agenticChatModelAdapter lets existing Eino ChatModelAgent consumers use the
// Responses API without flattening the provider-native message between tool
// turns. The underlying AgenticMessage is retained as private runtime metadata.
type agenticChatModelAdapter struct {
	inner      model.AgenticModel
	boundTools []*schema.ToolInfo
	toolsBound bool
}

func newAgenticChatModelAdapter(inner model.AgenticModel) model.ToolCallingChatModel {
	return &agenticChatModelAdapter{inner: inner}
}

func (m *agenticChatModelAdapter) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	agenticInput, err := legacyMessagesToAgentic(input)
	if err != nil {
		return nil, err
	}
	agenticOpts := m.agenticOptions(opts...)
	output, err := m.inner.Generate(ctx, agenticInput, agenticOpts...)
	if err != nil {
		return nil, err
	}
	return agenticMessageToLegacy(output, true), nil
}

func (m *agenticChatModelAdapter) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	agenticInput, err := legacyMessagesToAgentic(input)
	if err != nil {
		return nil, err
	}
	source, err := m.inner.Stream(ctx, agenticInput, m.agenticOptions(opts...)...)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("responses model returned a nil stream")
	}

	reader, writer := schema.Pipe[*schema.Message](modelStreamBuffer)
	go forwardAgenticStream(source, writer)
	return reader, nil
}

func (m *agenticChatModelAdapter) WithTools(toolInfos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if m == nil || m.inner == nil {
		return nil, fmt.Errorf("responses model is not configured")
	}
	return &agenticChatModelAdapter{
		inner:      m.inner,
		boundTools: append([]*schema.ToolInfo(nil), toolInfos...),
		toolsBound: true,
	}, nil
}

func (m *agenticChatModelAdapter) agenticOptions(opts ...model.Option) []model.Option {
	common := model.GetCommonOptions(nil, opts...)
	converted := make([]model.Option, 0, 8)
	if common.Temperature != nil {
		converted = append(converted, model.WithTemperature(*common.Temperature))
	}
	if common.Model != nil {
		converted = append(converted, model.WithModel(*common.Model))
	}
	if common.TopP != nil {
		converted = append(converted, model.WithTopP(*common.TopP))
	}
	if common.MaxTokens != nil {
		converted = append(converted, model.WithMaxTokens(*common.MaxTokens))
	}
	if common.DeferredTools != nil {
		converted = append(converted, model.WithDeferredTools(common.DeferredTools))
	}
	if common.ToolSearchTool != nil {
		converted = append(converted, model.WithToolSearchTool(common.ToolSearchTool))
	}

	requestTools := common.Tools
	if requestTools == nil && m.toolsBound {
		requestTools = m.boundTools
	}
	if requestTools != nil {
		converted = append(converted, model.WithTools(requestTools))
	}

	choice := common.AgenticToolChoice
	if choice == nil && common.ToolChoice != nil {
		choice = legacyToolChoiceToAgentic(*common.ToolChoice, common.AllowedToolNames)
	}
	if choice != nil {
		converted = append(converted, model.WithAgenticToolChoice(choice))
	}
	return converted
}

func legacyToolChoiceToAgentic(choice schema.ToolChoice, names []string) *schema.AgenticToolChoice {
	converted := &schema.AgenticToolChoice{Type: choice}
	if len(names) == 0 {
		return converted
	}
	allowed := make([]*schema.AllowedTool, 0, len(names))
	for _, name := range names {
		allowed = append(allowed, &schema.AllowedTool{FunctionName: name})
	}
	if choice == schema.ToolChoiceForced {
		converted.Forced = &schema.AgenticForcedToolChoice{Tools: allowed}
	} else {
		converted.Allowed = &schema.AgenticAllowedToolChoice{Tools: allowed}
	}
	return converted
}

func forwardAgenticStream(source *schema.StreamReader[*schema.AgenticMessage], writer *schema.StreamWriter[*schema.Message]) {
	defer source.Close()
	defer writer.Close()

	chunks := make([]*schema.AgenticMessage, 0, 16)
	for {
		chunk, err := source.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writer.Send(nil, err)
			return
		}
		chunks = append(chunks, chunk)
		if writer.Send(agenticMessageToLegacy(chunk, false), nil) {
			return
		}
	}
	if len(chunks) == 0 {
		return
	}
	full, err := schema.ConcatAgenticMessages(chunks)
	if err != nil {
		writer.Send(nil, err)
		return
	}
	metadata := &schema.Message{
		Role:  agenticRoleToLegacy(full.Role),
		Extra: map[string]any{agenticMessageExtraKey: full},
	}
	writer.Send(metadata, nil)
}

func legacyMessagesToAgentic(messages []*schema.Message) ([]*schema.AgenticMessage, error) {
	converted := make([]*schema.AgenticMessage, 0, len(messages))
	for i, message := range messages {
		if message == nil {
			return nil, fmt.Errorf("message at index %d is nil", i)
		}
		if message.Extra != nil {
			if original, ok := message.Extra[agenticMessageExtraKey].(*schema.AgenticMessage); ok && original != nil {
				converted = append(converted, original)
				continue
			}
		}
		converted = append(converted, legacyMessageToAgentic(message))
	}
	return converted, nil
}

func legacyMessageToAgentic(message *schema.Message) *schema.AgenticMessage {
	if message.Role == schema.Tool {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolResult{
				CallID: message.ToolCallID,
				Name:   message.ToolName,
				Content: []*schema.FunctionToolResultContentBlock{{
					Type: schema.FunctionToolResultContentBlockTypeText,
					Text: &schema.UserInputText{Text: message.Content},
				}},
			})},
		}
	}

	result := &schema.AgenticMessage{Role: legacyRoleToAgentic(message.Role)}
	if len(message.UserInputMultiContent) > 0 {
		for _, part := range message.UserInputMultiContent {
			result.ContentBlocks = append(result.ContentBlocks, inputPartToAgentic(part))
		}
	} else if message.Content != "" {
		if result.Role == schema.AgenticRoleTypeAssistant {
			result.ContentBlocks = append(result.ContentBlocks, schema.NewContentBlock(&schema.AssistantGenText{Text: message.Content}))
		} else {
			result.ContentBlocks = append(result.ContentBlocks, schema.NewContentBlock(&schema.UserInputText{Text: message.Content}))
		}
	}
	if result.Role == schema.AgenticRoleTypeAssistant {
		if message.ReasoningContent != "" {
			result.ContentBlocks = append(result.ContentBlocks, schema.NewContentBlock(&schema.Reasoning{Text: message.ReasoningContent}))
		}
		for _, call := range message.ToolCalls {
			block := schema.NewContentBlock(&schema.FunctionToolCall{
				CallID:    call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
			block.Extra = call.Extra
			result.ContentBlocks = append(result.ContentBlocks, block)
		}
	}
	return result
}

func inputPartToAgentic(part schema.MessageInputPart) *schema.ContentBlock {
	switch part.Type {
	case schema.ChatMessagePartTypeImageURL:
		if part.Image != nil {
			block := schema.NewContentBlock(&schema.UserInputImage{
				URL:        stringValue(part.Image.URL),
				Base64Data: stringValue(part.Image.Base64Data),
				MIMEType:   part.Image.MIMEType,
				Detail:     part.Image.Detail,
			})
			block.Extra = part.Extra
			return block
		}
	case schema.ChatMessagePartTypeAudioURL:
		if part.Audio != nil {
			block := schema.NewContentBlock(&schema.UserInputAudio{
				URL:        stringValue(part.Audio.URL),
				Base64Data: stringValue(part.Audio.Base64Data),
				MIMEType:   part.Audio.MIMEType,
			})
			block.Extra = part.Extra
			return block
		}
	case schema.ChatMessagePartTypeVideoURL:
		if part.Video != nil {
			block := schema.NewContentBlock(&schema.UserInputVideo{
				URL:        stringValue(part.Video.URL),
				Base64Data: stringValue(part.Video.Base64Data),
				MIMEType:   part.Video.MIMEType,
			})
			block.Extra = part.Extra
			return block
		}
	case schema.ChatMessagePartTypeFileURL:
		if part.File != nil {
			block := schema.NewContentBlock(&schema.UserInputFile{
				URL:        stringValue(part.File.URL),
				Name:       part.File.Name,
				Base64Data: stringValue(part.File.Base64Data),
				MIMEType:   part.File.MIMEType,
			})
			block.Extra = part.Extra
			return block
		}
	}
	block := schema.NewContentBlock(&schema.UserInputText{Text: part.Text})
	block.Extra = part.Extra
	return block
}

func agenticMessageToLegacy(message *schema.AgenticMessage, retainOriginal bool) *schema.Message {
	if message == nil {
		return nil
	}
	result := &schema.Message{Role: agenticRoleToLegacy(message.Role)}
	for blockIndex, block := range message.ContentBlocks {
		if block == nil {
			continue
		}
		switch block.Type {
		case schema.ContentBlockTypeAssistantGenText:
			if block.AssistantGenText != nil {
				result.Content += block.AssistantGenText.Text
			}
		case schema.ContentBlockTypeReasoning:
			if block.Reasoning != nil {
				result.ReasoningContent += block.Reasoning.Text
				result.AssistantGenMultiContent = append(result.AssistantGenMultiContent, schema.MessageOutputPart{
					Type:          schema.ChatMessagePartTypeReasoning,
					Reasoning:     &schema.MessageOutputReasoning{Text: block.Reasoning.Text, Signature: block.Reasoning.Signature},
					Extra:         block.Extra,
					StreamingMeta: messageStreamingMeta(block),
				})
			}
		case schema.ContentBlockTypeAssistantGenImage:
			if block.AssistantGenImage != nil {
				url, base64Data := optionalStrings(block.AssistantGenImage.URL, block.AssistantGenImage.Base64Data)
				result.AssistantGenMultiContent = append(result.AssistantGenMultiContent, schema.MessageOutputPart{
					Type: schema.ChatMessagePartTypeImageURL,
					Image: &schema.MessageOutputImage{MessagePartCommon: schema.MessagePartCommon{
						URL: url, Base64Data: base64Data, MIMEType: block.AssistantGenImage.MIMEType,
					}},
					Extra: block.Extra, StreamingMeta: messageStreamingMeta(block),
				})
			}
		case schema.ContentBlockTypeFunctionToolCall:
			if block.FunctionToolCall != nil {
				index := blockIndex
				if block.StreamingMeta != nil {
					index = block.StreamingMeta.Index
				}
				result.ToolCalls = append(result.ToolCalls, schema.ToolCall{
					Index: &index,
					ID:    block.FunctionToolCall.CallID,
					Type:  "function",
					Function: schema.FunctionCall{
						Name: block.FunctionToolCall.Name, Arguments: block.FunctionToolCall.Arguments,
					},
					Extra: block.Extra,
				})
			}
		case schema.ContentBlockTypeFunctionToolResult:
			if block.FunctionToolResult != nil {
				result.Role = schema.Tool
				result.ToolCallID = block.FunctionToolResult.CallID
				result.ToolName = block.FunctionToolResult.Name
				for _, content := range block.FunctionToolResult.Content {
					if content != nil {
						result.Content += content.String()
					}
				}
			}
		}
	}
	if message.ResponseMeta != nil {
		result.ResponseMeta = &schema.ResponseMeta{Usage: message.ResponseMeta.TokenUsage}
		result.ResponseMeta.FinishReason = agenticFinishReason(message, result.ToolCalls)
	}
	if retainOriginal {
		result.Extra = cloneExtra(message.Extra)
		if result.Extra == nil {
			result.Extra = make(map[string]any, 1)
		}
		result.Extra[agenticMessageExtraKey] = message
	}
	return result
}

func agenticFinishReason(message *schema.AgenticMessage, calls []schema.ToolCall) string {
	if len(calls) > 0 {
		return "tool_calls"
	}
	if message != nil && message.ResponseMeta != nil && message.ResponseMeta.OpenAIExtension != nil &&
		string(message.ResponseMeta.OpenAIExtension.Status) == "incomplete" {
		return "length"
	}
	return "stop"
}

func legacyRoleToAgentic(role schema.RoleType) schema.AgenticRoleType {
	switch role {
	case schema.System:
		return schema.AgenticRoleTypeSystem
	case schema.Assistant:
		return schema.AgenticRoleTypeAssistant
	default:
		return schema.AgenticRoleTypeUser
	}
}

func agenticRoleToLegacy(role schema.AgenticRoleType) schema.RoleType {
	switch role {
	case schema.AgenticRoleTypeSystem:
		return schema.System
	case schema.AgenticRoleTypeAssistant:
		return schema.Assistant
	default:
		return schema.User
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalStrings(urlValue, base64Value string) (*string, *string) {
	var url, base64Data *string
	if urlValue != "" {
		url = &urlValue
	}
	if base64Value != "" {
		base64Data = &base64Value
	}
	return url, base64Data
}

func messageStreamingMeta(block *schema.ContentBlock) *schema.MessageStreamingMeta {
	if block == nil || block.StreamingMeta == nil {
		return nil
	}
	return &schema.MessageStreamingMeta{Index: block.StreamingMeta.Index}
}

func cloneExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	clone := make(map[string]any, len(extra))
	for key, value := range extra {
		clone[key] = value
	}
	return clone
}
