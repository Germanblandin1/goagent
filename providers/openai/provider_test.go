package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/openai"
)

// fakeServer starts an httptest server that responds to POST /chat/completions
// with the provided raw JSON body. go-openai constructs the URL as baseURL+"/chat/completions".
func fakeServer(t *testing.T, responseJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
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

// capturingServer starts a server that saves the decoded request body into
// out and responds with responseJSON.
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

const stopResponse = `{
  "choices":[{"message":{"role":"assistant","content":"hello from openai"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":5,"completion_tokens":3}
}`

func newProvider(t *testing.T, srv *httptest.Server) *openai.Provider {
	t.Helper()
	return openai.New(openai.WithBaseURL(srv.URL), openai.WithAPIKey("test-key"))
}

func TestProvider_SimpleResponse(t *testing.T) {
	t.Parallel()

	srv := fakeServer(t, stopResponse)
	p := newProvider(t, srv)

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.TextContent() != "hello from openai" {
		t.Errorf("content = %q, want %q", resp.Message.TextContent(), "hello from openai")
	}
	if resp.StopReason != goagent.StopReasonEndTurn {
		t.Errorf("stop reason = %v, want EndTurn", resp.StopReason)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want {5 3}", resp.Usage)
	}
}

func TestProvider_EmptyModel_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := fakeServer(t, stopResponse)
	p := newProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err == nil {
		t.Fatal("expected error for empty model, got nil")
	}
	if !strings.Contains(err.Error(), "model not set") {
		t.Errorf("error = %q, want to contain 'model not set'", err.Error())
	}
}

func TestProvider_ToolCallResponse(t *testing.T) {
	t.Parallel()

	body := `{
	  "choices":[{
	    "message":{
	      "role":"assistant",
	      "tool_calls":[{"id":"call_1","type":"function","function":{"name":"calc","arguments":"{\"a\":2,\"b\":3}"}}]
	    },
	    "finish_reason":"tool_calls"
	  }]
	}`
	srv := fakeServer(t, body)
	p := newProvider(t, srv)

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("compute")},
		Tools:    []goagent.ToolDefinition{{Name: "calc", Description: "arithmetic", Parameters: map[string]any{}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != goagent.StopReasonToolUse {
		t.Errorf("stop reason = %v, want ToolUse", resp.StopReason)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "calc" {
		t.Errorf("tool call id/name = %q/%q, want call_1/calc", tc.ID, tc.Name)
	}
	if tc.Arguments["a"] != float64(2) || tc.Arguments["b"] != float64(3) {
		t.Errorf("arguments = %v, want {a:2 b:3}", tc.Arguments)
	}
}

func TestProvider_SystemPromptPrepended(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:        "gpt-4o",
		SystemPrompt: "be concise",
		Messages:     []goagent.Message{goagent.UserMessage("hi")},
	})

	messages, _ := captured["messages"].([]any)
	if len(messages) == 0 {
		t.Fatal("no messages in request body")
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" {
		t.Errorf("first message role = %v, want system", first["role"])
	}
	if first["content"] != "be concise" {
		t.Errorf("first message content = %v, want %q", first["content"], "be concise")
	}
}

func TestProvider_StopReasonMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		finishReason string
		want         goagent.StopReason
	}{
		{"stop", goagent.StopReasonEndTurn},
		{"length", goagent.StopReasonMaxTokens},
		{"tool_calls", goagent.StopReasonToolUse},
		{"unknown_reason", goagent.StopReasonEndTurn},
	}

	for _, tc := range cases {
		t.Run(tc.finishReason, func(t *testing.T) {
			t.Parallel()

			body := `{"choices":[{"message":{"role":"assistant"},"finish_reason":"` + tc.finishReason + `"}]}`
			srv := fakeServer(t, body)
			p := newProvider(t, srv)

			resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
				Model:    "gpt-4o",
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

func TestProvider_Temperature_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	temp := 0.8
	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:       "gpt-4o",
		Messages:    []goagent.Message{goagent.UserMessage("hi")},
		Temperature: &temp,
	})

	got, _ := captured["temperature"].(float64)
	if got != 0.8 {
		t.Errorf("temperature = %v, want 0.8", got)
	}
}

func TestProvider_TemperatureNil_FieldOmitted(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})

	got, _ := captured["temperature"].(float64)
	if got != 0 {
		t.Errorf("temperature = %v, want 0 (absent) when not configured", got)
	}
}

func TestProvider_MaxTokens_SentInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:     "gpt-4o",
		Messages:  []goagent.Message{goagent.UserMessage("hi")},
		MaxTokens: 512,
	})

	maxTokens, _ := captured["max_tokens"].(float64)
	if maxTokens != 512 {
		t.Errorf("max_tokens = %v, want 512", maxTokens)
	}
}

func TestProvider_MaxTokensZero_Omitted(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:     "gpt-4o",
		Messages:  []goagent.Message{goagent.UserMessage("hi")},
		MaxTokens: 0,
	})

	// go-openai omits zero-value int fields.
	if _, present := captured["max_tokens"]; present {
		t.Errorf("max_tokens should be absent when zero, got %v", captured["max_tokens"])
	}
}

func TestProvider_Effort_SentAsReasoningEffort(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "o3-mini",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Effort:   "high",
	})

	got, _ := captured["reasoning_effort"].(string)
	if got != "high" {
		t.Errorf("reasoning_effort = %q, want %q", got, "high")
	}
}

func TestProvider_Effort_Empty_Omitted(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Effort:   "",
	})

	if _, present := captured["reasoning_effort"]; present {
		t.Errorf("reasoning_effort should be absent when Effort is empty, got %v", captured["reasoning_effort"])
	}
}

func TestProvider_Thinking_Ignored(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Thinking: &goagent.ThinkingConfig{Enabled: true, BudgetTokens: 4096},
	})

	// ThinkingConfig must not be forwarded — OpenAI has no equivalent field.
	if _, present := captured["thinking"]; present {
		t.Errorf("'thinking' field should not be sent to OpenAI, got %v", captured["thinking"])
	}
}

func TestProvider_ImageContent(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	imgData := []byte("fake-png")
	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "gpt-4o",
		Messages: []goagent.Message{
			{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.TextBlock("what is this?"),
					goagent.ImageBlock(imgData, "image/png"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	if len(messages) == 0 {
		t.Fatal("no messages in request body")
	}
	userMsg, _ := messages[0].(map[string]any)
	content, ok := userMsg["content"].([]any)
	if !ok {
		t.Fatalf("expected multiContent array, got %T", userMsg["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	part0, _ := content[0].(map[string]any)
	if part0["type"] != "text" {
		t.Errorf("part[0].type = %v, want text", part0["type"])
	}
	part1, _ := content[1].(map[string]any)
	if part1["type"] != "image_url" {
		t.Errorf("part[1].type = %v, want image_url", part1["type"])
	}
}

func TestProvider_DocumentContent_ReturnsUnsupportedError(t *testing.T) {
	t.Parallel()

	srv := fakeServer(t, stopResponse)
	p := newProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "gpt-4o",
		Messages: []goagent.Message{
			{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.DocumentBlock([]byte("pdf data"), "application/pdf", "test.pdf"),
					goagent.TextBlock("summarize this"),
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for document content, got nil")
	}
	if !errors.Is(err, goagent.ErrUnsupportedContent) {
		t.Errorf("expected ErrUnsupportedContent, got: %v", err)
	}
}

func TestProvider_ThinkingBlocksDiscardedInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "gpt-4o",
		Messages: []goagent.Message{
			goagent.UserMessage("question"),
			{
				Role: goagent.RoleAssistant,
				Content: []goagent.ContentBlock{
					goagent.ThinkingBlock("internal reasoning", ""),
					goagent.TextBlock("answer text"),
				},
			},
		},
	})

	messages, _ := captured["messages"].([]any)
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] != "assistant" {
			continue
		}
		// Single-text optimization: content is a plain string.
		content, _ := msg["content"].(string)
		if strings.Contains(content, "internal reasoning") {
			t.Errorf("thinking block content leaked into OpenAI request: %q", content)
		}
	}
}

func TestProvider_ToolDefinitionsSent(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := newProvider(t, srv)

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Tools: []goagent.ToolDefinition{
			{Name: "add", Description: "adds numbers", Parameters: map[string]any{"type": "object"}},
		},
	})

	tools, ok := captured["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools array in request body, got %v", captured["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	if fn["name"] != "add" {
		t.Errorf("tool name = %v, want add", fn["name"])
	}
}

func TestProvider_EmptyChoices_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := fakeServer(t, `{"choices":[]}`)
	p := newProvider(t, srv)

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
	if !strings.Contains(err.Error(), "empty choices") {
		t.Errorf("error = %q, want to contain 'empty choices'", err.Error())
	}
}

func TestProvider_UsageFields_Mapped(t *testing.T) {
	t.Parallel()

	body := `{
	  "choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
	  "usage":{"prompt_tokens":10,"completion_tokens":7}
	}`
	srv := fakeServer(t, body)
	p := newProvider(t, srv)

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 7 {
		t.Errorf("usage = %+v, want {InputTokens:10 OutputTokens:7}", resp.Usage)
	}
}

func TestNew_ReturnsNonNilProvider(t *testing.T) {
	t.Parallel()

	p := openai.New()
	if p == nil {
		t.Error("New() returned nil")
	}
}
