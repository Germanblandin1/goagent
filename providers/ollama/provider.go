// Package ollama provides a goagent Provider that targets a locally running
// Ollama instance via its OpenAI-compatible API.
package ollama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/Germanblandin1/goagent"
)

// Provider implements goagent.Provider using Ollama's OpenAI-compatible endpoint.
type Provider struct {
	client *OllamaClient
	model  string
}

// ProviderOption is a functional option for configuring a Provider.
type ProviderOption func(*Provider)

// WithModel sets a default model on the Provider. It is used when the
// CompletionRequest does not specify a model. The per-request model always
// takes precedence.
func WithModel(model string) ProviderOption {
	return func(p *Provider) { p.model = model }
}

// New creates a Provider with a default OllamaClient targeting localhost:11434.
// For custom HTTP settings (timeout, base URL, transport), create a client
// with NewClient and pass it to NewWithClient instead.
func New(opts ...ProviderOption) *Provider {
	return NewWithClient(NewClient(), opts...)
}

// NewWithClient creates a Provider that delegates all HTTP to client.
// Use this when you need to share a client between Provider and OllamaEmbedder,
// or when the default OllamaClient settings are not sufficient.
func NewWithClient(client *OllamaClient, opts ...ProviderOption) *Provider {
	p := &Provider{client: client}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ollamaMessage is a raw Ollama API message that captures the reasoning field
// which the go-openai SDK silently drops.
type ollamaMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Reasoning  string            `json:"reasoning,omitempty"` // Ollama-specific thinking field
	ToolCalls  []openai.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type ollamaChoice struct {
	Message      ollamaMessage       `json:"message"`
	FinishReason openai.FinishReason `json:"finish_reason"`
}

type ollamaResponse struct {
	Choices []ollamaChoice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete sends a chat completion request to the Ollama API and returns the
// model's response.
//
// The model is resolved from the request first, then from the Provider's
// WithModel option; if both are empty, Complete returns an error without
// making a network call.
//
// If any message contains ContentDocument blocks, Complete returns an
// *UnsupportedContentError because the OpenAI-compatible API does not support
// document content.
//
// Cancellation and timeout are controlled entirely by ctx. There is no
// built-in retry: network or Ollama errors are returned wrapped in a
// descriptive message.
func (p *Provider) Complete(ctx context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return goagent.CompletionResponse{}, fmt.Errorf("ollama: model not set; use goagent.WithModel")
	}

	messages, err := toOpenAIMessages(req)
	if err != nil {
		return goagent.CompletionResponse{}, fmt.Errorf("building messages: %w", err)
	}

	chatReq := openai.ChatCompletionRequest{
		Model:    model,
		Messages: messages,
	}

	if len(req.Tools) > 0 {
		chatReq.Tools = toOpenAITools(req.Tools)
		chatReq.ToolChoice = "auto"
	}

	var resp ollamaResponse
	if err := p.client.do(ctx, "/v1/chat/completions", chatReq, &resp); err != nil {
		return goagent.CompletionResponse{}, err
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

	role, err := toOpenAIRole(m.Role)
	if err != nil {
		return openai.ChatCompletionMessage{}, err
	}
	msg := openai.ChatCompletionMessage{
		Role:       role,
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
		case goagent.ContentThinking:
			// Local models do not expect thinking blocks echoed back —
			// discard silently.
			continue
		}
	}
	return parts, nil
}

func toOpenAIRole(r goagent.Role) (string, error) {
	switch r {
	case goagent.RoleUser:
		return openai.ChatMessageRoleUser, nil
	case goagent.RoleSystem:
		return openai.ChatMessageRoleSystem, nil
	case goagent.RoleAssistant:
		return openai.ChatMessageRoleAssistant, nil
	case goagent.RoleTool:
		return openai.ChatMessageRoleTool, nil
	default:
		return "", fmt.Errorf("goagent/ollama: unsupported role %q — only user/assistant/tool/system are valid in conversation history", r)
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

func toGoAgentResponse(resp ollamaResponse) (goagent.CompletionResponse, error) {
	if len(resp.Choices) == 0 {
		return goagent.CompletionResponse{}, fmt.Errorf("ollama: empty choices in response")
	}

	choice := resp.Choices[0]

	// Build content blocks. The reasoning field takes priority over <think>
	// tags in the content text — some Ollama models use one mechanism, some
	// use the other, and some use both. We avoid duplicating the thinking.
	var content []goagent.ContentBlock
	if choice.Message.Reasoning != "" {
		// Model returned thinking in the dedicated reasoning field.
		content = append(content, goagent.ThinkingBlock(strings.TrimSpace(choice.Message.Reasoning), ""))
		if text := strings.TrimSpace(choice.Message.Content); text != "" {
			content = append(content, goagent.TextBlock(text))
		}
	} else {
		// Fall back to parsing <think>...</think> tags from the content text.
		content = parseThinkingFromText(choice.Message.Content)
	}

	msg := goagent.Message{
		Role:    goagent.RoleAssistant,
		Content: content,
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

// parseThinkingFromText parses a model response that may contain a
// <think>...</think> reasoning block at the start of the text.
// This is the mechanism used by local thinking models (QwQ, DeepSeek-R1,
// Phi-4-reasoning) — they emit reasoning as inline tags, not as a separate
// content type. The Signature field is always empty for local models.
//
// Malformed tags (unclosed, empty content) are returned as plain text
// without panicking.
func parseThinkingFromText(text string) []goagent.ContentBlock {
	const openTag = "<think>"
	const closeTag = "</think>"

	thinkStart := strings.Index(text, openTag)
	if thinkStart == -1 {
		return []goagent.ContentBlock{goagent.TextBlock(text)}
	}

	thinkEnd := strings.Index(text, closeTag)
	if thinkEnd == -1 {
		// Unclosed tag — treat entire response as plain text.
		return []goagent.ContentBlock{goagent.TextBlock(text)}
	}

	var blocks []goagent.ContentBlock

	thinking := strings.TrimSpace(text[thinkStart+len(openTag) : thinkEnd])
	if thinking != "" {
		blocks = append(blocks, goagent.ThinkingBlock(thinking, ""))
	}

	rest := strings.TrimSpace(text[thinkEnd+len(closeTag):])
	if rest != "" {
		blocks = append(blocks, goagent.TextBlock(rest))
	}

	if len(blocks) == 0 {
		// Both thinking and rest were empty — return an empty text block
		// to avoid a nil Content slice.
		blocks = append(blocks, goagent.TextBlock(""))
	}
	return blocks
}
