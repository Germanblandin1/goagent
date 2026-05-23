package openai

import (
	"context"
	"fmt"
	"io"
	"strings"

	openaiSDK "github.com/sashabaranov/go-openai"

	"github.com/Germanblandin1/goagent"
)

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
	done          bool
	err           error
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
		chunk, err := s.inner.Recv()
		if err == io.EOF {
			if !s.done {
				s.current = goagent.StreamEvent{
					Type:       goagent.StreamEventDone,
					StopReason: goagent.StopReasonEndTurn,
				}
				s.done = true
				return true
			}
			return false
		}
		if err != nil {
			s.err = fmt.Errorf("openai: stream recv: %w", err)
			return false
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk — StreamEventDone was already emitted from the
			// finish_reason chunk, so we skip this.
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
			// Usage is not reliably available here; it comes in a separate
			// usage-only chunk after this one. StreamEventDone.Usage will be zero.
			s.current = goagent.StreamEvent{
				Type:       goagent.StreamEventDone,
				StopReason: toStopReason(choice.FinishReason),
			}
			s.done = true
			return true
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
// Tool calls are translated to StreamEventToolStart + StreamEventToolDelta
// events before StreamEventDone, so the agent loop handles them the same way
// as other providers.
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
		return nil, fmt.Errorf("openai: creating stream: %w", err)
	}

	return &openaiStream{inner: inner}, nil
}
