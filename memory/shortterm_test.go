package memory_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/policy"
	"github.com/Germanblandin1/goagent/memory/storage"
)

func TestNewShortTerm_Default_Empty(t *testing.T) {
	t.Parallel()

	m := memory.NewShortTerm()
	msgs, err := m.Messages(context.Background())
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages on a fresh instance, got %d", len(msgs))
	}
}

func TestShortTerm_AppendAndMessages_Roundtrip(t *testing.T) {
	t.Parallel()

	m := memory.NewShortTerm()
	ctx := context.Background()

	want := []goagent.Message{
		goagent.UserMessage("hello"),
		goagent.AssistantMessage("hi there"),
	}

	if err := m.Append(ctx, want...); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := m.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].TextContent() != want[i].TextContent() {
			t.Errorf("msg[%d] text = %q, want %q", i, got[i].TextContent(), want[i].TextContent())
		}
		if got[i].Role != want[i].Role {
			t.Errorf("msg[%d] role = %q, want %q", i, got[i].Role, want[i].Role)
		}
	}
}

func TestShortTerm_Append_Empty_IsNoOp(t *testing.T) {
	t.Parallel()

	m := memory.NewShortTerm()
	ctx := context.Background()

	if err := m.Append(ctx); err != nil {
		t.Errorf("Append with no args: unexpected error: %v", err)
	}

	msgs, err := m.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after empty Append, got %d", len(msgs))
	}
}

func TestShortTerm_MultipleAppends_Accumulate(t *testing.T) {
	t.Parallel()

	m := memory.NewShortTerm()
	ctx := context.Background()

	_ = m.Append(ctx, goagent.UserMessage("first"))
	_ = m.Append(ctx, goagent.AssistantMessage("second"))

	msgs, err := m.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestNewShortTerm_WithPolicy_AppliesAtRead(t *testing.T) {
	t.Parallel()

	// FixedWindow(1) keeps only the last message.
	m := memory.NewShortTerm(memory.WithPolicy(policy.NewFixedWindow(1)))
	ctx := context.Background()

	_ = m.Append(ctx,
		goagent.UserMessage("first"),
		goagent.UserMessage("second"),
	)

	msgs, err := m.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (FixedWindow applied), got %d", len(msgs))
	}
	if msgs[0].TextContent() != "second" {
		t.Errorf("got %q, want %q", msgs[0].TextContent(), "second")
	}
}

func TestNewShortTerm_WithStorage_UsesProvidedBackend(t *testing.T) {
	t.Parallel()

	store := storage.NewInMemory()
	m := memory.NewShortTerm(memory.WithStorage(store))
	ctx := context.Background()

	_ = m.Append(ctx, goagent.UserMessage("hello"))

	// Verify the storage backend actually received the message.
	stored, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(stored) != 1 || stored[0].TextContent() != "hello" {
		t.Errorf("storage has %v, want [hello]", stored)
	}
}
