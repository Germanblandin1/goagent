package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	openaiSDK "github.com/sashabaranov/go-openai"

	"github.com/Germanblandin1/goagent"
)

// compile-time interface assertions.
var (
	_ goagent.Provider          = (*Provider)(nil)
	_ goagent.StreamingProvider = (*Provider)(nil)
)

// Complete implements goagent.Provider.
//
// If req.Model is empty, Complete returns an error without making a network call.
//
// If req.Effort is non-empty ("low", "medium", "high"), it is forwarded as
// reasoning_effort to support o-series reasoning models.
//
// req.Thinking is ignored — the OpenAI API does not expose thinking blocks.
//
// If any message contains ContentDocument blocks, Complete returns an
// *goagent.UnsupportedContentError.
func (p *Provider) Complete(ctx context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	if req.Model == "" {
		return goagent.CompletionResponse{}, fmt.Errorf("openai: model not set; use goagent.WithModel")
	}

	messages, err := toOpenAIMessages(req)
	if err != nil {
		return goagent.CompletionResponse{}, err
	}

	chatReq := openaiSDK.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
		// Note: for o-series models MaxCompletionTokens is the correct field,
		// but MaxTokens works for gpt-4o and most models. A future option can
		// toggle this.
		MaxTokens: req.MaxTokens,
	}

	if req.Temperature != nil {
		chatReq.Temperature = float32(*req.Temperature)
	}

	if req.Effort != "" {
		chatReq.ReasoningEffort = req.Effort
	}

	if len(req.Tools) > 0 {
		chatReq.Tools = toOpenAITools(req.Tools)
		chatReq.ToolChoice = "auto"
	}

	resp, err := p.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return goagent.CompletionResponse{}, fmt.Errorf("openai completion: %w", err)
	}

	return toGoAgentResponse(resp)
}

func toOpenAIMessages(req goagent.CompletionRequest) ([]openaiSDK.ChatCompletionMessage, error) {
	var out []openaiSDK.ChatCompletionMessage

	if req.SystemPrompt != "" {
		out = append(out, openaiSDK.ChatCompletionMessage{
			Role:    openaiSDK.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}

	for _, m := range req.Messages {
		msg, err := toOpenAIMessage(m)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func toOpenAIMessage(m goagent.Message) (openaiSDK.ChatCompletionMessage, error) {
	if m.HasContentType(goagent.ContentDocument) {
		return openaiSDK.ChatCompletionMessage{}, &goagent.UnsupportedContentError{
			ContentType: goagent.ContentDocument,
			Provider:    "openai",
			Reason:      "OpenAI Chat Completions API does not support document content",
		}
	}

	role, err := toOpenAIRole(m.Role)
	if err != nil {
		return openaiSDK.ChatCompletionMessage{}, err
	}

	msg := openaiSDK.ChatCompletionMessage{
		Role:       role,
		ToolCallID: m.ToolCallID,
	}

	// Single-text optimization: use Content string field for max compatibility.
	if len(m.Content) == 1 && m.Content[0].Type == goagent.ContentText {
		msg.Content = m.Content[0].Text
	} else if len(m.Content) > 0 {
		parts, err := toOpenAIParts(m.Content)
		if err != nil {
			return openaiSDK.ChatCompletionMessage{}, err
		}
		if len(parts) > 0 {
			msg.MultiContent = parts
		}
	}

	for _, tc := range m.ToolCalls {
		argsJSON, err := json.Marshal(tc.Arguments)
		if err != nil {
			return openaiSDK.ChatCompletionMessage{}, fmt.Errorf("marshaling tool call args: %w", err)
		}
		msg.ToolCalls = append(msg.ToolCalls, openaiSDK.ToolCall{
			ID:   tc.ID,
			Type: openaiSDK.ToolTypeFunction,
			Function: openaiSDK.FunctionCall{
				Name:      tc.Name,
				Arguments: string(argsJSON),
			},
		})
	}

	return msg, nil
}

func toOpenAIParts(blocks []goagent.ContentBlock) ([]openaiSDK.ChatMessagePart, error) {
	parts := make([]openaiSDK.ChatMessagePart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case goagent.ContentText:
			parts = append(parts, openaiSDK.ChatMessagePart{
				Type: openaiSDK.ChatMessagePartTypeText,
				Text: b.Text,
			})
		case goagent.ContentImage:
			dataURL := fmt.Sprintf("data:%s;base64,%s",
				b.Image.MediaType,
				base64.StdEncoding.EncodeToString(b.Image.Data),
			)
			parts = append(parts, openaiSDK.ChatMessagePart{
				Type: openaiSDK.ChatMessagePartTypeImageURL,
				ImageURL: &openaiSDK.ChatMessageImageURL{
					URL: dataURL,
				},
			})
		case goagent.ContentDocument:
			// Checked at message level — should not reach here.
			return nil, &goagent.UnsupportedContentError{
				ContentType: goagent.ContentDocument,
				Provider:    "openai",
				Reason:      "OpenAI Chat Completions API does not support document content",
			}
		case goagent.ContentThinking:
			// OpenAI does not accept thinking blocks — discard silently.
			continue
		}
	}
	return parts, nil
}

func toOpenAIRole(r goagent.Role) (string, error) {
	switch r {
	case goagent.RoleUser:
		return openaiSDK.ChatMessageRoleUser, nil
	case goagent.RoleSystem:
		return openaiSDK.ChatMessageRoleSystem, nil
	case goagent.RoleAssistant:
		return openaiSDK.ChatMessageRoleAssistant, nil
	case goagent.RoleTool:
		return openaiSDK.ChatMessageRoleTool, nil
	default:
		return "", fmt.Errorf("goagent/openai: unsupported role %q — only user/assistant/tool/system are valid in conversation history", r)
	}
}

func toOpenAITools(defs []goagent.ToolDefinition) []openaiSDK.Tool {
	out := make([]openaiSDK.Tool, len(defs))
	for i, d := range defs {
		out[i] = openaiSDK.Tool{
			Type: openaiSDK.ToolTypeFunction,
			Function: &openaiSDK.FunctionDefinition{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Parameters,
			},
		}
	}
	return out
}

func toGoAgentResponse(resp openaiSDK.ChatCompletionResponse) (goagent.CompletionResponse, error) {
	if len(resp.Choices) == 0 {
		return goagent.CompletionResponse{}, fmt.Errorf("openai: empty choices in response")
	}

	choice := resp.Choices[0]
	msg := goagent.Message{
		Role:    goagent.RoleAssistant,
		Content: []goagent.ContentBlock{},
	}

	if choice.Message.Content != "" {
		msg.Content = append(msg.Content, goagent.TextBlock(choice.Message.Content))
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return goagent.CompletionResponse{}, fmt.Errorf("openai: unmarshaling tool call args: %w", err)
			}
		}
		msg.ToolCalls = append(msg.ToolCalls, goagent.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return goagent.CompletionResponse{
		Message:    msg,
		StopReason: toStopReason(choice.FinishReason),
		Usage: goagent.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}, nil
}

func toStopReason(r openaiSDK.FinishReason) goagent.StopReason {
	switch r {
	case openaiSDK.FinishReasonStop:
		return goagent.StopReasonEndTurn
	case openaiSDK.FinishReasonLength:
		return goagent.StopReasonMaxTokens
	case openaiSDK.FinishReasonToolCalls:
		return goagent.StopReasonToolUse
	default:
		return goagent.StopReasonEndTurn
	}
}
