package goagent

import (
	"context"
	"errors"
	"fmt"
)

// Agent runs a ReAct loop: it alternates between calling the LLM provider and
// dispatching tool calls until the model produces a final answer or the
// iteration budget is exhausted.
//
// By default an Agent is stateless — each Run call is independent and carries
// no memory of previous calls. To persist conversation history across calls,
// configure a ShortTermMemory via WithShortTermMemory. For semantic retrieval
// across sessions, configure a LongTermMemory via WithLongTermMemory.
//
// # Concurrency
//
// Agent itself holds no mutable state after construction; all fields are set
// once by New and never written again. Whether concurrent calls to Run are
// safe depends entirely on the implementations injected by the caller:
//
//   - Provider: safe if the implementation is. All built-in providers are.
//   - ShortTermMemory: concurrent Run calls produce undefined message ordering.
//     See WithShortTermMemory.
//   - LongTermMemory: concurrent Retrieve and Store calls produce undefined
//     ordering. See WithLongTermMemory.
//   - Logger: slog.Logger is documented as safe for concurrent use.
//
// If no memory backend is configured (the default), Run is safe to call from
// multiple goroutines simultaneously.
type Agent struct {
	opts *options
}

// New creates an Agent with the provided options applied over sensible defaults.
// A Provider must be supplied via WithProvider before calling Run.
func New(opts ...Option) *Agent {
	o := defaults()
	for _, opt := range opts {
		opt(o)
	}
	return &Agent{opts: o}
}

// Run executes the ReAct loop for the given prompt and returns the model's
// final text response.
//
// This is the main entry point for text interactions. For sending images,
// documents, or other multimodal content, use RunBlocks.
//
// If a ShortTermMemory is configured via WithShortTermMemory, Run loads the
// conversation history before calling the provider and persists the new turn
// after producing an answer (or exhausting iterations).
//
// If a LongTermMemory is configured via WithLongTermMemory, Run retrieves
// semantically relevant context before building the request, and stores the
// completed turn (subject to WritePolicy) after each Run.
//
// Memory write failures are logged at Warn level and do not cause Run to
// return an error.
//
// Possible errors:
//   - *ProviderError — the provider returned an error
//   - *MaxIterationsError — the iteration budget was exhausted
//   - context.Canceled / context.DeadlineExceeded — context was cancelled
func (a *Agent) Run(ctx context.Context, prompt string) (string, error) {
	return a.run(ctx, []ContentBlock{TextBlock(prompt)})
}

// RunBlocks executes the ReAct loop with multimodal content and returns the
// model's final text response.
//
// It accepts one or more ContentBlock in any combination of types.
// For text-only prompts, prefer Run which is more ergonomic.
//
// Example:
//
//	result, err := agent.RunBlocks(ctx,
//	    goagent.ImageBlock(imgData, "image/png"),
//	    goagent.TextBlock("What animal is this?"),
//	)
//
// Possible errors:
//   - error if no content blocks are provided
//   - ErrInvalidMediaType if a content block has an unsupported MIME type
//   - *ProviderError — the provider returned an error
//   - *UnsupportedContentError — the provider does not support a content type
//   - *MaxIterationsError — the iteration budget was exhausted
//   - context.Canceled / context.DeadlineExceeded — context was cancelled
func (a *Agent) RunBlocks(ctx context.Context, blocks ...ContentBlock) (string, error) {
	if len(blocks) == 0 {
		return "", errors.New("goagent: RunBlocks requires at least one content block")
	}
	if err := validateBlocks(blocks); err != nil {
		return "", err
	}
	return a.run(ctx, blocks)
}

// validateBlocks checks that all content blocks have valid MIME types.
func validateBlocks(blocks []ContentBlock) error {
	for _, b := range blocks {
		switch b.Type {
		case ContentImage:
			if b.Image == nil || !ValidImageMediaType(b.Image.MediaType) {
				mediaType := ""
				if b.Image != nil {
					mediaType = b.Image.MediaType
				}
				return fmt.Errorf("%w: image media type %q", ErrInvalidMediaType, mediaType)
			}
		case ContentDocument:
			if b.Document == nil || !ValidDocumentMediaType(b.Document.MediaType) {
				mediaType := ""
				if b.Document != nil {
					mediaType = b.Document.MediaType
				}
				return fmt.Errorf("%w: document media type %q", ErrInvalidMediaType, mediaType)
			}
		}
	}
	return nil
}

// run is the ReAct loop. Both Run and RunBlocks delegate here.
// content is the slice of blocks forming the initial user message.
func (a *Agent) run(ctx context.Context, content []ContentBlock) (string, error) {
	if a.opts.provider == nil {
		return "", errors.New("goagent: no provider configured; use WithProvider")
	}

	messages, historyLen, err := a.buildMessages(ctx, content)
	if err != nil {
		return "", err
	}

	// Collect tool definitions once.
	toolDefs := make([]ToolDefinition, 0, len(a.opts.tools))
	for _, t := range a.opts.tools {
		toolDefs = append(toolDefs, t.Definition())
	}

	d := newDispatcher(a.opts.tools, a.opts.logger)

	var lastContent string

	for i := 0; i < a.opts.maxIterations; i++ {
		// Respect context cancellation at each loop boundary.
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req := CompletionRequest{
			Model:        a.opts.model,
			SystemPrompt: a.opts.systemPrompt,
			Messages:     messages,
			Tools:        toolDefs,
		}

		resp, err := a.opts.provider.Complete(ctx, req)
		if err != nil {
			return "", &ProviderError{Cause: err}
		}

		messages = append(messages, resp.Message)
		lastContent = resp.Message.TextContent()

		a.opts.logger.Debug("agent iteration",
			"iteration", i+1,
			"stop_reason", resp.StopReason,
			"content_len", len(lastContent),
			"tool_calls", len(resp.Message.ToolCalls),
		)

		// Model produced a final answer — persist and return.
		if resp.StopReason != StopReasonToolUse || len(resp.Message.ToolCalls) == 0 {
			a.persistTurn(ctx, messages, historyLen, lastContent)
			return lastContent, nil
		}

		// Dispatch tool calls (fan-out/fan-in).
		results := d.dispatch(ctx, resp.Message.ToolCalls)

		for _, r := range results {
			var toolContent []ContentBlock
			if r.Err != nil {
				toolContent = []ContentBlock{TextBlock(fmt.Sprintf("Error: %s", r.Err.Error()))}
			} else {
				toolContent = r.Content
			}
			messages = append(messages, Message{
				Role:       RoleTool,
				Content:    toolContent,
				ToolCallID: r.ToolCallID,
			})
		}
	}

	// Iteration budget exhausted — persist what we have and return an error.
	a.persistTurn(ctx, messages, historyLen, lastContent)
	return "", &MaxIterationsError{
		Iterations:  a.opts.maxIterations,
		LastThought: lastContent,
	}
}

// buildMessages constructs the full message slice to send to the provider for
// this run, combining long-term context, short-term history, and the current
// user content (in that order).
//
// Returns the message slice and historyLen — the number of messages that came
// from memory (used to compute the delta for persistence at the end of run).
func (a *Agent) buildMessages(ctx context.Context, content []ContentBlock) ([]Message, int, error) {
	var longTermMsgs, shortTermMsgs []Message

	if a.opts.longTerm != nil {
		var err error
		longTermMsgs, err = a.opts.longTerm.Retrieve(ctx, content, a.opts.longTermTopK)
		if err != nil {
			return nil, 0, fmt.Errorf("goagent: retrieving long-term context: %w", err)
		}
	}

	if a.opts.shortTerm != nil {
		var err error
		shortTermMsgs, err = a.opts.shortTerm.Messages(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("goagent: loading short-term history: %w", err)
		}
	}

	historyLen := len(longTermMsgs) + len(shortTermMsgs)
	messages := make([]Message, 0, historyLen+1)
	messages = append(messages, longTermMsgs...)
	messages = append(messages, shortTermMsgs...)
	messages = append(messages, Message{Role: RoleUser, Content: content})
	return messages, historyLen, nil
}

// persistTurn saves the new messages generated during this run to the
// configured memory backends. Failures are logged at Warn and never returned
// to the caller — the response was already produced correctly.
//
// messages is the full slice (history + new turn).
// historyLen marks where the new turn begins.
// finalContent is the model's last text response.
func (a *Agent) persistTurn(ctx context.Context, messages []Message, historyLen int, finalContent string) {
	if a.opts.shortTerm != nil {
		var toStore []Message
		if a.opts.traceTools {
			// Full trace: user msg + all assistant/tool turns from this Run.
			toStore = messages[historyLen:]
		} else {
			// Condensed: only user message + final assistant answer.
			toStore = []Message{
				messages[historyLen], // always the user message
				AssistantMessage(finalContent),
			}
		}
		if err := a.opts.shortTerm.Append(ctx, toStore...); err != nil {
			a.opts.logger.Warn("short-term memory append failed", "error", err)
		}
	}

	if a.opts.longTerm != nil {
		policy := a.opts.writePolicy
		if policy == nil {
			policy = StoreAlways
		}
		// policy returns nil to discard the turn or a non-nil slice of messages
		// to store. The slice may differ from the raw prompt+response pair when
		// the policy transforms or condenses the turn (e.g. an LLM judge).
		if msgs := policy(messages[historyLen], AssistantMessage(finalContent)); msgs != nil {
			if err := a.opts.longTerm.Store(ctx, msgs...); err != nil {
				a.opts.logger.Warn("long-term memory store failed", "error", err)
			}
		}
	}
}
