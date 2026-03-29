package policy

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"  // register GIF decoder for image.DecodeConfig
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig

	"github.com/Germanblandin1/goagent"
)

// TokenizerFunc counts the tokens consumed by a single message.
// It is used by TokenWindow to decide how many messages fit within the budget.
//
// The function must be pure and safe for concurrent use. Returning a value ≤ 0
// is treated as zero cost for that message.
type TokenizerFunc func(msg goagent.Message) int

// TokenWindowOption configures a TokenWindow policy.
type TokenWindowOption func(*tokenWindowPolicy)

// WithTokenizer sets the TokenizerFunc used to estimate message cost.
// Use this to plug in the exact tokenizer of your provider (e.g. tiktoken for
// OpenAI-compatible models, or Anthropic's token-counting endpoint).
//
// If WithTokenizer is not provided, TokenWindow uses the built-in heuristic:
//   - Text blocks: len(text)/4 tokens
//   - Image blocks (JPEG/PNG/GIF): ceil(w/64)×ceil(h/64)×170 tokens, derived
//     from the image dimensions decoded at runtime. Falls back to 2500 tokens
//     when the format is unsupported (e.g. WebP, which requires the external
//     dependency golang.org/x/image/webp and is not decoded by the stdlib).
//     The fallback is conservative on purpose: overestimating trims the window
//     slightly, while underestimating can cause the provider to reject the
//     request outright.
//   - Document blocks: len(data)/1500 tokens
//   - Per-message overhead: 4 tokens
func WithTokenizer(fn TokenizerFunc) TokenWindowOption {
	return func(p *tokenWindowPolicy) { p.tokenizer = fn }
}

type tokenWindowPolicy struct {
	maxTokens int
	tokenizer TokenizerFunc
}

// NewTokenWindow returns a Policy that keeps the most recent groups that fit
// within maxTokens estimated tokens.
//
// Like FixedWindow, it operates on atomic groups, guaranteeing that the tool
// call invariant is never violated.
//
// If the most recent group alone exceeds the budget, it is included anyway —
// better to send some context than none.
//
// By default, token cost is estimated with the heuristic len(content)/4 + 4
// per message. To use an exact count, pass WithTokenizer with the tokenizer
// of your chosen provider.
//
// NewTokenWindow panics if maxTokens is zero or negative — this is a
// programming error that cannot be corrected at runtime.
func NewTokenWindow(maxTokens int, opts ...TokenWindowOption) Policy {
	if maxTokens <= 0 {
		panic("policy: TokenWindow maxTokens must be positive")
	}
	p := &tokenWindowPolicy{
		maxTokens: maxTokens,
		tokenizer: estimateTokens,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (t *tokenWindowPolicy) Apply(_ context.Context, msgs []goagent.Message) ([]goagent.Message, error) {
	if len(msgs) == 0 {
		return nil, nil
	}

	groups := buildGroups(msgs)

	// Always include the most recent group even if it exceeds the budget.
	lastIncluded := len(groups) - 1
	budget := t.maxTokens - groupTokens(groups[lastIncluded], msgs, t.tokenizer)

	for i := lastIncluded - 1; i >= 0; i-- {
		cost := groupTokens(groups[i], msgs, t.tokenizer)
		if budget-cost < 0 {
			break
		}
		budget -= cost
		lastIncluded = i
	}

	from := groups[lastIncluded].start
	out := make([]goagent.Message, len(msgs)-from)
	copy(out, msgs[from:])
	return out, nil
}

// estimateTokens returns a rough token estimate for a single message.
//   - Text: len(text)/4 tokens.
//   - Image (JPEG/PNG/GIF): Anthropic formula ceil(w/64)×ceil(h/64)×170,
//     decoded from the raw bytes. Falls back to 2500 on decode failure.
//   - Document: len(data)/1500 tokens.
//   - Overhead: 4 tokens per message.
func estimateTokens(msg goagent.Message) int {
	tokens := 4 // per-message overhead
	for _, b := range msg.Content {
		switch b.Type {
		case goagent.ContentText:
			tokens += len(b.Text) / 4
		case goagent.ContentImage:
			if b.Image != nil {
				tokens += imageTokens(b.Image.Data)
			}
		case goagent.ContentDocument:
			if b.Document != nil {
				tokens += len(b.Document.Data) / 1500
			}
		}
	}
	return tokens
}

// imageTokens computes the token cost of an image using Anthropic's formula:
// ceil(width/64) × ceil(height/64) × 170.
//
// The image dimensions are decoded from the raw bytes using the stdlib image
// package (JPEG, PNG, GIF). WebP is not supported by the stdlib; those images
// fall back to 2500 tokens. The fallback is intentionally conservative:
// overestimating keeps more messages out of the window, which is safe —
// underestimating could cause the provider to reject the request because the
// actual token count exceeds the context limit. Pass [WithTokenizer] to
// override this estimate when exact costs are required (e.g. by calling
// Anthropic's token-counting endpoint directly).
func imageTokens(data []byte) int {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 2500 // conservative fallback: better to drop a message than hit provider limits
	}
	w := (cfg.Width + 63) / 64  // ceil(width/64)
	h := (cfg.Height + 63) / 64 // ceil(height/64)
	return w * h * 170
}
