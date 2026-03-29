package policy

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

type fixedWindowPolicy struct {
	maxGroups int
}

// NewFixedWindow returns a Policy that limits history to the most recent n groups.
// A "group" is an atomic exchange: a simple message (user or assistant without
// tools) or a linked set of assistant+tool_use + all its tool results.
//
// Using groups instead of individual messages guarantees that the tool call
// invariant is never violated: a tool_result is never sent without its
// corresponding tool_use.
//
// For a history without tool calls, NewFixedWindow(n) is equivalent to the
// last n messages — behavior is identical.
//
// If n is zero or negative, Apply returns nil.
func NewFixedWindow(n int) Policy {
	return &fixedWindowPolicy{maxGroups: n}
}

func (f *fixedWindowPolicy) Apply(_ context.Context, msgs []goagent.Message) ([]goagent.Message, error) {
	if len(msgs) == 0 || f.maxGroups <= 0 {
		return nil, nil
	}

	groups := buildGroups(msgs)

	start := 0
	if len(groups) > f.maxGroups {
		start = len(groups) - f.maxGroups
	}

	from := groups[start].start
	out := make([]goagent.Message, len(msgs)-from)
	copy(out, msgs[from:])
	return out, nil
}
