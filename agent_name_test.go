package goagent_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/session"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// TestWithName_LongTermMemoryNamespaceIsolation verifies that two agents
// configured with different names but sharing the same InMemoryStore cannot
// see each other's stored entries.
func TestWithName_LongTermMemoryNamespaceIsolation(t *testing.T) {
	t.Parallel()

	store := vector.NewInMemoryStore()
	embedder := &testutil.MockEmbedder{Dim: 4}

	ltmAlice, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(embedder),
	)
	if err != nil {
		t.Fatalf("NewLongTerm alice: %v", err)
	}

	ltmBob, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(embedder),
	)
	if err != nil {
		t.Fatalf("NewLongTerm bob: %v", err)
	}

	alice, err := goagent.New(
		goagent.WithName("alice"),
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("alice-answer"))),
		goagent.WithLongTermMemory(ltmAlice),
	)
	if err != nil {
		t.Fatalf("New alice: %v", err)
	}

	bob, err := goagent.New(
		goagent.WithName("bob"),
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("bob-answer"))),
		goagent.WithLongTermMemory(ltmBob),
	)
	if err != nil {
		t.Fatalf("New bob: %v", err)
	}

	if _, err := alice.Run(context.Background(), "hello from alice"); err != nil {
		t.Fatalf("alice.Run: %v", err)
	}
	if _, err := bob.Run(context.Background(), "hello from bob"); err != nil {
		t.Fatalf("bob.Run: %v", err)
	}

	queryVec, err := embedder.Embed(context.Background(), []goagent.ContentBlock{goagent.TextBlock("hello")})
	if err != nil {
		t.Fatalf("Embed query: %v", err)
	}

	// Alice's view: should only see her own entries.
	ctxAlice, err := session.NewContext(context.Background(), "alice")
	if err != nil {
		t.Fatalf("NewContext alice: %v", err)
	}
	aliceMsgs, err := store.Search(ctxAlice, queryVec, 10)
	if err != nil {
		t.Fatalf("Search alice: %v", err)
	}
	for _, msg := range aliceMsgs {
		text := msg.Message.TextContent()
		if text == "hello from bob" || text == "bob-answer" {
			t.Errorf("alice's view contains bob's message: %q", text)
		}
	}

	// Bob's view: should only see his own entries.
	ctxBob, err := session.NewContext(context.Background(), "bob")
	if err != nil {
		t.Fatalf("NewContext bob: %v", err)
	}
	bobMsgs, err := store.Search(ctxBob, queryVec, 10)
	if err != nil {
		t.Fatalf("Search bob: %v", err)
	}
	for _, msg := range bobMsgs {
		text := msg.Message.TextContent()
		if text == "hello from alice" || text == "alice-answer" {
			t.Errorf("bob's view contains alice's message: %q", text)
		}
	}

	// Each agent must have stored at least one entry.
	if len(aliceMsgs) == 0 {
		t.Error("alice has no stored messages")
	}
	if len(bobMsgs) == 0 {
		t.Error("bob has no stored messages")
	}
}
