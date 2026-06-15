package provider

// Converters between the provider-agnostic request/response types and the
// OpenAI SDK's wire types, shared by every OpenAI-compatible provider.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

func toOpenAIMessages(msgs []Message) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, msg := range msgs {
		switch msg.Role {
		case RoleUser:
			result = append(result, openai.UserMessage(combinedText(msg.Content)))
		case RoleAssistant:
			result = append(result, toOpenAIAssistantMessage(msg))
		case RoleSystem:
			result = append(result, openai.SystemMessage(combinedText(msg.Content)))
		case RoleTool:
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolResult {
					result = append(result, openai.ToolMessage(block.ToolOutput, block.ToolResultID))
				}
			}
		}
	}
	return result
}

func toOpenAIAssistantMessage(msg Message) openai.ChatCompletionMessageParamUnion {
	assistant := openai.ChatCompletionAssistantMessageParam{}

	if text := combinedText(msg.Content); text != "" {
		assistant.Content.OfString = param.NewOpt(text)
	}

	for _, block := range msg.Content {
		if block.Type != ContentTypeToolUse {
			continue
		}
		assistant.ToolCalls = append(assistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: block.ToolUseID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      block.ToolName,
					Arguments: string(block.ToolInput),
				},
			},
		})
	}

	return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}
}

func toOpenAITools(tools []ToolDefinition) ([]openai.ChatCompletionToolUnionParam, error) {
	result := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		var params shared.FunctionParameters
		if tool.InputSchema != nil {
			if err := json.Unmarshal(tool.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("tool %q: parse input schema: %w", tool.Name, err)
			}
		}

		result = append(result, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        tool.Name,
			Description: param.NewOpt(tool.Description),
			Parameters:  params,
		}))
	}
	return result, nil
}

// combinedText concatenates all text blocks, ignoring tool blocks.
func combinedText(blocks []ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == ContentTypeText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func fromOpenAIResponse(res *openai.ChatCompletion) CompletionResponse {
	var content []ContentBlock
	var stopReason StopReason

	if len(res.Choices) > 0 {
		choice := res.Choices[0]
		stopReason = fromOpenAIFinishReason(choice.FinishReason)

		if choice.Message.Content != "" {
			content = append(content, NewTextContent(choice.Message.Content))
		}

		for _, tc := range choice.Message.ToolCalls {
			if tc.Type == "function" {
				content = append(content, NewToolUseContent(
					tc.ID,
					tc.Function.Name,
					json.RawMessage(tc.Function.Arguments),
				))
			}
		}
	}

	return CompletionResponse{
		ID:         res.ID,
		Content:    content,
		StopReason: stopReason,
		Usage: Usage{
			InputTokens:  res.Usage.PromptTokens,
			OutputTokens: res.Usage.CompletionTokens,
		},
		Model: res.Model,
	}
}

func fromOpenAIFinishReason(reason string) StopReason {
	switch reason {
	case "stop":
		return StopReasonStop
	case "length":
		return StopReasonMaxTokens
	case "tool_calls":
		return StopReasonToolUse
	default:
		return StopReason(reason)
	}
}
