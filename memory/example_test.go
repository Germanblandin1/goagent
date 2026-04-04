package memory_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/policy"
	"github.com/Germanblandin1/goagent/memory/storage"
)

// Example demonstrates appending messages to a ShortTermMemory and reading
// them back via Messages.
func Example() {
	mem := memory.NewShortTerm()
	ctx := context.Background()

	if err := mem.Append(ctx,
		goagent.UserMessage("hello"),
		goagent.AssistantMessage("hi"),
	); err != nil {
		log.Fatal(err)
	}

	msgs, err := mem.Messages(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(msgs))
	fmt.Println(msgs[0].TextContent())
	// Output:
	// 2
	// hello
}

// ExampleNewShortTerm_withPolicy shows how a FixedWindow policy limits the
// number of messages returned by Messages, even when more are stored.
func ExampleNewShortTerm_withPolicy() {
	mem := memory.NewShortTerm(
		memory.WithStorage(storage.NewInMemory()),
		memory.WithPolicy(policy.NewFixedWindow(2)),
	)
	ctx := context.Background()

	if err := mem.Append(ctx,
		goagent.UserMessage("one"),
		goagent.AssistantMessage("two"),
		goagent.UserMessage("three"),
		goagent.AssistantMessage("four"),
		goagent.UserMessage("five"),
	); err != nil {
		log.Fatal(err)
	}

	visible, err := mem.Messages(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(visible))
	fmt.Println(visible[0].TextContent())
	// Output:
	// 2
	// four
}

// ExampleNewLongTerm shows how to construct a LongTermMemory.
// No Output: is provided because both WithVectorStore and WithEmbedder require
// real implementations (a vector database and an embedding model).
func ExampleNewLongTerm() {
	_, err := memory.NewLongTerm(
		memory.WithVectorStore(stubVectorStore{}),
		memory.WithEmbedder(stubEmbedder{}),
		memory.WithTopK(5),
	)
	if err != nil {
		log.Fatal(err)
	}
}

type stubVectorStore struct{}

func (stubVectorStore) Upsert(_ context.Context, _ string, _ []float32, _ goagent.Message) error {
	return nil
}

func (stubVectorStore) Search(_ context.Context, _ []float32, _ int) ([]goagent.ScoredMessage, error) {
	return nil, nil
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}
