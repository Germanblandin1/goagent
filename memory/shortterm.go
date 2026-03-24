package memory

import (
	"context"
	"fmt"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/policy"
	"github.com/Germanblandin1/goagent/memory/storage"
)

type shortTermConfig struct {
	storage storage.Storage
	policy  policy.Policy
}

// ShortTermOption configures a ShortTermMemory created by NewShortTerm.
type ShortTermOption func(*shortTermConfig)

// WithStorage sets the storage backend for a ShortTermMemory.
// Default: in-process slice (storage.NewInMemory).
func WithStorage(s storage.Storage) ShortTermOption {
	return func(c *shortTermConfig) { c.storage = s }
}

// WithPolicy sets the read policy for a ShortTermMemory.
// Default: NoOp (all messages are passed to the provider unchanged).
func WithPolicy(p policy.Policy) ShortTermOption {
	return func(c *shortTermConfig) { c.policy = p }
}

// NewShortTerm creates a ShortTermMemory with the given options.
// Defaults: in-process storage + NoOp policy (full history sent to provider).
func NewShortTerm(opts ...ShortTermOption) goagent.ShortTermMemory {
	cfg := &shortTermConfig{
		storage: storage.NewInMemory(),
		policy:  policy.NewNoOp(),
	}
	for _, o := range opts {
		o(cfg)
	}
	return &shortTermMemory{storage: cfg.storage, policy: cfg.policy}
}

type shortTermMemory struct {
	storage storage.Storage
	policy  policy.Policy
}

func (m *shortTermMemory) Messages(ctx context.Context) ([]goagent.Message, error) {
	all, err := m.storage.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading messages: %w", err)
	}
	return m.policy.Apply(ctx, all)
}

func (m *shortTermMemory) Append(ctx context.Context, msgs ...goagent.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	return m.storage.Append(ctx, msgs...)
}
