// Package policy provides Policy implementations for the goagent memory system.
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
