package storage_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/storage"
)

// Example demonstrates creating an InMemory storage, saving a message slice,
// and loading it back.
func Example() {
	s := storage.NewInMemory()
	ctx := context.Background()

	msgs := []goagent.Message{
		goagent.UserMessage("hello"),
		goagent.AssistantMessage("hi"),
	}
	if err := s.Save(ctx, msgs); err != nil {
		log.Fatal(err)
	}

	loaded, err := s.Load(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(loaded))
	fmt.Println(loaded[0].TextContent())
	// Output:
	// 2
	// hello
}

// ExampleInMemory_Save shows that Save replaces the entire history —
// calling Save twice keeps only the second batch.
func ExampleInMemory_Save() {
	s := storage.NewInMemory()
	ctx := context.Background()

	_ = s.Save(ctx, []goagent.Message{
		goagent.UserMessage("first"),
	})

	second := []goagent.Message{
		goagent.UserMessage("second"),
		goagent.AssistantMessage("ok"),
	}
	if err := s.Save(ctx, second); err != nil {
		log.Fatal(err)
	}

	loaded, err := s.Load(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(loaded))
	fmt.Println(loaded[0].TextContent())
	// Output:
	// 2
	// second
}
