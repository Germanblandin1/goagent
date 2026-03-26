package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/Germanblandin1/goagent"
)

const defaultMaxTokens = 4096

// Provider implements goagent.Provider using the Anthropic Messages API.
type Provider struct {
	client    sdk.Client
	maxTokens int64
}

// Option is a functional option for configuring a Provider.
type Option func(*providerConfig)

type providerConfig struct {
	opts      []option.RequestOption
	maxTokens int64
}

// WithAPIKey sets the Anthropic API key explicitly.
// If not set, the SDK reads ANTHROPIC_API_KEY from the environment.
func WithAPIKey(key string) Option {
	return func(c *providerConfig) {
		c.opts = append(c.opts, option.WithAPIKey(key))
	}
}

// WithBaseURL overrides the Anthropic API base URL.
// Useful for proxies or API-compatible services.
func WithBaseURL(url string) Option {
	return func(c *providerConfig) {
		c.opts = append(c.opts, option.WithBaseURL(url))
	}
}

// WithMaxTokens sets the maximum number of tokens the model may generate per
// completion. Default: 4096.
func WithMaxTokens(n int64) Option {
	return func(c *providerConfig) { c.maxTokens = n }
}

// New creates a Provider that connects to the Anthropic Messages API.
// The model is set at the agent level via goagent.WithModel.
func New(opts ...Option) *Provider {
	cfg := &providerConfig{maxTokens: defaultMaxTokens}
	for _, o := range opts {
		o(cfg)
	}
	return &Provider{
		client:    sdk.NewClient(cfg.opts...),
		maxTokens: cfg.maxTokens,
	}
}

// Complete sends a chat completion request to the Anthropic Messages API and
// returns the model's response.
//
// The model must be set in the request (via goagent.WithModel); if it is
// empty, Complete returns an error immediately.
//
// Possible errors:
//   - error if model is empty
//   - error wrapping the SDK error on API failures
//   - error on message/tool conversion failures
func (p *Provider) Complete(ctx context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	if req.Model == "" {
		return goagent.CompletionResponse{}, fmt.Errorf("anthropic: model not set; use goagent.WithModel")
	}

	messages, err := toAnthropicMessages(req.Messages)
	if err != nil {
		return goagent.CompletionResponse{}, fmt.Errorf("building messages: %w", err)
	}

	params := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		Messages:  messages,
		MaxTokens: p.maxTokens,
	}

	if req.SystemPrompt != "" {
		params.System = []sdk.TextBlockParam{{Text: req.SystemPrompt}}
	}

	if len(req.Tools) > 0 {
		params.Tools = toAnthropicTools(req.Tools)
		params.ToolChoice = sdk.ToolChoiceUnionParam{
			OfAuto: &sdk.ToolChoiceAutoParam{},
		}
	}

	params.Thinking = buildThinkingParam(req.Thinking)
	params.OutputConfig = buildOutputConfig(req.Effort)

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return goagent.CompletionResponse{}, fmt.Errorf("anthropic completion: %w", err)
	}

	return toGoAgentResponse(resp), nil
}

// toAnthropicMessages converts goagent messages to Anthropic SDK messages.
// Tool result messages are folded into the preceding user message as
// ToolResultBlock content blocks, matching Anthropic's API format where tool
// results are content within a user message, not standalone messages.
func toAnthropicMessages(msgs []goagent.Message) ([]sdk.MessageParam, error) {
	var out []sdk.MessageParam

	for i := 0; i < len(msgs); i++ {
		m := msgs[i]

		switch m.Role {
		case goagent.RoleUser:
			blocks, err := toAnthropicBlocks(m.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, sdk.NewUserMessage(blocks...))

		case goagent.RoleAssistant:
			blocks, err := toAssistantBlocks(m)
			if err != nil {
				return nil, err
			}
			out = append(out, sdk.NewAssistantMessage(blocks...))

		case goagent.RoleTool:
			// Collect consecutive tool messages into one user message.
			var toolBlocks []sdk.ContentBlockParamUnion
			for i < len(msgs) && msgs[i].Role == goagent.RoleTool {
				tb, err := toToolResultBlock(msgs[i])
				if err != nil {
					return nil, err
				}
				toolBlocks = append(toolBlocks, tb)
				i++
			}
			i-- // compensate for outer loop increment
			out = append(out, sdk.NewUserMessage(toolBlocks...))

		case goagent.RoleSystem:
			// System messages are handled via the top-level system param.
			// Skip them here.
			continue
		}
	}
	return out, nil
}

// toAnthropicBlocks converts goagent ContentBlocks to Anthropic SDK blocks.
func toAnthropicBlocks(blocks []goagent.ContentBlock) ([]sdk.ContentBlockParamUnion, error) {
	out := make([]sdk.ContentBlockParamUnion, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case goagent.ContentText:
			out = append(out, sdk.NewTextBlock(b.Text))

		case goagent.ContentImage:
			encoded := base64.StdEncoding.EncodeToString(b.Image.Data)
			out = append(out, sdk.NewImageBlockBase64(
				b.Image.MediaType,
				encoded,
			))

		case goagent.ContentDocument:
			block, err := toDocumentBlock(b.Document)
			if err != nil {
				return nil, err
			}
			out = append(out, block)
		}
	}
	return out, nil
}

// toDocumentBlock converts a goagent DocumentData to an Anthropic document block.
func toDocumentBlock(doc *goagent.DocumentData) (sdk.ContentBlockParamUnion, error) {
	switch doc.MediaType {
	case "application/pdf":
		encoded := base64.StdEncoding.EncodeToString(doc.Data)
		block := sdk.NewDocumentBlock(sdk.Base64PDFSourceParam{
			Data: encoded,
		})
		if block.OfDocument != nil && doc.Title != "" {
			block.OfDocument.Title = sdk.String(doc.Title)
		}
		return block, nil

	case "text/plain":
		block := sdk.NewDocumentBlock(sdk.PlainTextSourceParam{
			Data: string(doc.Data),
		})
		if block.OfDocument != nil && doc.Title != "" {
			block.OfDocument.Title = sdk.String(doc.Title)
		}
		return block, nil

	default:
		return sdk.ContentBlockParamUnion{}, fmt.Errorf("anthropic: unsupported document media type %q", doc.MediaType)
	}
}

// toAssistantBlocks converts a goagent assistant message to Anthropic blocks.
// If the message contains tool calls, they are included as ToolUseBlocks.
func toAssistantBlocks(m goagent.Message) ([]sdk.ContentBlockParamUnion, error) {
	var out []sdk.ContentBlockParamUnion

	for _, b := range m.Content {
		switch b.Type {
		case goagent.ContentThinking:
			if b.Thinking != nil {
				// Redacted thinking blocks must be echoed back as RedactedThinkingBlockParam
				// so the API can verify the encrypted data. Regular thinking blocks use
				// ThinkingBlockParam with the opaque signature.
				if b.Thinking.Thinking == "[redacted]" {
					out = append(out, sdk.NewRedactedThinkingBlock(b.Thinking.Signature))
				} else {
					out = append(out, sdk.NewThinkingBlock(b.Thinking.Signature, b.Thinking.Thinking))
				}
			}
		case goagent.ContentText:
			if b.Text != "" {
				out = append(out, sdk.NewTextBlock(b.Text))
			}
		}
	}

	for _, tc := range m.ToolCalls {
		inputJSON, err := json.Marshal(tc.Arguments)
		if err != nil {
			return nil, fmt.Errorf("marshaling tool call args: %w", err)
		}
		out = append(out, sdk.NewToolUseBlock(tc.ID, json.RawMessage(inputJSON), tc.Name))
	}

	return out, nil
}

// toToolResultBlock converts a goagent tool-role message to an Anthropic
// ToolResultBlock.
func toToolResultBlock(m goagent.Message) (sdk.ContentBlockParamUnion, error) {
	text := m.TextContent()
	isError := len(text) > 0 && len(text) > 6 && text[:6] == "Error:"
	return sdk.NewToolResultBlock(m.ToolCallID, text, isError), nil
}

// toAnthropicTools converts goagent tool definitions to Anthropic SDK tools.
func toAnthropicTools(defs []goagent.ToolDefinition) []sdk.ToolUnionParam {
	out := make([]sdk.ToolUnionParam, len(defs))
	for i, d := range defs {
		schema := sdk.ToolInputSchemaParam{
			Properties: d.Parameters["properties"],
		}
		if req, ok := d.Parameters["required"].([]string); ok {
			schema.Required = req
		}
		// Pass through any additional schema fields.
		if extra, ok := d.Parameters["additionalProperties"]; ok {
			if schema.ExtraFields == nil {
				schema.ExtraFields = make(map[string]any)
			}
			schema.ExtraFields["additionalProperties"] = extra
		}

		out[i] = sdk.ToolUnionParam{
			OfTool: &sdk.ToolParam{
				Name:        d.Name,
				Description: sdk.String(d.Description),
				InputSchema: schema,
			},
		}
	}
	return out
}

// toGoAgentResponse converts an Anthropic SDK response to goagent types.
func toGoAgentResponse(resp *sdk.Message) goagent.CompletionResponse {
	msg := goagent.Message{
		Role: goagent.RoleAssistant,
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			msg.Content = append(msg.Content, goagent.TextBlock(block.Text))
		case "thinking":
			msg.Content = append(msg.Content, goagent.ThinkingBlock(block.Thinking, block.Signature))
		case "redacted_thinking":
			// The model redacted its reasoning for safety reasons. Preserve it
			// as a thinking block so it can be echoed back to the API. The
			// encrypted data is stored in Signature (treated as opaque token).
			msg.Content = append(msg.Content, goagent.ThinkingBlock("[redacted]", block.Data))
		case "tool_use":
			var args map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &args)
			}
			msg.ToolCalls = append(msg.ToolCalls, goagent.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}

	// Ensure Content is never nil.
	if msg.Content == nil {
		msg.Content = []goagent.ContentBlock{}
	}

	return goagent.CompletionResponse{
		Message:    msg,
		StopReason: toStopReason(resp.StopReason),
		Usage: goagent.Usage{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
		},
	}
}

func toStopReason(r sdk.StopReason) goagent.StopReason {
	switch r {
	case sdk.StopReasonEndTurn:
		return goagent.StopReasonEndTurn
	case sdk.StopReasonMaxTokens:
		return goagent.StopReasonMaxTokens
	case sdk.StopReasonToolUse:
		return goagent.StopReasonToolUse
	default:
		return goagent.StopReasonEndTurn
	}
}

// buildThinkingParam translates a goagent ThinkingConfig to the SDK union type.
// Returns the zero value (omitted by omitzero) when thinking is disabled.
func buildThinkingParam(cfg *goagent.ThinkingConfig) sdk.ThinkingConfigParamUnion {
	if cfg == nil || !cfg.Enabled {
		return sdk.ThinkingConfigParamUnion{}
	}
	if cfg.BudgetTokens > 0 {
		// Manual mode: fixed token budget.
		return sdk.ThinkingConfigParamOfEnabled(int64(cfg.BudgetTokens))
	}
	// Adaptive mode: the model decides how much to reason.
	return sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{}}
}

// buildOutputConfig translates an effort string to the SDK OutputConfigParam.
// Returns the zero value (omitted by omitzero) when effort is not configured.
func buildOutputConfig(effort string) sdk.OutputConfigParam {
	if effort == "" {
		return sdk.OutputConfigParam{}
	}
	return sdk.OutputConfigParam{Effort: sdk.OutputConfigEffort(effort)}
}
