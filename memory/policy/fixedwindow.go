package policy

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

type fixedWindowPolicy struct {
	n int
}

// NewFixedWindow returns a Policy that keeps the most recent n messages.
// If the history has fewer than n messages, all messages are returned.
//
// The policy preserves the tool call invariant: if truncation would leave a
// leading RoleTool message without its preceding assistant tool_use, those
// orphaned tool results are also discarded.
//
// Panics if n is zero or negative — this is a programming error.
func NewFixedWindow(n int) Policy {
	if n <= 0 {
		panic("policy: FixedWindow size must be positive")
	}
	return &fixedWindowPolicy{n: n}
}

func (f *fixedWindowPolicy) Apply(_ context.Context, msgs []goagent.Message) ([]goagent.Message, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	start := 0
	if len(msgs) > f.n {
		start = len(msgs) - f.n
	}
	start = adjustStart(msgs, start)
	if start >= len(msgs) {
		return nil, nil
	}
	out := make([]goagent.Message, len(msgs)-start)
	copy(out, msgs[start:])
	return out, nil
}
