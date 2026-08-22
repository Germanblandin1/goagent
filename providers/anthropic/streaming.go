package anthropic

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/Germanblandin1/goagent"
)

// anthropicStream implements goagent.Stream over the Anthropic SDK stream.
// The inner SDK stream is stored via closures to avoid referencing the SDK's
// internal generic stream type directly.
type anthropicStream struct {
	// nextFn/currentFn/errFn/closeFn wrap the SDK stream methods.
	// currentFn calls AsAny() on the SDK event, returning the concrete variant.
	nextFn    func() bool
	currentFn func() any
	errFn     func() error
	closeFn   func() error

	current          goagent.StreamEvent
	toolAccumulators map[int]*toolAcc
}

type toolAcc struct {
	id    string
	name  string
	input strings.Builder
}

// Next advances to the next meaningful StreamEvent.
// Events that do not produce a StreamEvent (message_start, content_block_stop,
// message_stop) are consumed silently and the loop continues.
func (s *anthropicStream) Next(_ context.Context) bool {
	for s.nextFn() {
		variant := s.currentFn()
		switch ev := variant.(type) {

		case sdk.ContentBlockDeltaEvent:
			switch delta := ev.Delta.AsAny().(type) {
			case sdk.TextDelta:
				s.current = goagent.StreamEvent{
					Type: goagent.StreamEventText,
					Text: delta.Text,
				}
				return true

			case sdk.ThinkingDelta:
				// Extended-thinking reasoning token. Emit it as StreamEventThinking
				// so the agent routes it to OnThinkingText without mixing it into
				// the final response text — mirroring the Ollama provider.
				s.current = goagent.StreamEvent{
					Type: goagent.StreamEventThinking,
					Text: delta.Thinking,
				}
				return true

			case sdk.SignatureDelta:
				// Opaque signature that seals the thinking block. It carries no
				// reasoning text; emit it on the Signature field so RunStream can
				// rebuild a signed ContentThinking block and echo it back in a
				// later turn (required by the API when thinking + tools are used).
				s.current = goagent.StreamEvent{
					Type:      goagent.StreamEventThinking,
					Signature: delta.Signature,
				}
				return true

			case sdk.InputJSONDelta:
				if acc, ok := s.toolAccumulators[int(ev.Index)]; ok {
					acc.input.WriteString(delta.PartialJSON)
					s.current = goagent.StreamEvent{
						Type:       goagent.StreamEventToolDelta,
						ToolID:     acc.id,
						InputDelta: acc.input.String(),
					}
					return true
				}
			}

		case sdk.ContentBlockStartEvent:
			switch block := ev.ContentBlock.AsAny().(type) {
			case sdk.ToolUseBlock:
				if s.toolAccumulators == nil {
					s.toolAccumulators = make(map[int]*toolAcc)
				}
				s.toolAccumulators[int(ev.Index)] = &toolAcc{
					id:   block.ID,
					name: block.Name,
				}
				s.current = goagent.StreamEvent{
					Type:     goagent.StreamEventToolStart,
					ToolID:   block.ID,
					ToolName: block.Name,
				}
				return true
			}

		case sdk.MessageDeltaEvent:
			s.current = goagent.StreamEvent{
				Type:       goagent.StreamEventDone,
				StopReason: toStopReason(ev.Delta.StopReason),
				Usage: goagent.Usage{
					// MessageDelta only carries output tokens;
					// input tokens live in MessageStartEvent (not captured here).
					OutputTokens: int(ev.Usage.OutputTokens),
				},
			}
			return true
		}
	}
	return false
}

func (s *anthropicStream) Event() goagent.StreamEvent { return s.current }
func (s *anthropicStream) Err() error                 { return s.errFn() }
func (s *anthropicStream) Close() error               { return s.closeFn() }

// CompleteStream implements goagent.StreamingProvider.
func (p *Provider) CompleteStream(ctx context.Context, req goagent.CompletionRequest) (goagent.Stream, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("anthropic: model not set; use goagent.WithModel")
	}
	params, err := p.buildMessageParams(req)
	if err != nil {
		return nil, err
	}
	inner := p.client.cl.Messages.NewStreaming(ctx, params)
	return &anthropicStream{
		nextFn:    func() bool { return inner.Next() },
		currentFn: func() any { return inner.Current().AsAny() },
		errFn:     func() error { return classifyError(inner.Err()) },
		closeFn:   func() error { return inner.Close() },
	}, nil
}
