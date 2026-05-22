package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

const judgeSystemPrompt = `You are a conversation memory judge.
Your sole task is to decide whether a conversation exchange contains information
worth persisting for long-term recall across sessions.

You MUST respond with a single JSON object — no markdown, no explanation, nothing else:
{"save": true, "message": "<one-sentence summary of what to remember>"}
{"save": false, "message": ""}

Rules:
- save: true only for exchanges that contain durable information: facts, user
  preferences, technical explanations, decisions, or named entities that would
  be useful to recall in a future session.
- save: false for greetings, small talk, trivial affirmations ("ok", "thanks"),
  or any exchange with no information worth recalling later.
- message: a concise one-sentence summary of what to remember. Empty string
  when save is false.
- Any response that is not valid JSON will be treated as save: false.`

// judgeResult is the structured verdict produced by the judge agent.
// The judge must emit exactly this JSON shape and nothing else.
type judgeResult struct {
	Save    bool   `json:"save"`
	Message string `json:"message"`
}

// newJudgePolicy returns a goagent.WritePolicy backed by a stateless judge agent.
//
// The judge agent is a second, independent Agent instance with no memory of its
// own. It receives a single formatted exchange (user prompt + assistant response)
// and must reply with a judgeResult JSON object.
//
// The returned WritePolicy:
//  1. Sends the prompt+response to the judge agent via a single Run call.
//  2. Parses the JSON reply into a judgeResult.
//  3. Returns nil if save is false, the JSON is malformed, or the agent call
//     fails — in all three cases the turn is silently discarded and the main
//     agent is unaffected.
//  4. Returns a single-message slice containing the judge's curated summary
//     (RoleAssistant) when save is true. This replaces the raw prompt+response
//     pair — only the distilled memory entry reaches LongTermMemory.Store.
//
// ctx should be the application-level context (e.g. from signal.NotifyContext)
// so the judge respects cancellation and shutdown signals.
func newJudgePolicy(ctx context.Context) goagent.WritePolicy {
	judge, err := goagent.New(
		goagent.WithProvider(ollama.New()),
		goagent.WithModel("gpt-oss:120b-cloud"),
		goagent.WithSystemPrompt(judgeSystemPrompt),
	)
	if err != nil {
		log.Fatal(err)
	}

	return func(prompt, response goagent.Message) []goagent.Message {
		input := fmt.Sprintf("User: %s\nAssistant: %s", prompt.TextContent(), response.TextContent())

		reply, err := judge.Run(ctx, input)
		if err != nil {
			log.Printf("judge: agent error: %v", err)
			return nil
		}

		var result judgeResult
		if err := json.Unmarshal([]byte(reply), &result); err != nil {
			log.Printf("judge: invalid JSON %q: %v", reply, err)
			return nil
		}

		if !result.Save {
			return nil
		}

		log.Printf("judge: storing — %s", result.Message)

		// Return the judge's curated summary as the sole stored message.
		// Using RoleAssistant signals this is a condensed knowledge entry, not
		// a verbatim user turn. The original raw exchange is not persisted.
		return []goagent.Message{
			goagent.AssistantMessage(result.Message),
		}
	}
}
