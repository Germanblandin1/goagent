package goagent_test

import (
	"context"
	"errors"
	"testing"

	goagent "github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

func TestRunStream_TextSimple(t *testing.T) {
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "Hello"},
		{Type: goagent.StreamEventText, Text: " world"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	mock := testutil.NewMockStreamingProvider(events)

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// v2: handler accumulates tokens as they arrive (not a single replay call).
	var received string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			received += ev.Text
		}
		return nil
	}

	result, err := agent.RunStream(context.Background(), "hi", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello world" {
		t.Errorf("result = %q, want %q", result, "Hello world")
	}
	if received != "Hello world" {
		t.Errorf("handler accumulated %q, want %q", received, "Hello world")
	}
}

func TestRunStream_FallbackNoStreaming(t *testing.T) {
	mock := testutil.NewMockProvider(goagent.CompletionResponse{
		Message:    goagent.AssistantMessage("full response"),
		StopReason: goagent.StopReasonEndTurn,
	})

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatal(err)
	}

	var received string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			received = ev.Text
		}
		return nil
	}

	result, err := agent.RunStream(context.Background(), "hi", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "full response" {
		t.Errorf("result = %q, want %q", result, "full response")
	}
	if received != "full response" {
		t.Errorf("handler received %q, want %q", received, "full response")
	}
}

func TestRunStream_HandlerErrorCancels(t *testing.T) {
	handlerErr := errors.New("handler cancelled")
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "token"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	mock := testutil.NewMockStreamingProvider(events)

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := func(_ goagent.StreamEvent) error { return handlerErr }

	_, err = agent.RunStream(context.Background(), "hi", handler)
	if !errors.Is(err, handlerErr) {
		t.Errorf("expected handlerErr, got %v", err)
	}
}

func TestRunStream_ToolCallNotDeliveredToHandler(t *testing.T) {
	// Stream contains only tool call events — handler must not be called.
	toolEvents := []goagent.StreamEvent{
		{Type: goagent.StreamEventToolStart, ToolName: "noop", ToolID: "t1"},
		{Type: goagent.StreamEventToolDelta, ToolID: "t1", InputDelta: `{}`},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonToolUse},
	}
	// Second iteration: final text response.
	finalEvents := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "done"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}

	noop := testutil.NewMockTool("noop", "no-op tool", "ok")
	prov := newCyclingStreamProvider([][]goagent.StreamEvent{toolEvents, finalEvents})

	agent, err := goagent.New(
		goagent.WithProvider(prov),
		goagent.WithModel("test-model"),
		goagent.WithTool(noop),
	)
	if err != nil {
		t.Fatal(err)
	}

	handlerCalled := false
	var handlerText string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			handlerCalled = true
			handlerText = ev.Text
		}
		return nil
	}

	result, err := agent.RunStream(context.Background(), "go", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}
	if !handlerCalled {
		t.Error("handler was never called for the final text response")
	}
	if handlerText != "done" {
		t.Errorf("handler text = %q, want %q", handlerText, "done")
	}
}

func TestRunStream_NilHandler(t *testing.T) {
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "hello"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	mock := testutil.NewMockStreamingProvider(events)

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := agent.RunStream(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %q, want %q", result, "hello")
	}
}

func TestRunStream_OnStreamTokenHook(t *testing.T) {
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "tok1"},
		{Type: goagent.StreamEventText, Text: "tok2"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	mock := testutil.NewMockStreamingProvider(events)

	var tokens []string
	hooks := goagent.Hooks{
		OnStreamToken: func(_ context.Context, token string) {
			tokens = append(tokens, token)
		},
	}

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.RunStream(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tokens) != 2 {
		t.Errorf("OnStreamToken called %d times, want 2", len(tokens))
	}
	if tokens[0] != "tok1" || tokens[1] != "tok2" {
		t.Errorf("tokens = %v, want [tok1 tok2]", tokens)
	}
}

func TestRunStream_V2_TokenByToken(t *testing.T) {
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "Ho"},
		{Type: goagent.StreamEventText, Text: "la"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	mock := testutil.NewMockStreamingProvider(events)

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatal(err)
	}

	var calls []string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			calls = append(calls, ev.Text)
		}
		return nil
	}

	result, err := agent.RunStream(context.Background(), "hi", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hola" {
		t.Errorf("result = %q, want %q", result, "Hola")
	}
	// v2: handler must receive 2 separate calls, not 1 with "Hola".
	if len(calls) != 2 {
		t.Errorf("handler called %d times, want 2", len(calls))
	}
	if len(calls) == 2 && (calls[0] != "Ho" || calls[1] != "la") {
		t.Errorf("handler calls = %v, want [Ho la]", calls)
	}
}

func TestRunStream_V2_ThinkingTextVisible(t *testing.T) {
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "Voy a calcular"},
		{Type: goagent.StreamEventToolStart, ToolName: "calc", ToolID: "t1"},
		{Type: goagent.StreamEventToolDelta, ToolID: "t1", InputDelta: `{"x":1}`},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonToolUse},
	}
	// Second iteration: final text response.
	finalEvents := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "1"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}

	calc := testutil.NewMockTool("calc", "calculator", "1")
	prov := newCyclingStreamProvider([][]goagent.StreamEvent{events, finalEvents})

	agent, err := goagent.New(
		goagent.WithProvider(prov),
		goagent.WithModel("test-model"),
		goagent.WithTool(calc),
	)
	if err != nil {
		t.Fatal(err)
	}

	var handlerTexts []string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			handlerTexts = append(handlerTexts, ev.Text)
		}
		return nil
	}

	_, err = agent.RunStream(
		context.Background(), "calc", handler,
		goagent.WithShowThinkingText(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First call: thinking text "Voy a calcular" (hasTools=false at that point,
	// ToolStart arrives after). Second call: final "1".
	if len(handlerTexts) < 1 {
		t.Fatal("handler was never called")
	}
	if handlerTexts[0] != "Voy a calcular" {
		t.Errorf("first handler text = %q, want %q", handlerTexts[0], "Voy a calcular")
	}
}

func TestRunStream_V2_ThinkingTextSuppressed(t *testing.T) {
	// ToolStart arrives before any text — clean suppression case.
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventToolStart, ToolName: "calc", ToolID: "t1"},
		{Type: goagent.StreamEventToolDelta, ToolID: "t1", InputDelta: `{"x":1}`},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonToolUse},
	}
	finalEvents := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "2"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}

	calc := testutil.NewMockTool("calc", "calculator", "2")
	prov := newCyclingStreamProvider([][]goagent.StreamEvent{events, finalEvents})

	agent, err := goagent.New(
		goagent.WithProvider(prov),
		goagent.WithModel("test-model"),
		goagent.WithTool(calc),
	)
	if err != nil {
		t.Fatal(err)
	}

	var firstIterHandlerCalled bool
	callCount := 0
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			callCount++
			// Only the final "2" should reach handler; no thinking text.
			if ev.Text != "2" {
				firstIterHandlerCalled = true
			}
		}
		return nil
	}

	_, err = agent.RunStream(
		context.Background(), "calc", handler,
		goagent.WithShowThinkingText(false),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if firstIterHandlerCalled {
		t.Error("handler received thinking text but showThinkingText=false")
	}
	if callCount != 1 {
		t.Errorf("handler called %d times, want 1 (only final response)", callCount)
	}
}

func TestRunStreamBlocks_Basic(t *testing.T) {
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "hello"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	mock := testutil.NewMockStreamingProvider(events)

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatal(err)
	}

	var received string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			received += ev.Text
		}
		return nil
	}

	result, err := agent.RunStreamBlocks(
		context.Background(),
		[]goagent.ContentBlock{goagent.TextBlock("hi")},
		handler,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %q, want %q", result, "hello")
	}
	if received != "hello" {
		t.Errorf("handler accumulated %q, want %q", received, "hello")
	}
}

func TestRunStreamBlocks_EmptyContent(t *testing.T) {
	mock := testutil.NewMockStreamingProvider(nil)
	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.RunStreamBlocks(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error for empty content, got nil")
	}
}

func TestRunStream_V2_ThinkingTextAfterToolStart_Visible(t *testing.T) {
	// ToolStart arrives before any text — text that follows is true thinking text
	// (hasTools=true when the StreamEventText is processed).
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventToolStart, ToolName: "calc", ToolID: "t1"},
		{Type: goagent.StreamEventText, Text: "let me calculate"},
		{Type: goagent.StreamEventToolDelta, ToolID: "t1", InputDelta: `{"x":1}`},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonToolUse},
	}
	finalEvents := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "result"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}

	var thinkingTokens []string
	hooks := goagent.Hooks{
		OnThinkingText: func(_ context.Context, token string) {
			thinkingTokens = append(thinkingTokens, token)
		},
	}

	calc := testutil.NewMockTool("calc", "calculator", "1")
	prov := newCyclingStreamProvider([][]goagent.StreamEvent{events, finalEvents})

	agent, err := goagent.New(
		goagent.WithProvider(prov),
		goagent.WithModel("test-model"),
		goagent.WithTool(calc),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	var handlerTexts []string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			handlerTexts = append(handlerTexts, ev.Text)
		}
		return nil
	}

	_, err = agent.RunStream(
		context.Background(), "calc", handler,
		goagent.WithShowThinkingText(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With showThinkingText=true, thinking text (after ToolStart) reaches handler.
	if len(handlerTexts) == 0 || handlerTexts[0] != "let me calculate" {
		t.Errorf("handler texts = %v, want first = %q", handlerTexts, "let me calculate")
	}
	// OnThinkingText hook must fire for each thinking token.
	if len(thinkingTokens) != 1 || thinkingTokens[0] != "let me calculate" {
		t.Errorf("OnThinkingText tokens = %v, want [let me calculate]", thinkingTokens)
	}
}

func TestRunStream_V2_ThinkingTextAfterToolStart_Suppressed(t *testing.T) {
	// ToolStart before text — suppression is guaranteed for text that arrives after ToolStart.
	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventToolStart, ToolName: "calc", ToolID: "t1"},
		{Type: goagent.StreamEventText, Text: "hidden"},
		{Type: goagent.StreamEventToolDelta, ToolID: "t1", InputDelta: `{"x":1}`},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonToolUse},
	}
	finalEvents := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "result"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}

	calc := testutil.NewMockTool("calc", "calculator", "1")
	prov := newCyclingStreamProvider([][]goagent.StreamEvent{events, finalEvents})

	agent, err := goagent.New(
		goagent.WithProvider(prov),
		goagent.WithModel("test-model"),
		goagent.WithTool(calc),
	)
	if err != nil {
		t.Fatal(err)
	}

	var handlerTexts []string
	handler := func(ev goagent.StreamEvent) error {
		if ev.Type == goagent.StreamEventText {
			handlerTexts = append(handlerTexts, ev.Text)
		}
		return nil
	}

	_, err = agent.RunStream(
		context.Background(), "calc", handler,
		goagent.WithShowThinkingText(false),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "hidden" must not reach handler — only the final "result".
	for _, text := range handlerTexts {
		if text == "hidden" {
			t.Error("suppressed thinking text reached handler with showThinkingText=false")
		}
	}
}

// cyclingStreamProvider cycles through multiple event sequences, returning
// a new stream on each CompleteStream call. Implements StreamingProvider.
type cyclingStreamProvider struct {
	streams [][]goagent.StreamEvent
	pos     int
}

func newCyclingStreamProvider(streams [][]goagent.StreamEvent) *cyclingStreamProvider {
	return &cyclingStreamProvider{streams: streams}
}

func (p *cyclingStreamProvider) Complete(_ context.Context, _ goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	return goagent.CompletionResponse{}, errors.New("cyclingStreamProvider: Complete not supported")
}

func (p *cyclingStreamProvider) CompleteStream(_ context.Context, _ goagent.CompletionRequest) (goagent.Stream, error) {
	if p.pos >= len(p.streams) {
		return nil, errors.New("cyclingStreamProvider: no more streams")
	}
	events := p.streams[p.pos]
	p.pos++
	return &testutil.MockStream{Events: events}, nil
}
