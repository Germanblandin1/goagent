package ollama_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// fakeStreamServer serves NDJSON lines on POST /api/chat.
func fakeStreamServer(t *testing.T, ndjson string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(ndjson))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOllamaCompleteStream_TextTokens(t *testing.T) {
	t.Parallel()

	ndjson := "" +
		`{"message":{"content":"Hello"},"done":false}` + "\n" +
		`{"message":{"content":" world"},"done":false}` + "\n" +
		`{"message":{"content":""},"done":true,"done_reason":"stop","eval_count":3,"prompt_eval_count":5}` + "\n"

	srv := fakeStreamServer(t, ndjson)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model: "llama3",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var events []goagent.StreamEvent
	for stream.Next(context.Background()) {
		events = append(events, stream.Event())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Type != goagent.StreamEventText || events[0].Text != "Hello" {
		t.Errorf("event[0] = %+v, want Text=Hello", events[0])
	}
	if events[1].Type != goagent.StreamEventText || events[1].Text != " world" {
		t.Errorf("event[1] = %+v, want Text=' world'", events[1])
	}
	if events[2].Type != goagent.StreamEventDone {
		t.Errorf("event[2].Type = %v, want StreamEventDone", events[2].Type)
	}
	if events[2].StopReason != goagent.StopReasonEndTurn {
		t.Errorf("event[2].StopReason = %v, want EndTurn", events[2].StopReason)
	}
	if events[2].Usage.InputTokens != 5 || events[2].Usage.OutputTokens != 3 {
		t.Errorf("event[2].Usage = %+v, want {5, 3}", events[2].Usage)
	}
}

func TestOllamaCompleteStream_ToolCalls(t *testing.T) {
	t.Parallel()

	// Done chunk contains tool calls — they must be emitted as ToolStart+ToolDelta
	// events before the Done event.
	ndjson := `{"message":{"content":"","tool_calls":[{"function":{"name":"calc","arguments":{"x":1,"y":2}}}]},"done":true,"done_reason":"stop","eval_count":5,"prompt_eval_count":3}` + "\n"

	srv := fakeStreamServer(t, ndjson)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{
		Model: "llama3",
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var events []goagent.StreamEvent
	for stream.Next(context.Background()) {
		events = append(events, stream.Event())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	// Expect: ToolStart, ToolDelta, Done
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (ToolStart, ToolDelta, Done)", len(events))
	}

	if events[0].Type != goagent.StreamEventToolStart {
		t.Errorf("event[0].Type = %v, want ToolStart", events[0].Type)
	}
	if events[0].ToolName != "calc" {
		t.Errorf("event[0].ToolName = %q, want %q", events[0].ToolName, "calc")
	}
	if events[0].ToolID == "" {
		t.Error("event[0].ToolID is empty")
	}

	if events[1].Type != goagent.StreamEventToolDelta {
		t.Errorf("event[1].Type = %v, want ToolDelta", events[1].Type)
	}
	if events[1].ToolID != events[0].ToolID {
		t.Errorf("event[1].ToolID = %q, want %q", events[1].ToolID, events[0].ToolID)
	}
	if events[1].InputDelta == "" {
		t.Error("event[1].InputDelta is empty, want JSON args")
	}

	if events[2].Type != goagent.StreamEventDone {
		t.Errorf("event[2].Type = %v, want Done", events[2].Type)
	}
	if events[2].StopReason != goagent.StopReasonToolUse {
		t.Errorf("event[2].StopReason = %v, want ToolUse", events[2].StopReason)
	}
}

func TestOllamaCompleteStream_MultipleToolCalls(t *testing.T) {
	t.Parallel()

	ndjson := `{"message":{"content":"","tool_calls":[{"function":{"name":"a","arguments":{"k":"v"}}},{"function":{"name":"b","arguments":{}}}]},"done":true,"done_reason":"stop"}` + "\n"

	srv := fakeStreamServer(t, ndjson)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{Model: "llama3"})
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
	if starts != 2 || deltas != 2 {
		t.Errorf("starts=%d deltas=%d, want 2/2", starts, deltas)
	}
}

func TestOllamaCompleteStream_StopReasonLength(t *testing.T) {
	t.Parallel()

	ndjson := `{"message":{"content":"truncated"},"done":true,"done_reason":"length","eval_count":1,"prompt_eval_count":1}` + "\n"

	srv := fakeStreamServer(t, ndjson)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{Model: "llama3"})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var doneEvent goagent.StreamEvent
	for stream.Next(context.Background()) {
		if stream.Event().Type == goagent.StreamEventDone {
			doneEvent = stream.Event()
		}
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if doneEvent.StopReason != goagent.StopReasonMaxTokens {
		t.Errorf("StopReason = %v, want MaxTokens", doneEvent.StopReason)
	}
}

func TestOllamaCompleteStream_InvalidJSON(t *testing.T) {
	t.Parallel()

	ndjson := `{invalid json` + "\n"

	srv := fakeStreamServer(t, ndjson)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	stream, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{Model: "llama3"})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	for stream.Next(context.Background()) {
	}
	if stream.Err() == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestOllamaCompleteStream_EmptyModel(t *testing.T) {
	t.Parallel()

	p := ollama.New()
	_, err := p.CompleteStream(context.Background(), goagent.CompletionRequest{})
	if err == nil {
		t.Error("expected error for empty model, got nil")
	}
}

func TestOllamaCompleteStream_WithMessagesAndSystemPrompt(t *testing.T) {
	t.Parallel()

	ndjson := `{"message":{"content":"ok"},"done":false}` + "\n" +
		`{"message":{"content":""},"done":true,"done_reason":"stop","eval_count":1,"prompt_eval_count":2}` + "\n"

	srv := fakeStreamServer(t, ndjson)
	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	req := goagent.CompletionRequest{
		Model:        "llama3",
		SystemPrompt: "You are helpful",
		Messages: []goagent.Message{
			goagent.UserMessage("hello"),
			{
				Role:       goagent.RoleAssistant,
				Content:    []goagent.ContentBlock{goagent.TextBlock("hi")},
				ToolCalls:  []goagent.ToolCall{{ID: "t1", Name: "calc", Arguments: map[string]any{"x": 1}}},
			},
		},
	}
	stream, err := p.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	defer stream.Close()

	var text string
	for stream.Next(context.Background()) {
		if ev := stream.Event(); ev.Type == goagent.StreamEventText {
			text += ev.Text
		}
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if text != "ok" {
		t.Errorf("text = %q, want %q", text, "ok")
	}
}
