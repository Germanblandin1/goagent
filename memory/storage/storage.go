// Package storage provides Storage implementations for the goagent memory system.
//
// Storage is responsible solely for persisting and loading the full message
// slice. It has no knowledge of which messages to discard or how to filter
// them — that responsibility belongs to the Policy layer.
package storage

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

// Storage persists and loads the complete message history without any
// filtering logic. Load always returns all stored messages. Save replaces
// the entire stored state — the caller decides what subset to save.
// Append adds messages to the existing history without loading the full state.
// Implementations must be safe for concurrent use.
type Storage interface {
	Load(ctx context.Context) ([]goagent.Message, error)
	Save(ctx context.Context, msgs []goagent.Message) error
	Append(ctx context.Context, msgs ...goagent.Message) error
}
