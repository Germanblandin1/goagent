package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// fakeServer starts an httptest server that responds to POST /v1/chat/completions
// with the provided raw JSON body.
func fakeServer(t *testing.T, responseJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
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
  "choices":[{"message":{"role":"assistant","content":"hello from ollama"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":5,"completion_tokens":3}
}`

func TestProvider_SimpleResponse(t *testing.T) {
	t.Parallel()

	srv := fakeServer(t, stopResponse)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "llama3",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.TextContent() != "hello from ollama" {
		t.Errorf("content = %q, want %q", resp.Message.TextContent(), "hello from ollama")
	}
	if resp.StopReason != goagent.StopReasonEndTurn {
		t.Errorf("stop reason = %v, want EndTurn", resp.StopReason)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want {5 3}", resp.Usage)
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
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "llama3",
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
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:        "llama3",
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
			p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

			resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
				Model:    "llama3",
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

func TestProvider_ImageContent(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	imgData := []byte("fake-png")
	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "llava",
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
	// The user message should have multiContent with text + image_url parts.
	userMsg, _ := messages[0].(map[string]any)
	content, ok := userMsg["content"].([]any)
	if !ok {
		t.Fatalf("expected multiContent array, got %T", userMsg["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}

	// First part should be text.
	part0, _ := content[0].(map[string]any)
	if part0["type"] != "text" {
		t.Errorf("part[0].type = %v, want text", part0["type"])
	}

	// Second part should be image_url with a data URI.
	part1, _ := content[1].(map[string]any)
	if part1["type"] != "image_url" {
		t.Errorf("part[1].type = %v, want image_url", part1["type"])
	}
}

func TestProvider_EmptyModel(t *testing.T) {
	t.Parallel()

	srv := fakeServer(t, stopResponse)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

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


func TestProvider_DocumentContent_ReturnsUnsupportedError(t *testing.T) {
	t.Parallel()

	srv := fakeServer(t, stopResponse)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "llama3",
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

// ── Extended Thinking (Ollama) ───────────────────────────────────────────────

func TestProvider_ParseThinkingFromText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		responseContent string
		wantThinking    string // empty = no thinking block expected
		wantText        string
	}{
		{
			name:            "think tags with reasoning and answer",
			responseContent: "<think>I need to add</think>The answer is 4",
			wantThinking:    "I need to add",
			wantText:        "The answer is 4",
		},
		{
			name:            "no think tags — plain text",
			responseContent: "just a plain answer",
			wantThinking:    "",
			wantText:        "just a plain answer",
		},
		{
			name:            "unclosed think tag — treat all as text",
			responseContent: "<think>unclosed reasoning",
			wantThinking:    "",
			wantText:        "<think>unclosed reasoning",
		},
		{
			name:            "empty think tags — no thinking block, text preserved",
			responseContent: "<think></think>the answer",
			wantThinking:    "",
			wantText:        "the answer",
		},
		{
			name:            "thinking only, no text after",
			responseContent: "<think>pure reasoning</think>",
			wantThinking:    "pure reasoning",
			wantText:        "",
		},
		{
			name:            "whitespace trimmed in thinking and text",
			responseContent: "<think>  spaced reasoning  </think>  spaced answer  ",
			wantThinking:    "spaced reasoning",
			wantText:        "spaced answer",
		},
		{
			name:            "empty response",
			responseContent: "",
			wantThinking:    "",
			wantText:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := `{"choices":[{"message":{"role":"assistant","content":` +
				jsonString(tt.responseContent) +
				`},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`

			srv := fakeServer(t, body)
			p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

			resp, err := p.Complete(context.Background(), goagent.CompletionRequest{
				Model:    "qwq",
				Messages: []goagent.Message{goagent.UserMessage("question")},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantThinking != "" {
				if !resp.Message.HasContentType(goagent.ContentThinking) {
					t.Fatal("expected thinking block, got none")
				}
				var found string
				for _, b := range resp.Message.Content {
					if b.Type == goagent.ContentThinking && b.Thinking != nil {
						found = b.Thinking.Thinking
					}
				}
				if found != tt.wantThinking {
					t.Errorf("ThinkingBlock.Thinking = %q, want %q", found, tt.wantThinking)
				}
			} else {
				if resp.Message.HasContentType(goagent.ContentThinking) {
					t.Error("unexpected thinking block in response")
				}
			}

			if tt.wantText != "" {
				if resp.Message.TextContent() != tt.wantText {
					t.Errorf("TextContent() = %q, want %q", resp.Message.TextContent(), tt.wantText)
				}
			}
		})
	}
}

func TestProvider_ThinkingBlocksDiscardedInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model: "qwq",
		Messages: []goagent.Message{
			goagent.UserMessage("question"),
			{
				Role: goagent.RoleAssistant,
				Content: []goagent.ContentBlock{
					goagent.ThinkingBlock("internal reasoning", ""), // should be discarded
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
		// In the Ollama provider, content is a plain string (single text block optimization).
		// Thinking blocks must have been discarded, leaving only the text.
		content, _ := msg["content"].(string)
		if strings.Contains(content, "internal reasoning") {
			t.Errorf("thinking block content leaked into Ollama request: %q", content)
		}
	}
}

func TestProvider_ThinkingAndEffortIgnored(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingServer(t, stopResponse, &captured)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	_, _ = p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "qwq",
		Messages: []goagent.Message{goagent.UserMessage("hi")},
		Thinking: &goagent.ThinkingConfig{Enabled: true, BudgetTokens: 4096},
		Effort:   "high",
	})

	if _, present := captured["thinking"]; present {
		t.Errorf("'thinking' field should not be sent to Ollama, but was present: %v", captured["thinking"])
	}
	if _, present := captured["output_config"]; present {
		t.Errorf("'output_config' field should not be sent to Ollama, but was present: %v", captured["output_config"])
	}
}

// jsonString encodes s as a JSON string literal, e.g. `"hello"`.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
