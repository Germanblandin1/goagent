// Package ollama provides a goagent Provider that targets a locally running
// Ollama instance via its OpenAI-compatible API.
package ollama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	openai "github.com/sashabaranov/go-openai"

	"github.com/Germanblandin1/goagent"
)

const defaultBaseURL = "http://localhost:11434/v1"

// Provider implements goagent.Provider using Ollama's OpenAI-compatible endpoint.
type Provider struct {
	client *openai.Client
}

// Option is a functional option for configuring a Provider.
type Option func(*Provider)

// WithBaseURL overrides the Ollama base URL. The default is
// "http://localhost:11434/v1" (the standard local Ollama endpoint).
// Use this option when Ollama runs on a different host or port, or when
// targeting a remote Ollama instance.
func WithBaseURL(url string) Option {
	return func(p *Provider) {
		cfg := openai.DefaultConfig("ollama")
		cfg.BaseURL = url
		p.client = openai.NewClientWithConfig(cfg)
	}
}

// New creates a Provider connecting to Ollama at the default base URL.
// The model is set at the agent level via goagent.WithModel.
func New(opts ...Option) *Provider {
	cfg := openai.DefaultConfig("ollama") // Ollama does not validate the token.
	cfg.BaseURL = defaultBaseURL

	p := &Provider{client: openai.NewClientWithConfig(cfg)}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Complete sends a chat completion request to the Ollama API and returns the
// model's response.
//
// The model must be set in the request (via goagent.WithModel); if it is empty,
// Complete returns an error immediately without making a network call.
//
// If any message contains ContentDocument blocks, Complete returns an
// *UnsupportedContentError because the OpenAI-compatible API does not support
// document content.
//
// Cancellation and timeout are controlled entirely by ctx — pass a
// context.WithTimeout or context.WithDeadline to bound the request duration.
// There is no built-in retry: if the request fails (e.g., Ollama is not
// running, the model is not pulled, or the network times out), the underlying
// error is returned wrapped as "ollama completion: <cause>". A connection
// refused error typically means Ollama is not running on the configured URL.
func (p *Provider) Complete(ctx context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	messages, err := toOpenAIMessages(req)
	if err != nil {
		return goagent.CompletionResponse{}, fmt.Errorf("building messages: %w", err)
	}

	if req.Model == "" {
		return goagent.CompletionResponse{}, fmt.Errorf("ollama: model not set; use goagent.WithModel")
	}

	chatReq := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
	}

	if len(req.Tools) > 0 {
		chatReq.Tools = toOpenAITools(req.Tools)
		chatReq.ToolChoice = "auto"
	}

	resp, err := p.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return goagent.CompletionResponse{}, fmt.Errorf("ollama completion: %w", err)
	}

	return toGoAgentResponse(resp)
}

// toOpenAIMessages converts a CompletionRequest to the message slice expected
// by the go-openai SDK. The system prompt is prepended as the first message.
func toOpenAIMessages(req goagent.CompletionRequest) ([]openai.ChatCompletionMessage, error) {
	var out []openai.ChatCompletionMessage

	if req.SystemPrompt != "" {
		out = append(out, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
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

func toOpenAIMessage(m goagent.Message) (openai.ChatCompletionMessage, error) {
	// Check for unsupported document content.
	if m.HasContentType(goagent.ContentDocument) {
		return openai.ChatCompletionMessage{}, &goagent.UnsupportedContentError{
			ContentType: goagent.ContentDocument,
			Provider:    "ollama",
			Reason:      "OpenAI-compatible API does not support document content",
		}
	}

	msg := openai.ChatCompletionMessage{
		Role:       toOpenAIRole(m.Role),
		ToolCallID: m.ToolCallID,
	}

	// Optimization: if the message has a single text block, use the simple
	// Content field for maximum compatibility with all models.
	if len(m.Content) == 1 && m.Content[0].Type == goagent.ContentText {
		msg.Content = m.Content[0].Text
	} else if len(m.Content) > 0 {
		parts, err := toOpenAIParts(m.Content)
		if err != nil {
			return openai.ChatCompletionMessage{}, err
		}
		msg.MultiContent = parts
	}

	for _, tc := range m.ToolCalls {
		argsJSON, err := json.Marshal(tc.Arguments)
		if err != nil {
			return openai.ChatCompletionMessage{}, fmt.Errorf("marshaling tool call args: %w", err)
		}
		msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
			ID:   tc.ID,
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      tc.Name,
				Arguments: string(argsJSON),
			},
		})
	}
	return msg, nil
}

// toOpenAIParts translates goagent ContentBlocks to go-openai ChatMessageParts.
func toOpenAIParts(blocks []goagent.ContentBlock) ([]openai.ChatMessagePart, error) {
	parts := make([]openai.ChatMessagePart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case goagent.ContentText:
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeText,
				Text: b.Text,
			})
		case goagent.ContentImage:
			dataURL := fmt.Sprintf("data:%s;base64,%s",
				b.Image.MediaType,
				base64.StdEncoding.EncodeToString(b.Image.Data),
			)
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL: dataURL,
				},
			})
		case goagent.ContentDocument:
			// Should not reach here — checked at message level.
			return nil, &goagent.UnsupportedContentError{
				ContentType: goagent.ContentDocument,
				Provider:    "ollama",
				Reason:      "OpenAI-compatible API does not support document content",
			}
		}
	}
	return parts, nil
}

func toOpenAIRole(r goagent.Role) string {
	switch r {
	case goagent.RoleSystem:
		return openai.ChatMessageRoleSystem
	case goagent.RoleAssistant:
		return openai.ChatMessageRoleAssistant
	case goagent.RoleTool:
		return openai.ChatMessageRoleTool
	default:
		return openai.ChatMessageRoleUser
	}
}

func toOpenAITools(defs []goagent.ToolDefinition) []openai.Tool {
	out := make([]openai.Tool, len(defs))
	for i, d := range defs {
		out[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Parameters,
			},
		}
	}
	return out
}

func toGoAgentResponse(resp openai.ChatCompletionResponse) (goagent.CompletionResponse, error) {
	if len(resp.Choices) == 0 {
		return goagent.CompletionResponse{}, fmt.Errorf("ollama: empty choices in response")
	}

	choice := resp.Choices[0]
	msg := goagent.Message{
		Role:    goagent.RoleAssistant,
		Content: []goagent.ContentBlock{goagent.TextBlock(choice.Message.Content)},
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return goagent.CompletionResponse{}, fmt.Errorf("unmarshaling tool call args: %w", err)
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

func toStopReason(r openai.FinishReason) goagent.StopReason {
	switch r {
	case openai.FinishReasonStop:
		return goagent.StopReasonEndTurn
	case openai.FinishReasonLength:
		return goagent.StopReasonMaxTokens
	case openai.FinishReasonToolCalls:
		return goagent.StopReasonToolUse
	default:
		return goagent.StopReasonEndTurn
	}
}
