package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	openaiSDK "github.com/sashabaranov/go-openai"

	"github.com/Germanblandin1/goagent"
)

// openaiStreamChunk is a streaming SSE chunk decoded directly from the raw
// bytes (via ChatCompletionStream.RecvRaw) instead of the SDK's typed
// ChatCompletionStreamResponse. Decoding it ourselves lets us read reasoning
// fields the SDK's delta struct omits: OpenAI-compatible upstreams surface
// reasoning under two different conventions, and only one is modelled by the SDK.
//
//   - reasoning_content — DeepSeek (direct). The SDK also exposes this as
//     ChatCompletionStreamChoiceDelta.ReasoningContent.
//   - reasoning (with optional reasoning_details) — OpenRouter. The SDK delta
//     has no such field, so Recv() would silently drop it. The text lives in the
//     plain reasoning string; reasoning_details is a redundant structured mirror
//     we do not need.
//
// The official OpenAI API sends neither field, so its chunks produce no thinking
// events and behaviour there is unchanged.
type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string               `json:"content"`
			ReasoningContent string               `json:"reasoning_content"`
			Reasoning        string               `json:"reasoning"`
			ToolCalls        []openaiSDK.ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason openaiSDK.FinishReason `json:"finish_reason"`
		Index        int                    `json:"index"`
	} `json:"choices"`
	// Usage is present only on the final chunk when the request sets
	// stream_options.include_usage; it is null on every other chunk.
	Usage *openaiSDK.Usage `json:"usage"`
}

// openaiToolAcc accumulates a single tool call across multiple stream deltas.
// OpenAI sends ID and Name only in the first delta for a given index; subsequent
// deltas carry additional Arguments JSON fragments.
type openaiToolAcc struct {
	id      string
	name    string
	input   strings.Builder
	started bool // true after StreamEventToolStart has been emitted
}

// openaiStream implements goagent.Stream over go-openai's ChatCompletionStream.
type openaiStream struct {
	inner         *openaiSDK.ChatCompletionStream
	current       goagent.StreamEvent
	toolAccs      map[int]*openaiToolAcc
	pendingEvents []goagent.StreamEvent
	// stopReason and usage are captured as their chunks arrive and folded into
	// the single StreamEventDone emitted at EOF. The finish_reason chunk arrives
	// before the usage-only chunk, so Done must be deferred until both are seen.
	stopReason goagent.StopReason
	usage      goagent.Usage
	done       bool
	err        error
}

// Next advances to the next StreamEvent. Returns false when the stream is
// exhausted or an error occurred.
func (s *openaiStream) Next(_ context.Context) bool {
	if s.done || s.err != nil {
		return false
	}

	// Drain any events queued from a previous chunk before reading a new one.
	if len(s.pendingEvents) > 0 {
		s.current = s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		if s.current.Type == goagent.StreamEventDone {
			s.done = true
		}
		return true
	}

	for {
		raw, err := s.inner.RecvRaw()
		if err == io.EOF {
			if !s.done {
				// finish_reason and usage arrived in earlier chunks; emit the
				// single terminal Done now carrying both.
				s.current = goagent.StreamEvent{
					Type:       goagent.StreamEventDone,
					StopReason: s.stopReason,
					Usage:      s.usage,
				}
				s.done = true
				return true
			}
			return false
		}
		if err != nil {
			s.err = fmt.Errorf("openai: stream recv: %w", classifyError(err))
			return false
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal(raw, &chunk); err != nil {
			s.err = fmt.Errorf("openai: decoding stream chunk: %w", err)
			return false
		}

		// Usage arrives on a dedicated final chunk (include_usage). Capture it
		// for the deferred Done event; align field mapping with Complete.
		if chunk.Usage != nil {
			s.usage = goagent.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}

		if len(chunk.Choices) == 0 {
			// Usage-only or keep-alive chunk with no choice payload. Done is
			// deferred to EOF so it can carry the usage captured above.
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Handle tool call deltas.
		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			if s.toolAccs == nil {
				s.toolAccs = make(map[int]*openaiToolAcc)
			}
			acc, exists := s.toolAccs[idx]
			if !exists {
				acc = &openaiToolAcc{}
				s.toolAccs[idx] = acc
			}
			// ID and Name only appear in the first delta for this index.
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.input.WriteString(tc.Function.Arguments)
			}

			if !acc.started && acc.id != "" && acc.name != "" {
				acc.started = true
				s.current = goagent.StreamEvent{
					Type:     goagent.StreamEventToolStart,
					ToolID:   acc.id,
					ToolName: acc.name,
				}
				// Queue a ToolDelta if we already have some input JSON.
				if acc.input.Len() > 0 {
					s.pendingEvents = append(s.pendingEvents, goagent.StreamEvent{
						Type:       goagent.StreamEventToolDelta,
						ToolID:     acc.id,
						InputDelta: acc.input.String(),
					})
				}
				return true
			} else if acc.started && tc.Function.Arguments != "" {
				s.current = goagent.StreamEvent{
					Type:       goagent.StreamEventToolDelta,
					ToolID:     acc.id,
					InputDelta: acc.input.String(),
				}
				return true
			}
		}

		// Handle reasoning tokens. DeepSeek uses reasoning_content, OpenRouter
		// uses reasoning; upstreams send one or the other, never both. Emit them
		// as StreamEventThinking (like Ollama's thinking field) so the agent
		// routes them to OnThinkingText without mixing them into the final text.
		// Reasoning tokens arrive before content. Checked before Content: a chunk
		// carries reasoning or content, not both.
		reasoning := delta.ReasoningContent
		if reasoning == "" {
			reasoning = delta.Reasoning
		}
		if reasoning != "" {
			s.current = goagent.StreamEvent{
				Type: goagent.StreamEventThinking,
				Text: reasoning,
			}
			return true
		}

		// Handle text delta.
		if delta.Content != "" {
			s.current = goagent.StreamEvent{
				Type: goagent.StreamEventText,
				Text: delta.Content,
			}
			return true
		}

		// Handle finish reason — FinishReasonNull ("null") must be ignored.
		if choice.FinishReason != "" && choice.FinishReason != openaiSDK.FinishReasonNull {
			// Capture the stop reason but defer Done to EOF: usage arrives in a
			// separate usage-only chunk after this one, and Done must carry it.
			s.stopReason = toStopReason(choice.FinishReason)
			continue
		}
	}
}

func (s *openaiStream) Event() goagent.StreamEvent { return s.current }
func (s *openaiStream) Err() error                 { return s.err }
func (s *openaiStream) Close() error               { return s.inner.Close() }

// CompleteStream implements goagent.StreamingProvider using the OpenAI Chat
// Completions API with streaming enabled.
//
// Text tokens are delivered as StreamEventText events as they arrive.
// Reasoning tokens, when the upstream provides them (OpenAI-compatible APIs
// such as DeepSeek via reasoning_content or OpenRouter via reasoning), are
// delivered as StreamEventThinking events, keeping them separate from the final
// text. The official OpenAI API does not surface reasoning, so no thinking
// events are emitted there. Tool calls are translated to StreamEventToolStart +
// StreamEventToolDelta events before StreamEventDone, so the agent loop handles
// them the same way as other providers. The request sets
// stream_options.include_usage, so the terminal StreamEventDone carries token
// usage.
func (p *Provider) CompleteStream(ctx context.Context, req goagent.CompletionRequest) (goagent.Stream, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("openai: model not set; use goagent.WithModel")
	}

	messages, err := toOpenAIMessages(req)
	if err != nil {
		return nil, err
	}

	chatReq := openaiSDK.ChatCompletionRequest{
		Model:     req.Model,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
		StreamOptions: &openaiSDK.StreamOptions{
			IncludeUsage: true,
		},
	}

	if req.Temperature != nil {
		chatReq.Temperature = float32(*req.Temperature)
	}
	if req.Effort != "" {
		chatReq.ReasoningEffort = req.Effort
	}
	if len(req.Tools) > 0 {
		chatReq.Tools = toOpenAITools(req.Tools)
		chatReq.ToolChoice = "auto"
	}

	inner, err := p.client.CreateChatCompletionStream(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("openai: creating stream: %w", classifyError(err))
	}

	// stopReason defaults to end-turn: if a stream ends without a finish_reason
	// chunk, Done still reports a sensible terminal reason.
	return &openaiStream{inner: inner, stopReason: goagent.StopReasonEndTurn}, nil
}
