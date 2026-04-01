package goagent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Germanblandin1/goagent/internal/session"
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
// Use WithName to assign a stable identity to the agent. When a LongTermMemory
// is configured, the name is used as the session namespace so that multiple
// agents sharing the same memory backend can only see their own entries.
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
//   - Circuit breaker state is protected by mutexes and is safe for concurrent
//     use across simultaneous Run calls.
//
// If no memory backend is configured (the default), Run is safe to call from
// multiple goroutines simultaneously.
type Agent struct {
	opts       *options
	dispatcher *dispatcher
}

// New creates an Agent with the provided options applied over sensible defaults.
// A Provider must be supplied via WithProvider before calling Run.
//
// If any WithMCP* options are present, New establishes the MCP connections,
// discovers their tools, and returns an error if any connection fails.
// On error, all already-opened connections are closed before returning.
//
// Call Close to release MCP connections when the agent is no longer needed:
//
//	agent, err := goagent.New(...)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer agent.Close()
func New(opts ...Option) (*Agent, error) {
	o := defaults()
	for _, opt := range opts {
		opt(o)
	}

	for _, fn := range o.mcpConnectors {
		tools, closer, err := fn(context.Background(), o.logger)
		if err != nil {
			// Close already-opened connections before returning.
			for _, c := range o.mcpClosers {
				_ = c.Close()
			}
			return nil, err
		}
		o.tools = append(o.tools, tools...)
		if closer != nil {
			o.mcpClosers = append(o.mcpClosers, closer)
		}
	}

	d := newDispatcher(o.tools, o.logger, buildDispatchChain(o))
	return &Agent{opts: o, dispatcher: d}, nil
}

// buildDispatchChain constructs the ordered middleware slice for tool dispatch.
// Order (outermost first): logging → timeout → circuit breaker → caller middlewares → panic recovery.
// Panic recovery is innermost so that upstream middlewares (circuit breaker,
// timeout) observe the recovered error rather than a raw panic.
func buildDispatchChain(o *options) []DispatchMiddleware {
	mws := make([]DispatchMiddleware, 0, 4+len(o.dispatchMWs))
	mws = append(mws, loggingMiddleware(o.logger))
	mws = append(mws, timeoutMiddleware(o.toolTimeout))
	if o.cbMaxFailures > 0 && o.cbResetTimeout > 0 {
		mws = append(mws, circuitBreakerMiddleware(o.cbMaxFailures, o.cbResetTimeout, o.hooks.OnCircuitOpen))
	}
	mws = append(mws, o.dispatchMWs...)
	mws = append(mws, panicRecoveryMiddleware())
	return mws
}

// Close releases all MCP connections opened during New.
// For stdio transports: terminates the subprocess.
// For SSE transports: closes the HTTP connection.
// Idempotent — multiple calls are safe.
// Errors are logged at Warn level; the method always returns nil.
func (a *Agent) Close() error {
	for _, c := range a.opts.mcpClosers {
		if err := c.Close(); err != nil {
			a.opts.logger.Warn("mcp client close error", "err", err)
		}
	}
	a.opts.mcpClosers = nil
	return nil
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

	runStart := time.Now()

	if fn := a.opts.hooks.OnRunStart; fn != nil {
		fn()
	}

	// Accumulators for RunResult — populated throughout the loop and
	// written to OnRunEnd / WithRunResult at every exit point.
	var (
		totalUsage     Usage
		totalToolCalls int
		totalToolTime  time.Duration
		iterations     int
		runErr         error
		lastContent    string
	)

	// finishRun fires OnRunEnd and writes WithRunResult at every exit path.
	// It is called explicitly (not via defer) so that it runs before
	// persistTurn, giving hook consumers a RunResult that reflects only the
	// loop — not the asynchronous memory write.
	finishRun := func() {
		result := RunResult{
			Duration:   time.Since(runStart),
			Iterations: iterations,
			TotalUsage: totalUsage,
			ToolCalls:  totalToolCalls,
			ToolTime:   totalToolTime,
			Err:        runErr,
		}
		if fn := a.opts.hooks.OnRunEnd; fn != nil {
			fn(result)
		}
		if a.opts.runResult != nil {
			*a.opts.runResult = result
		}
	}

	if a.opts.name != "" {
		// Inject the agent name as the session ID so that LongTermMemory and
		// InMemoryStore can scope vector entries to this agent. The name is
		// validated here: ":" is forbidden because it is the separator used in
		// the "sessionID:baseID:chunkIndex" entry ID format.
		var err error
		ctx, err = session.NewContext(ctx, a.opts.name)
		if err != nil {
			runErr = fmt.Errorf("goagent: invalid agent name %q: %w", a.opts.name, err)
			finishRun()
			return "", runErr
		}
	}

	messages, historyLen, err := a.buildMessages(ctx, content)
	if err != nil {
		runErr = err
		finishRun()
		return "", err
	}

	// Collect tool definitions once.
	toolDefs := make([]ToolDefinition, 0, len(a.opts.tools))
	for _, t := range a.opts.tools {
		toolDefs = append(toolDefs, t.Definition())
	}

	for i := 0; i < a.opts.maxIterations; i++ {
		iterations = i + 1

		// Respect context cancellation at each loop boundary.
		select {
		case <-ctx.Done():
			runErr = ctx.Err()
			finishRun()
			return "", runErr
		default:
		}

		if fn := a.opts.hooks.OnIterationStart; fn != nil {
			fn(i)
		}

		req := CompletionRequest{
			Model:        a.opts.model,
			SystemPrompt: a.opts.systemPrompt,
			Messages:     messages,
			Tools:        toolDefs,
			Thinking:     a.opts.thinking,
			Effort:       a.opts.effort,
		}

		if fn := a.opts.hooks.OnProviderRequest; fn != nil {
			fn(i, req.Model, len(req.Messages))
		}

		provStart := time.Now()
		resp, err := a.opts.provider.Complete(ctx, req)
		provDuration := time.Since(provStart)

		if err != nil {
			if fn := a.opts.hooks.OnProviderResponse; fn != nil {
				fn(i, ProviderEvent{
					Duration: provDuration,
					Err:      err,
				})
			}
			runErr = &ProviderError{Cause: err}
			finishRun()
			return "", runErr
		}

		totalUsage.add(resp.Usage)

		if fn := a.opts.hooks.OnProviderResponse; fn != nil {
			fn(i, ProviderEvent{
				Duration:   provDuration,
				Usage:      resp.Usage,
				StopReason: resp.StopReason,
				ToolCalls:  len(resp.Message.ToolCalls),
			})
		}

		messages = append(messages, resp.Message)
		lastContent = resp.Message.TextContent()

		if fn := a.opts.hooks.OnThinking; fn != nil {
			for _, block := range resp.Message.Content {
				if block.Type == ContentThinking && block.Thinking != nil {
					fn(block.Thinking.Thinking)
				}
			}
		}

		a.opts.logger.Debug("agent iteration",
			"iteration", i+1,
			"stop_reason", resp.StopReason,
			"content_len", len(lastContent),
			"tool_calls", len(resp.Message.ToolCalls),
		)

		// Model produced a final answer — persist and return.
		if resp.StopReason != StopReasonToolUse || len(resp.Message.ToolCalls) == 0 {
			if fn := a.opts.hooks.OnResponse; fn != nil {
				fn(lastContent, i+1)
			}
			finishRun()
			a.persistTurn(ctx, messages, historyLen, lastContent)
			return lastContent, nil
		}

		if fn := a.opts.hooks.OnToolCall; fn != nil {
			for _, tc := range resp.Message.ToolCalls {
				fn(tc.Name, tc.Arguments)
			}
		}

		// Dispatch tool calls (fan-out/fan-in).
		results := a.dispatcher.dispatch(ctx, resp.Message.ToolCalls)

		if fn := a.opts.hooks.OnToolResult; fn != nil {
			for _, r := range results {
				fn(r.Name, r.Content, r.Duration, r.Err)
			}
		}

		// Accumulate tool metrics.
		totalToolCalls += len(results)
		for _, r := range results {
			totalToolTime += r.Duration
		}

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
	if fn := a.opts.hooks.OnResponse; fn != nil {
		fn(lastContent, a.opts.maxIterations)
	}
	runErr = &MaxIterationsError{
		Iterations:  a.opts.maxIterations,
		LastThought: lastContent,
	}
	finishRun()
	a.persistTurn(ctx, messages, historyLen, lastContent)
	return "", runErr
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
		start := time.Now()
		var rerr error
		longTermMsgs, rerr = a.opts.longTerm.Retrieve(ctx, content, a.opts.longTermTopK)
		if fn := a.opts.hooks.OnLongTermRetrieve; fn != nil {
			fn(len(longTermMsgs), time.Since(start), rerr)
		}
		if rerr != nil {
			return nil, 0, fmt.Errorf("goagent: retrieving long-term context: %w", rerr)
		}
	}

	if a.opts.shortTerm != nil {
		start := time.Now()
		var rerr error
		shortTermMsgs, rerr = a.opts.shortTerm.Messages(ctx)
		if fn := a.opts.hooks.OnShortTermLoad; fn != nil {
			fn(len(shortTermMsgs), time.Since(start), rerr)
		}
		if rerr != nil {
			return nil, 0, fmt.Errorf("goagent: loading short-term history: %w", rerr)
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
			// Thinking blocks are stripped — they have no value in historical
			// context and the API discards them from prior turns anyway.
			toStore = stripThinking(messages[historyLen:])
		} else {
			// Condensed: only user message + final assistant answer.
			toStore = []Message{
				messages[historyLen], // always the user message
				AssistantMessage(finalContent),
			}
		}
		start := time.Now()
		serr := a.opts.shortTerm.Append(ctx, toStore...)
		if fn := a.opts.hooks.OnShortTermAppend; fn != nil {
			fn(len(toStore), time.Since(start), serr)
		}
		if serr != nil {
			a.opts.logger.Warn("short-term memory append failed", "error", serr)
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
			start := time.Now()
			serr := a.opts.longTerm.Store(ctx, msgs...)
			if fn := a.opts.hooks.OnLongTermStore; fn != nil {
				fn(len(msgs), time.Since(start), serr)
			}
			if serr != nil {
				a.opts.logger.Warn("long-term memory store failed", "error", serr)
			}
		}
	}
}

// stripThinking returns a copy of msgs with all ContentThinking blocks
// removed from each message's Content. The original slice is not modified.
// Used to clean messages before persisting to memory — thinking blocks have
// no value in historical context once the turn is complete.
func stripThinking(msgs []Message) []Message {
	result := make([]Message, 0, len(msgs))
	for _, msg := range msgs {
		cleaned := Message{
			Role:       msg.Role,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}
		for _, block := range msg.Content {
			if block.Type != ContentThinking {
				cleaned.Content = append(cleaned.Content, block)
			}
		}
		result = append(result, cleaned)
	}
	return result
}
