package storage_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/storage"
)

func TestInMemory_Load_Empty(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	got, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil slice for empty storage, got %v", got)
	}
}

func TestInMemory_SaveAndLoad_RoundTrip(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	msgs := []goagent.Message{
		goagent.UserMessage("hello"),
		goagent.AssistantMessage("hi there"),
	}
	if err := s.Save(context.Background(), msgs); err != nil {
		t.Fatalf("Save: unexpected error: %v", err)
	}
	got, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if len(got) != len(msgs) {
		t.Fatalf("got %d messages, want %d", len(got), len(msgs))
	}
	for i, want := range msgs {
		if got[i].TextContent() != want.TextContent() || got[i].Role != want.Role {
			t.Errorf("msgs[%d]: got %+v, want %+v", i, got[i], want)
		}
	}
}

func TestInMemory_Save_ReplacesExisting(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	_ = s.Save(context.Background(), []goagent.Message{
		goagent.UserMessage("old"),
	})
	newMsgs := []goagent.Message{
		goagent.UserMessage("new-1"),
		goagent.UserMessage("new-2"),
	}
	_ = s.Save(context.Background(), newMsgs)
	got, _ := s.Load(context.Background())
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}
	if got[0].TextContent() != "new-1" {
		t.Errorf("got[0].TextContent() = %q, want %q", got[0].TextContent(), "new-1")
	}
}

func TestInMemory_Load_DefensiveCopy(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	_ = s.Save(context.Background(), []goagent.Message{
		goagent.UserMessage("original"),
	})
	got, _ := s.Load(context.Background())
	got[0].Content = []goagent.ContentBlock{goagent.TextBlock("mutated")}

	got2, _ := s.Load(context.Background())
	if got2[0].TextContent() != "original" {
		t.Error("Load defensive copy violated: mutating the result affected internal state")
	}
}

func TestInMemory_Save_DefensiveCopy(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	msgs := []goagent.Message{goagent.UserMessage("original")}
	_ = s.Save(context.Background(), msgs)
	msgs[0].Content = []goagent.ContentBlock{goagent.TextBlock("mutated after save")}

	got, _ := s.Load(context.Background())
	if got[0].TextContent() != "original" {
		t.Error("Save defensive copy violated: mutating the source slice affected stored state")
	}
}

func TestInMemory_Append_ToEmpty(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	ctx := context.Background()

	err := s.Append(ctx, goagent.UserMessage("hello"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, _ := s.Load(ctx)
	if len(got) != 1 || got[0].TextContent() != "hello" {
		t.Fatalf("got %v, want [hello]", got)
	}
}

func TestInMemory_Append_ToExisting(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	ctx := context.Background()

	_ = s.Save(ctx, []goagent.Message{goagent.UserMessage("first")})
	_ = s.Append(ctx, goagent.AssistantMessage("second"), goagent.UserMessage("third"))

	got, _ := s.Load(ctx)
	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3", len(got))
	}
	if got[0].TextContent() != "first" || got[2].TextContent() != "third" {
		t.Errorf("unexpected messages: %v", got)
	}
}

func TestInMemory_Append_Empty(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	_ = s.Save(context.Background(), []goagent.Message{goagent.UserMessage("existing")})

	if err := s.Append(context.Background()); err != nil {
		t.Fatalf("Append with no messages: %v", err)
	}

	got, _ := s.Load(context.Background())
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
}

func TestInMemory_Append_DefensiveCopy(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	ctx := context.Background()

	msg := goagent.UserMessage("original")
	_ = s.Append(ctx, msg)

	// Mutate the source after appending.
	msg.Content = []goagent.ContentBlock{goagent.TextBlock("mutated")}

	got, _ := s.Load(ctx)
	if got[0].TextContent() != "original" {
		t.Error("Append defensive copy violated: mutating the source affected stored state")
	}
}

func TestInMemory_ConcurrentReadersAndWriter(t *testing.T) {
	t.Parallel()
	s := storage.NewInMemory()
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = s.Save(ctx, []goagent.Message{goagent.UserMessage("msg")})
		}
	}()
	const readers = 10
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = s.Load(ctx)
			}
		}()
	}
	wg.Wait()
}
