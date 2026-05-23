package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/openai"
)

// sseServer starts an httptest server that serves SSE events on POST /chat/completions.
// Each event is a JSON string (without the "data: " prefix — the helper adds it).
// The server sends a final "data: [DONE]\n\n" after all events.
func sseServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// capturingSSEServer serves SSE events and captures the decoded request body into out.
func capturingSSEServer(t *testing.T, events []string, out *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(out)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newStreamProvider(t *testing.T, srv *httptest.Server) *openai.Provider {
	t.Helper()
	return openai.New(openai.WithBaseURL(srv.URL), openai.WithAPIKey("test-key"))
}

// sseChunk builds a minimal SSE JSON payload for a text delta.
func textChunk(text string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q},"finish_reason":null,"index":0}]}`, text)
}

// finishChunk builds a minimal SSE JSON payload with a finish_reason.
func finishChunk(reason string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{},"finish_reason":%q,"index":0}]}`, reason)
}

func TestCompleteStream_TextTokens(t *testing.T) {
	t.Parallel()

	events := []string{
		textChunk("Hello"),
		textChunk(" world"),
		finishChunk("stop"),
	}
	srv := sseServer(t, events)
	p := newStreamProvider(t, srv)

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var collected []goagent.StreamEvent
	for stream.Next(context.Background()) {
		collected = append(collected, stream.Event())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	var textEvents []goagent.StreamEvent
	var doneEvent *goagent.StreamEvent
	for i := range collected {
		switch collected[i].Type {
		case goagent.StreamEventText:
			textEvents = append(textEvents, collected[i])
		case goagent.StreamEventDone:
			doneEvent = &collected[i]
		}
	}

	if len(textEvents) != 2 {
		t.Fatalf("got %d text events, want 2", len(textEvents))
	}
	if textEvents[0].Text != "Hello" {
		t.Errorf("text[0] = %q, want Hello", textEvents[0].Text)
	}
	if textEvents[1].Text != " world" {
		t.Errorf("text[1] = %q, want ' world'", textEvents[1].Text)
	}
	if doneEvent == nil {
		t.Fatal("no Done event received")
	}
	if doneEvent.StopReason != goagent.StopReasonEndTurn {
		t.Errorf("Done.StopReason = %v, want EndTurn", doneEvent.StopReason)
	}
}

func TestCompleteStream_StopReasonLength(t *testing.T) {
	t.Parallel()

	events := []string{
		textChunk("truncated"),
		finishChunk("length"),
	}
	srv := sseServer(t, events)
	p := newStreamProvider(t, srv)

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var doneEvent *goagent.StreamEvent
	for stream.Next(context.Background()) {
		ev := stream.Event()
		if ev.Type == goagent.StreamEventDone {
			doneEvent = &ev
		}
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if doneEvent == nil {
		t.Fatal("no Done event received")
	}
	if doneEvent.StopReason != goagent.StopReasonMaxTokens {
		t.Errorf("StopReason = %v, want MaxTokens", doneEvent.StopReason)
	}
}

func TestCompleteStream_EmptyModel_ReturnsError(t *testing.T) {
	t.Parallel()

	p := openai.New()
	_, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{})
	if err == nil {
		t.Error("expected error for empty model, got nil")
	}
}

func TestCompleteStream_ToolStartAndDelta(t *testing.T) {
	t.Parallel()

	// OpenAI tool streaming: first delta has id+name, subsequent deltas have arguments.
	idx := 0
	events := []string{
		// First delta: ID and Name, no arguments yet.
		fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"id":"call_abc","type":"function","function":{"name":"calc","arguments":""}}]},"finish_reason":null,"index":0}]}`, idx),
		// Second delta: first argument fragment.
		fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"function":{"arguments":"{\"a\":"}}]},"finish_reason":null,"index":0}]}`, idx),
		// Third delta: second argument fragment.
		fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"function":{"arguments":"2}"}}]},"finish_reason":null,"index":0}]}`, idx),
		finishChunk("tool_calls"),
	}
	srv := sseServer(t, events)
	p := newStreamProvider(t, srv)

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var collected []goagent.StreamEvent
	for stream.Next(context.Background()) {
		collected = append(collected, stream.Event())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	var toolStartEvent *goagent.StreamEvent
	var toolDeltaEvents []goagent.StreamEvent
	var doneEvent *goagent.StreamEvent
	for i := range collected {
		switch collected[i].Type {
		case goagent.StreamEventToolStart:
			toolStartEvent = &collected[i]
		case goagent.StreamEventToolDelta:
			toolDeltaEvents = append(toolDeltaEvents, collected[i])
		case goagent.StreamEventDone:
			doneEvent = &collected[i]
		}
	}

	if toolStartEvent == nil {
		t.Fatal("no ToolStart event received")
	}
	if toolStartEvent.ToolID != "call_abc" {
		t.Errorf("ToolStart.ToolID = %q, want call_abc", toolStartEvent.ToolID)
	}
	if toolStartEvent.ToolName != "calc" {
		t.Errorf("ToolStart.ToolName = %q, want calc", toolStartEvent.ToolName)
	}
	if len(toolDeltaEvents) == 0 {
		t.Fatal("no ToolDelta events received")
	}
	if doneEvent == nil {
		t.Fatal("no Done event received")
	}
	if doneEvent.StopReason != goagent.StopReasonToolUse {
		t.Errorf("Done.StopReason = %v, want ToolUse", doneEvent.StopReason)
	}
}

func TestCompleteStream_MultipleToolCalls(t *testing.T) {
	t.Parallel()

	// Two tool calls with indices 0 and 1, each gets ToolStart.
	events := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"tool_a","arguments":""}}]},"finish_reason":null,"index":0}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_1","type":"function","function":{"name":"tool_b","arguments":""}}]},"finish_reason":null,"index":0}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":null,"index":0}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{}"}}]},"finish_reason":null,"index":0}]}`,
		finishChunk("tool_calls"),
	}
	srv := sseServer(t, events)
	p := newStreamProvider(t, srv)

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var starts, deltas int
	for stream.Next(context.Background()) {
		switch stream.Event().Type {
		case goagent.StreamEventToolStart:
			starts++
		case goagent.StreamEventToolDelta:
			deltas++
		}
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if starts != 2 {
		t.Errorf("ToolStart events = %d, want 2", starts)
	}
	if deltas != 2 {
		t.Errorf("ToolDelta events = %d, want 2", deltas)
	}
}

func TestCompleteStream_Effort_SentAsReasoningEffort(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingSSEServer(t, []string{finishChunk("stop")}, &captured)
	p := newStreamProvider(t, srv)

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model:  "o3-mini",
		Effort: "medium",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()
	for stream.Next(context.Background()) {
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}

	got, _ := captured["reasoning_effort"].(string)
	if got != "medium" {
		t.Errorf("reasoning_effort = %q, want %q", got, "medium")
	}
}

func TestCompleteStream_WithSystemPrompt(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := capturingSSEServer(t, []string{textChunk("ok"), finishChunk("stop")}, &captured)
	p := newStreamProvider(t, srv)

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model:        "gpt-4o",
		SystemPrompt: "be helpful",
		Messages:     []goagent.Message{goagent.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()
	for stream.Next(context.Background()) {
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}

	messages, _ := captured["messages"].([]any)
	if len(messages) == 0 {
		t.Fatal("no messages in request body")
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" {
		t.Errorf("first message role = %v, want system", first["role"])
	}
}

func TestCompleteStream_ContextCancelled(t *testing.T) {
	t.Parallel()

	// Server that blocks indefinitely.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	p := openai.New(openai.WithBaseURL(srv.URL), openai.WithAPIKey("test-key"))
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := p.CompleteStream(ctx, goagent.CompletionRequest{
		Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	cancel()
	for stream.Next(ctx) {
	}
	if stream.Err() == nil {
		t.Error("expected error after context cancellation, got nil")
	}
}
