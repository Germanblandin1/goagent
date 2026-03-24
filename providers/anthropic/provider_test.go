package anthropic_test

import (
	"context"
	"encoding/json"
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
	return provider.New(
		provider.WithBaseURL(srv.URL),
		provider.WithAPIKey("test-key"),
	)
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
