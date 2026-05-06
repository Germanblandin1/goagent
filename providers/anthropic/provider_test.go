package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Germanblandin1/goagent"
	provider "github.com/Germanblandin1/goagent/providers/anthropic"
)

// fakeAnthropicServer starts an httptest server that responds to
// POST /v1/messages with the provided raw JSON body.
func fakeAnthropicServer(t *testing.T, responseJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(responseJSON)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// capturingServer saves the decoded request body into out and responds with
// responseJSON.
func capturingServer(t *testing.T, responseJSON string, out *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(out)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(responseJSON)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const textResponse = `{
  "id": "msg_01",
  "type": "message",
  "role": "assistant",
  "content": [{"type": "text", "text": "hello from anthropic"}],
  "model": "claude-sonnet-4-6",
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {"input_tokens": 10, "output_tokens": 5}
}`

func newTestProvider(t *testing.T, srv *httptest.Server) *provider.Provider {
	t.Helper()
	client := provider.NewClient(
		provider.WithBaseURL(srv.URL),
		provider.WithAPIKey("test-key"),
	)
	return provider.NewWithClient(client)
}

func TestProvider_SimpleResponse(t *testing.T) {
	t.Parallel()

	srv := fakeAnthropicServer(t, textResponse)
	p := newTestProvider(t, srv)

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.TextContent() != "hello from anthropic" {
		t.Errorf("content = %q, want %q", resp.Message.TextContent(), "hello from anthropic")
	}
	if resp.StopReason != goagent.StopReasonEndTurn {
		t.Errorf("stop reason = %v, want EndTurn", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want {10 5}", resp.Usage)
	}
}

func TestProvider_ToolCallResponse(t *testing.T) {
	t.Parallel()

	body := `{
	  "id": "msg_02",
	  "type": "message",
	  "role": "assistant",
	  "content": [
	    {"type": "text", "text": "I'll calculate that."},
	    {"type": "tool_use", "id": "toolu_01", "name": "calc", "input": {"a": 2, "b": 3}}
	  ],
	  "model": "claude-sonnet-4-6",
	  "stop_reason": "tool_use",
	  "stop_sequence": null,
	  "usage": {"input_tokens": 15, "output_tokens": 8}
	}`

	srv := fakeAnthropicServer(t, body)
	p := newTestProvider(t, srv)

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("compute")},
		Tools:    []goagent.ToolDefinition{{Name: "calc", Description: "arithmetic", Parameters: map[string]any{}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != goagent.StopReasonToolUse {
		t.Errorf("stop reason = %v, want ToolUse", resp.StopReason)
	}
	if resp.Message.TextContent() != "I'll calculate that." {
		t.Errorf("text = %q, want %q", resp.Message.TextContent(), "I'll calculate that.")
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "toolu_01" || tc.Name != "calc" {
		t.Errorf("tool call id/name = %q/%q, want toolu_01/calc", tc.ID, tc.Name)
	}
	if tc.Arguments["a"] != float64(2) || tc.Arguments["b"] != float64(3) {
		t.Errorf("arguments = %v, want {a:2 b:3}", tc.Arguments)
	}
}

func TestProvider_SystemPrompt(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:        "claude-sonnet-4-6",
		SystemPrompt: "be concise",
		Messages:     []goagent.Message{goagent.UserMessage("hi")},
	})

	system, ok := captured["system"].([]any)
	if !ok || len(system) == 0 {
		t.Fatal("expected system field in request body")
	}
	first, _ := system[0].(map[string]any)
	if first["text"] != "be concise" {
		t.Errorf("system text = %v, want %q", first["text"], "be concise")
	}
}

func TestProvider_StopReasonMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		stopReason string
		want       goagent.StopReason
	}{
		{"end_turn", goagent.StopReasonEndTurn},
		{"max_tokens", goagent.StopReasonMaxTokens},
		{"tool_use", goagent.StopReasonToolUse},
		{"stop_sequence", goagent.StopReasonEndTurn},
	}

	for _, tc := range cases {
		t.Run(tc.stopReason, func(t *testing.T) {
			t.Parallel()

			body := `{
				"id":"msg_x","type":"message","role":"assistant",
				"content":[{"type":"text","text":"ok"}],
				"model":"claude-sonnet-4-6","stop_reason":"` + tc.stopReason + `",
				"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}
			}`
			srv := fakeAnthropicServer(t, body)
			p := newTestProvider(t, srv)

			resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
				Model:    "claude-sonnet-4-6",
				Messages: []goagent.Message{goagent.UserMessage("x")},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StopReason != tc.want {
				t.Errorf("stop reason = %v, want %v", resp.StopReason, tc.want)
			}
		})
	}
}

func TestProvider_EmptyModel_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := fakeAnthropicServer(t, textResponse)
	p := newTestProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err == nil {
		t.Fatal("expected error for empty model, got nil")
	}
}

func TestProvider_ToolDefinitionsSent(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Tools: []goagent.ToolDefinition{
			{
				Name:        "greet",
				Description: "says hello",
				Parameters: map[string]any{
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
					"required": []string{"name"},
				},
			},
		},
	})

	tools, ok := captured["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("expected tools in request body")
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "greet" {
		t.Errorf("tool name = %v, want %q", tool["name"], "greet")
	}
}

func TestProvider_ToolResultMessages(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			goagent.UserMessage("compute 2+3"),
			{
				Role:    goagent.RoleAssistant,
				Content: []goagent.ContentBlock{goagent.TextBlock("I'll use the calculator")},
				ToolCalls: []goagent.ToolCall{
					{ID: "toolu_01", Name: "calc", Arguments: map[string]any{"a": 2, "b": 3}},
				},
			},
			{
				Role:       goagent.RoleTool,
				Content:    []goagent.ContentBlock{goagent.TextBlock("5")},
				ToolCallID: "toolu_01",
			},
		},
	})

	messages, ok := captured["messages"].([]any)
	if !ok {
		t.Fatal("expected messages in request body")
	}
	// Should be: user, assistant, user (with tool_result)
	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}

	// Third message should be a user message containing a tool_result block.
	third, _ := messages[2].(map[string]any)
	if third["role"] != "user" {
		t.Errorf("third message role = %v, want user", third["role"])
	}
	content, _ := third["content"].([]any)
	if len(content) == 0 {
		t.Fatal("third message has no content blocks")
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "tool_result" {
		t.Errorf("block type = %v, want tool_result", block["type"])
	}
	if block["tool_use_id"] != "toolu_01" {
		t.Errorf("tool_use_id = %v, want toolu_01", block["tool_use_id"])
	}
}

// ── Extended Thinking & Effort ───────────────────────────────────────────────

func TestProvider_ThinkingConfigManual_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Thinking: &goagent.ThinkingConfig{Enabled: true, BudgetTokens: 8000},
	})

	thinking, ok := captured["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("'thinking' field missing or not an object in request; captured: %v", captured)
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v, want %q", thinking["type"], "enabled")
	}
	budget, _ := thinking["budget_tokens"].(float64)
	if budget != 8000 {
		t.Errorf("thinking.budget_tokens = %v, want 8000", thinking["budget_tokens"])
	}
}

func TestProvider_ThinkingConfigAdaptive_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-opus-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Thinking: &goagent.ThinkingConfig{Enabled: true, BudgetTokens: 0},
	})

	thinking, ok := captured["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("'thinking' field missing or not an object; captured: %v", captured)
	}
	if thinking["type"] != "adaptive" {
		t.Errorf("thinking.type = %v, want %q", thinking["type"], "adaptive")
	}
}

func TestProvider_NoThinking_FieldOmitted(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		// No Thinking field.
	})

	if _, present := captured["thinking"]; present {
		t.Errorf("'thinking' field should be omitted when not configured, but was present: %v", captured["thinking"])
	}
}

func TestProvider_Effort_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Effort:   "medium",
	})

	outputConfig, ok := captured["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("'output_config' field missing or not an object; captured: %v", captured)
	}
	if outputConfig["effort"] != "medium" {
		t.Errorf("output_config.effort = %v, want %q", outputConfig["effort"], "medium")
	}
}

func TestProvider_NoEffort_FieldOmitted(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})

	if _, present := captured["output_config"]; present {
		t.Errorf("'output_config' should be omitted when effort is empty, but was: %v", captured["output_config"])
	}
}

func TestProvider_ThinkingBlockInResponse(t *testing.T) {
	t.Parallel()

	body := `{
	  "id": "msg_think",
	  "type": "message",
	  "role": "assistant",
	  "content": [
	    {"type": "thinking", "thinking": "let me reason...", "signature": "sigABC"},
	    {"type": "text", "text": "the answer"}
	  ],
	  "model": "claude-sonnet-4-6",
	  "stop_reason": "end_turn",
	  "usage": {"input_tokens": 10, "output_tokens": 20}
	}`
	srv := fakeAnthropicServer(t, body)
	p := newTestProvider(t, srv)

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Message.HasContentType(goagent.ContentThinking) {
		t.Fatal("response does not contain a thinking block")
	}
	var thinkingBlock goagent.ContentBlock
	for _, b := range resp.Message.Content {
		if b.Type == goagent.ContentThinking {
			thinkingBlock = b
			break
		}
	}
	if thinkingBlock.Thinking == nil {
		t.Fatal("ContentBlock.Thinking is nil")
	}
	if thinkingBlock.Thinking.Thinking != "let me reason..." {
		t.Errorf("Thinking.Thinking = %q, want %q", thinkingBlock.Thinking.Thinking, "let me reason...")
	}
	if thinkingBlock.Thinking.Signature != "sigABC" {
		t.Errorf("Thinking.Signature = %q, want %q", thinkingBlock.Thinking.Signature, "sigABC")
	}
	if resp.Message.TextContent() != "the answer" {
		t.Errorf("TextContent() = %q, want %q", resp.Message.TextContent(), "the answer")
	}
}

func TestProvider_RedactedThinkingBlockInResponse(t *testing.T) {
	t.Parallel()

	body := `{
	  "id": "msg_redacted",
	  "type": "message",
	  "role": "assistant",
	  "content": [
	    {"type": "redacted_thinking", "data": "encryptedABC"},
	    {"type": "text", "text": "answer despite redaction"}
	  ],
	  "model": "claude-sonnet-4-6",
	  "stop_reason": "end_turn",
	  "usage": {"input_tokens": 5, "output_tokens": 10}
	}`
	srv := fakeAnthropicServer(t, body)
	p := newTestProvider(t, srv)

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Message.HasContentType(goagent.ContentThinking) {
		t.Fatal("response does not contain a thinking block for the redacted block")
	}
	var thinkingBlock goagent.ContentBlock
	for _, b := range resp.Message.Content {
		if b.Type == goagent.ContentThinking {
			thinkingBlock = b
			break
		}
	}
	if thinkingBlock.Thinking == nil {
		t.Fatal("ContentBlock.Thinking is nil")
	}
	if thinkingBlock.Thinking.Thinking != "[redacted]" {
		t.Errorf("Thinking.Thinking = %q, want %q", thinkingBlock.Thinking.Thinking, "[redacted]")
	}
	// data field stored as Signature so it can be echoed back correctly.
	if thinkingBlock.Thinking.Signature != "encryptedABC" {
		t.Errorf("Thinking.Signature = %q, want %q", thinkingBlock.Thinking.Signature, "encryptedABC")
	}
}

func TestProvider_ThinkingBlocksPassedBackToAPI(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			goagent.UserMessage("what is 2+2"),
			{
				Role: goagent.RoleAssistant,
				Content: []goagent.ContentBlock{
					goagent.ThinkingBlock("I need to add 2+2", "sigXYZ"),
					goagent.TextBlock(""),
				},
				ToolCalls: []goagent.ToolCall{
					{ID: "t1", Name: "calc", Arguments: map[string]any{"a": 2, "b": 2}},
				},
			},
			{Role: goagent.RoleTool, Content: []goagent.ContentBlock{goagent.TextBlock("4")}, ToolCallID: "t1"},
		},
	})

	messages, ok := captured["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatal("no messages in captured request")
	}

	// Find the assistant message in the captured JSON.
	var foundThinkingInAssistant bool
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] != "assistant" {
			continue
		}
		content, _ := msg["content"].([]any)
		for _, cb := range content {
			block, _ := cb.(map[string]any)
			if block["type"] == "thinking" {
				foundThinkingInAssistant = true
				if block["signature"] != "sigXYZ" {
					t.Errorf("thinking block signature = %v, want %q", block["signature"], "sigXYZ")
				}
			}
		}
	}
	if !foundThinkingInAssistant {
		t.Error("thinking block not found in assistant message sent to API")
	}
}

// ── New and WithMaxTokens ────────────────────────────────────────────────────

func TestNew_ReturnsNonNilProvider(t *testing.T) {
	t.Parallel()
	// API key is read from the environment (may be empty in CI, but New does
	// not validate it at construction time).
	p := provider.New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
}

func TestWithMaxTokens_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)

	client := provider.NewClient(
		provider.WithBaseURL(srv.URL),
		provider.WithAPIKey("test-key"),
	)
	p := provider.NewWithClient(client, provider.WithMaxTokens(8192))

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})

	maxTokens, _ := captured["max_tokens"].(float64)
	if maxTokens != 8192 {
		t.Errorf("max_tokens = %v, want 8192", maxTokens)
	}
}

// ── Content blocks: image and document ──────────────────────────────────────

func TestProvider_ImageBlock_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	imgBytes := []byte{0xFF, 0xD8, 0xFF} // fake JPEG header
	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.ImageBlock(imgBytes, "image/jpeg")},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	if len(messages) == 0 {
		t.Fatal("no messages in captured request")
	}
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) == 0 {
		t.Fatal("no content blocks in first message")
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "image" {
		t.Errorf("block type = %v, want image", block["type"])
	}
}

func TestProvider_DocumentPDF_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.DocumentBlock([]byte{0x25, 0x50, 0x44, 0x46}, "application/pdf", "report"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) == 0 {
		t.Fatal("no content blocks")
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "document" {
		t.Errorf("block type = %v, want document", block["type"])
	}
}

func TestProvider_DocumentPlainText_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.DocumentBlock([]byte("plain text content"), "text/plain", "notes"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) == 0 {
		t.Fatal("no content blocks")
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "document" {
		t.Errorf("block type = %v, want document", block["type"])
	}
}

func TestProvider_DocumentUnsupportedType_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := fakeAnthropicServer(t, textResponse)
	p := newTestProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.DocumentBlock([]byte("<html>"), "text/html", "page"),
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for unsupported document media type, got nil")
	}
}

// ── Message role handling ────────────────────────────────────────────────────

func TestProvider_UnsupportedRole_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := fakeAnthropicServer(t, textResponse)
	p := newTestProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			{Role: "invalid_role", Content: []goagent.ContentBlock{goagent.TextBlock("hi")}},
		},
	})
	if err == nil {
		t.Fatal("expected error for unsupported role, got nil")
	}
}

func TestProvider_SystemRoleInMessages_Skipped(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []goagent.Message{
			// System messages in conversation history are skipped;
			// they must be passed via CompletionRequest.SystemPrompt instead.
			{Role: goagent.RoleSystem, Content: []goagent.ContentBlock{goagent.TextBlock("be helpful")}},
			goagent.UserMessage("hi"),
		},
	})

	messages, _ := captured["messages"].([]any)
	// Only the user message should be forwarded; the system message is dropped.
	if len(messages) != 1 {
		t.Errorf("got %d messages, want 1 (system message skipped)", len(messages))
	}
}

// ── Tool schema: additionalProperties ───────────────────────────────────────

func TestProvider_ToolWithAdditionalProperties(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, textResponse, &captured)
	p := newTestProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Tools: []goagent.ToolDefinition{
			{
				Name:        "strict_tool",
				Description: "strict schema",
				Parameters: map[string]any{
					"properties":           map[string]any{"x": map[string]any{"type": "number"}},
					"required":             []string{"x"},
					"additionalProperties": false,
				},
			},
		},
	})

	tools, _ := captured["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("no tools in request")
	}
	tool, _ := tools[0].(map[string]any)
	schema, _ := tool["input_schema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false", schema["additionalProperties"])
	}
}

// ── Streaming ────────────────────────────────────────────────────────────────

func TestCompleteStream_EmptyModel_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := fakeAnthropicServer(t, textResponse)
	p := newTestProvider(t, srv)

	_, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err == nil {
		t.Fatal("expected error for empty model, got nil")
	}
}

// sseServer returns an httptest.Server that streams the given SSE event
// strings (each already formatted as "event: X\ndata: Y") to any POST
// /v1/messages request.
func sseServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "%s\n\n", ev)
		}
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCompleteStream_TextEvents(t *testing.T) {
	t.Parallel()

	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}

	srv := sseServer(t, events)
	p := newTestProvider(t, srv)

	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error creating stream: %v", err)
	}
	defer stream.Close()

	var texts []string
	for stream.Next(ctx) {
		ev := stream.Event()
		if ev.Type == goagent.StreamEventText {
			texts = append(texts, ev.Text)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(texts) == 0 || texts[0] != "Hello" {
		t.Errorf("text events = %v, want [Hello]", texts)
	}
}

func TestCompleteStream_ToolStartAndDeltaEvents(t *testing.T) {
	t.Parallel()

	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"msg_02","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":15,"output_tokens":0}}}`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"calc","input":{}}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"2}"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":5}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}

	srv := sseServer(t, events)
	p := newTestProvider(t, srv)

	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, goagent.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []goagent.Message{goagent.UserMessage("compute")},
		Tools:    []goagent.ToolDefinition{{Name: "calc", Description: "arithmetic", Parameters: map[string]any{}}},
	})
	if err != nil {
		t.Fatalf("unexpected error creating stream: %v", err)
	}
	defer stream.Close()

	var toolStarts, toolDeltas int
	var stopReason goagent.StopReason
	for stream.Next(ctx) {
		ev := stream.Event()
		switch ev.Type {
		case goagent.StreamEventToolStart:
			toolStarts++
		case goagent.StreamEventToolDelta:
			toolDeltas++
		case goagent.StreamEventDone:
			stopReason = ev.StopReason
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if toolStarts != 1 {
		t.Errorf("tool start events = %d, want 1", toolStarts)
	}
	if toolDeltas == 0 {
		t.Error("expected at least one tool delta event, got none")
	}
	if stopReason != goagent.StopReasonToolUse {
		t.Errorf("stop reason = %v, want ToolUse", stopReason)
	}
}
