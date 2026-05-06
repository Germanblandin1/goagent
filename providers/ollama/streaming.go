package ollama

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Germanblandin1/goagent"
)

// ollamaStreamRequest is the body sent to Ollama's native /api/chat endpoint
// with streaming enabled.
type ollamaStreamRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaNativeMsg   `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []ollamaNativeTool  `json:"tools,omitempty"`
}

type ollamaNativeMsg struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []ollamaNativeCall `json:"tool_calls,omitempty"`
}

type ollamaNativeTool struct {
	Type     string                `json:"type"`
	Function ollamaNativeFunction  `json:"function"`
}

type ollamaNativeFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ollamaNativeCall struct {
	Function ollamaNativeCallFunc `json:"function"`
}

type ollamaNativeCallFunc struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ollamaStreamChunk is a single JSON line in the streaming response.
type ollamaStreamChunk struct {
	Message struct {
		Content   string             `json:"content"`
		ToolCalls []ollamaNativeCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason,omitempty"`
	EvalCount       int    `json:"eval_count"`
	PromptEvalCount int    `json:"prompt_eval_count"`
}

// ollamaStream implements goagent.Stream over Ollama's native streaming API.
type ollamaStream struct {
	resp    *http.Response
	scanner *bufio.Scanner
	// pending holds events queued from a done chunk (tool events + done) so
	// that Next can return them one at a time without buffering the whole stream.
	pending []goagent.StreamEvent
	current goagent.StreamEvent
	err     error
	done    bool
}

func (s *ollamaStream) Next(_ context.Context) bool {
	if s.done || s.err != nil {
		return false
	}

	// Drain events queued from the previous chunk before reading the next one.
	if len(s.pending) > 0 {
		s.current = s.pending[0]
		s.pending = s.pending[1:]
		if s.current.Type == goagent.StreamEventDone {
			s.done = true
		}
		return true
	}

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunk ollamaStreamChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			s.err = fmt.Errorf("ollama: decoding stream chunk: %w", err)
			return false
		}
		if chunk.Done {
			// Emit one ToolStart + one ToolDelta per tool call, then Done.
			for i, tc := range chunk.Message.ToolCalls {
				toolID := fmt.Sprintf("ollama-tool-%d", i)
				args := tc.Function.Arguments
				if args == nil {
					args = map[string]any{}
				}
				argsJSON, _ := json.Marshal(args)
				s.pending = append(s.pending, goagent.StreamEvent{
					Type:     goagent.StreamEventToolStart,
					ToolName: tc.Function.Name,
					ToolID:   toolID,
				})
				s.pending = append(s.pending, goagent.StreamEvent{
					Type:       goagent.StreamEventToolDelta,
					ToolID:     toolID,
					InputDelta: string(argsJSON),
				})
			}
			s.pending = append(s.pending, goagent.StreamEvent{
				Type:       goagent.StreamEventDone,
				StopReason: ollamaStopReason(chunk.DoneReason, len(chunk.Message.ToolCalls) > 0),
				Usage: goagent.Usage{
					InputTokens:  chunk.PromptEvalCount,
					OutputTokens: chunk.EvalCount,
				},
			})
			s.current = s.pending[0]
			s.pending = s.pending[1:]
			if s.current.Type == goagent.StreamEventDone {
				s.done = true
			}
			return true
		}
		if chunk.Message.Content != "" {
			s.current = goagent.StreamEvent{
				Type: goagent.StreamEventText,
				Text: chunk.Message.Content,
			}
			return true
		}
	}
	if err := s.scanner.Err(); err != nil {
		s.err = fmt.Errorf("ollama: reading stream: %w", err)
	}
	return false
}

// ollamaStopReason maps the Ollama done_reason string to a goagent StopReason.
// Tool calls take priority over done_reason because Ollama may still report
// done_reason:"stop" even when the message contains tool calls.
func ollamaStopReason(reason string, hasTools bool) goagent.StopReason {
	if hasTools {
		return goagent.StopReasonToolUse
	}
	switch reason {
	case "length":
		return goagent.StopReasonMaxTokens
	default:
		return goagent.StopReasonEndTurn
	}
}

func (s *ollamaStream) Event() goagent.StreamEvent { return s.current }
func (s *ollamaStream) Err() error                 { return s.err }
func (s *ollamaStream) Close() error               { return s.resp.Body.Close() }

// CompleteStream implements goagent.StreamingProvider using Ollama's /api/chat
// endpoint with stream:true.
//
// Text tokens are delivered as StreamEventText events as they arrive.
// Tool calls appear in the final done chunk and are translated to
// StreamEventToolStart + StreamEventToolDelta events before StreamEventDone,
// so the agent loop handles them the same way as Anthropic streaming.
func (p *Provider) CompleteStream(ctx context.Context, req goagent.CompletionRequest) (goagent.Stream, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("ollama: model not set; use goagent.WithModel")
	}

	body := ollamaStreamRequest{
		Model:  req.Model,
		Stream: true,
	}

	if req.SystemPrompt != "" {
		body.Messages = append(body.Messages, ollamaNativeMsg{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	for _, m := range req.Messages {
		msg, err := toOllamaNativeMsg(m)
		if err != nil {
			return nil, err
		}
		body.Messages = append(body.Messages, msg)
	}

	for _, t := range req.Tools {
		body.Tools = append(body.Tools, ollamaNativeTool{
			Type: "function",
			Function: ollamaNativeFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	resp, err := p.client.doStream(ctx, "/api/chat", body)
	if err != nil {
		return nil, err
	}
	return &ollamaStream{
		resp:    resp,
		scanner: bufio.NewScanner(resp.Body),
	}, nil
}

// toOllamaNativeMsg converts a goagent Message to the Ollama native chat format.
func toOllamaNativeMsg(m goagent.Message) (ollamaNativeMsg, error) {
	role, err := toOpenAIRole(m.Role)
	if err != nil {
		return ollamaNativeMsg{}, err
	}

	msg := ollamaNativeMsg{
		Role:    role,
		Content: m.TextContent(),
	}

	for _, tc := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ollamaNativeCall{
			Function: ollamaNativeCallFunc{
				Name:      tc.Name,
				Arguments: tc.Arguments,
			},
		})
	}

	return msg, nil
}
