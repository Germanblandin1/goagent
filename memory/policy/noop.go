package policy

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

type noopPolicy struct{}

// NewNoOp returns a Policy that passes all messages through unchanged.
// It is the default policy when constructing a ShortTermMemory without
// an explicit policy — the full history is always sent to the provider.
func NewNoOp() Policy {
	return &noopPolicy{}
}

func (n *noopPolicy) Apply(_ context.Context, msgs []goagent.Message) ([]goagent.Message, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	out := make([]goagent.Message, len(msgs))
	copy(out, msgs)
	return out, nil
}
