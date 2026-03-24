// Package policy provides read Policy implementations for the goagent memory system.
//
// A Policy receives the full message history from storage and returns the subset
// that will be included in the next provider request. It has no knowledge of
// where messages are stored — that is the Storage layer's responsibility.
package policy

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

// Policy decides which messages from the full history are included in the
// next request to the provider. It is applied at read time, never at write time.
type Policy interface {
	Apply(ctx context.Context, msgs []goagent.Message) ([]goagent.Message, error)
}

// adjustStart moves start forward past any leading RoleTool messages.
// This preserves the invariant that a tool_result message is never sent
// without its preceding tool_use (assistant) message. Anthropic's API
// rejects requests that violate this ordering.
func adjustStart(msgs []goagent.Message, start int) int {
	for start < len(msgs) && msgs[start].Role == goagent.RoleTool {
		start++
	}
	return start
}
