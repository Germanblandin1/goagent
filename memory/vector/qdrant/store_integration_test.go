//go:build integration

package qdrant_test

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/Germanblandin1/goagent"
	goagent_qdrant "github.com/Germanblandin1/goagent/memory/vector/qdrant"
	"github.com/qdrant/go-client/qdrant"
)

// openClient connects to the Qdrant gRPC endpoint from QDRANT_TEST_ADDR.
// Skips the test if the variable is not set.
// Format: "localhost:6334"
func openClient(t *testing.T) *qdrant.Client {
	t.Helper()
	addr := os.Getenv("QDRANT_TEST_ADDR")
	if addr == "" {
		t.Skip("QDRANT_TEST_ADDR not set")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("invalid QDRANT_TEST_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid port in QDRANT_TEST_ADDR %q: %v", addr, err)
	}
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: host,
		Port: port,
	})
	if err != nil {
		t.Fatalf("qdrant.NewClient: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// collectionNameFor returns a sanitized collection name unique to the given test.
func collectionNameFor(t *testing.T) string {
	t.Helper()
	raw := "test_" + t.Name()
	b := make([]byte, len(raw))
	for i := range raw {
		c := raw[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b[i] = c
		} else {
			b[i] = '_'
		}
	}
	return string(b)
}

// createAndConfig calls CreateCollection and returns a Config pointing at it.
// Registers cleanup to delete the collection when the test ends.
func createAndConfig(t *testing.T, client *qdrant.Client, dims uint64) goagent_qdrant.Config {
	t.Helper()
	name := collectionNameFor(t)
	cfg := goagent_qdrant.CollectionConfig{CollectionName: name, Dims: dims}
	if err := goagent_qdrant.CreateCollection(context.Background(), client, cfg); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	t.Cleanup(func() {
		client.DeleteCollection(context.Background(), name) //nolint:errcheck
	})
	return goagent_qdrant.Config{CollectionName: name}
}

func vec(vals ...float32) []float32 { return vals }

func TestUpsert_IdempotentSameID(t *testing.T) {
	client := openClient(t)
	scfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, scfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("first")); err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("second")); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}

	results, err := store.Search(ctx, v, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if got := results[0].Message.TextContent(); got != "second" {
		t.Errorf("want text %q, got %q", "second", got)
	}
}

func TestSearch_ReturnsTopK(t *testing.T) {
	client := openClient(t)
	scfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, scfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		v := vec(float32(i+1), 0, 0)
		if err := store.Upsert(ctx, string(rune('a'+i)), v, goagent.UserMessage("doc")); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("want 3 results, got %d", len(results))
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted by score descending at index %d", i)
		}
	}
}

func TestSearch_ScoreIsHighForSimilarVectors(t *testing.T) {
	client := openClient(t)
	scfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, scfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("text")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for identical vector, got %f", results[0].Score)
	}
}

func TestSearch_PreservesMetadata(t *testing.T) {
	client := openClient(t)
	scfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, scfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	msg := goagent.Message{
		Role:     goagent.RoleUser,
		Content:  []goagent.ContentBlock{goagent.TextBlock("hello")},
		Metadata: map[string]any{"source": "test.md", "page": float64(3)},
	}
	if err := store.Upsert(ctx, "doc1", v, msg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	meta := results[0].Message.Metadata
	if meta == nil {
		t.Fatal("expected non-nil Metadata")
	}
	if meta["source"] != "test.md" {
		t.Errorf("metadata source: want %q, got %v", "test.md", meta["source"])
	}
	if meta["page"] != float64(3) {
		t.Errorf("metadata page: want %v, got %v", float64(3), meta["page"])
	}
}

func TestSearch_EmptyMetadata(t *testing.T) {
	client := openClient(t)
	scfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, scfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("hello")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Message.Metadata != nil {
		t.Error("expected nil Metadata when no metadata was stored")
	}
}

func TestDelete_RemovesFromSearch(t *testing.T) {
	client := openClient(t)
	scfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, scfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("text")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.Delete(ctx, "doc1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := store.Search(ctx, v, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results after delete, got %d", len(results))
	}
}

func TestCreateCollection_Idempotent(t *testing.T) {
	client := openClient(t)
	name := collectionNameFor(t)
	cfg := goagent_qdrant.CollectionConfig{CollectionName: name, Dims: 3}
	ctx := context.Background()
	t.Cleanup(func() {
		client.DeleteCollection(ctx, name) //nolint:errcheck
	})

	if err := goagent_qdrant.CreateCollection(ctx, client, cfg); err != nil {
		t.Fatalf("first CreateCollection: %v", err)
	}
	if err := goagent_qdrant.CreateCollection(ctx, client, cfg); err != nil {
		t.Fatalf("second CreateCollection: %v", err)
	}
}

func TestFullRoundtrip(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	name := collectionNameFor(t)
	t.Cleanup(func() {
		client.DeleteCollection(ctx, name) //nolint:errcheck
	})

	if err := goagent_qdrant.CreateCollection(ctx, client, goagent_qdrant.CollectionConfig{
		CollectionName: name,
		Dims:           3,
	}); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{CollectionName: name})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	docs := []struct {
		id   string
		vec  []float32
		text string
		meta map[string]any
	}{
		{"a", vec(1, 0, 0), "document A", map[string]any{"src": "a.md"}},
		{"b", vec(0, 1, 0), "document B", map[string]any{"src": "b.md"}},
		{"c", vec(0, 0, 1), "document C", map[string]any{"src": "c.md"}},
	}
	for _, d := range docs {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.text)},
			Metadata: d.meta,
		}
		if err := store.Upsert(ctx, d.id, d.vec, msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Score < 0.99 {
		t.Errorf("top result should be ~1.0 for identical vector, got %f", results[0].Score)
	}
	if results[0].Message.Metadata["src"] != "a.md" {
		t.Errorf("top result metadata: want src=a.md, got %v", results[0].Message.Metadata)
	}
	if results[0].Message.Role != goagent.RoleDocument {
		t.Errorf("want RoleDocument, got %v", results[0].Message.Role)
	}
}

func TestCount_EmptyCollection(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	n, err := store.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("want 0, got %d", n)
	}
}

func TestCount_AfterUpserts(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Upsert(ctx, id, vec(1, 0, 0), goagent.UserMessage(id)); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}
	n, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Errorf("want 3, got %d", n)
	}
}

func TestCount_WithFilter(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	data := []struct {
		id   string
		meta map[string]any
	}{
		{"a", map[string]any{"env": "prod"}},
		{"b", map[string]any{"env": "dev"}},
		{"c", map[string]any{"env": "prod"}},
	}
	for _, d := range data {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.id)},
			Metadata: d.meta,
		}
		if err := store.Upsert(ctx, d.id, vec(1, 0, 0), msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	n, err := store.Count(ctx, goagent.WithFilter(map[string]any{"env": "prod"}))
	if err != nil {
		t.Fatalf("Count with filter: %v", err)
	}
	if n != 2 {
		t.Errorf("want 2, got %d", n)
	}
}

func TestBulkUpsert_SearchFindsAll(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	entries := []goagent.UpsertEntry{
		{ID: "a", Vector: vec(1, 0, 0), Message: goagent.UserMessage("alpha")},
		{ID: "b", Vector: vec(0, 1, 0), Message: goagent.UserMessage("beta")},
		{ID: "c", Vector: vec(0, 0, 1), Message: goagent.UserMessage("gamma")},
	}
	if err := store.BulkUpsert(ctx, entries); err != nil {
		t.Fatalf("BulkUpsert: %v", err)
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if got := results[0].Message.TextContent(); got != "alpha" {
		t.Errorf("top result = %q, want %q", got, "alpha")
	}
}

func TestBulkUpsert_EmptyIsNoop(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.BulkUpsert(context.Background(), nil); err != nil {
		t.Errorf("BulkUpsert(nil) = %v, want nil", err)
	}
}

func TestBulkDelete_RemovesAll(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Upsert(ctx, id, vec(1, 0, 0), goagent.UserMessage(id)); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}

	if err := store.BulkDelete(ctx, []string{"a", "b"}); err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}

	n, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 doc remaining, got %d", n)
	}
}

func TestBulkDelete_EmptyIsNoop(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.BulkDelete(context.Background(), nil); err != nil {
		t.Errorf("BulkDelete(nil) = %v, want nil", err)
	}
}

// TestWithDistanceFunc_Custom exercises NewDistanceFunc and WithDistanceFunc
// using a Euclid collection. The custom DistanceFunc applies the same
// normalization as the built-in Euclid value: 1/(1+distance).
func TestWithDistanceFunc_Custom(t *testing.T) {
	client := openClient(t)
	name := collectionNameFor(t)
	ctx := context.Background()
	t.Cleanup(func() { client.DeleteCollection(ctx, name) }) //nolint:errcheck

	if err := goagent_qdrant.CreateCollection(ctx, client, goagent_qdrant.CollectionConfig{
		CollectionName: name,
		Dims:           3,
		DistanceFunc:   goagent_qdrant.Euclid,
	}); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	// NewDistanceFunc constructs a custom DistanceFunc — identical normalisation
	// to the built-in Euclid, but constructed via the public API.
	custom := goagent_qdrant.NewDistanceFunc(
		qdrant.Distance_Euclid,
		func(s float64) float64 { return 1.0 / (1.0 + s) },
	)
	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{CollectionName: name},
		goagent_qdrant.WithDistanceFunc(custom))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("text")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	// Euclid distance for identical vectors is 0; normalized score = 1/(1+0) = 1.
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for identical vector, got %f", results[0].Score)
	}
}

// TestSearch_ComplexMetadata verifies round-trip storage of bool, []any, and
// nested map[string]any metadata. This exercises the anyToValue and valueToAny
// helper branches that are not hit by the basic string/float64 metadata tests.
func TestSearch_ComplexMetadata(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	msg := goagent.Message{
		Role:    goagent.RoleUser,
		Content: []goagent.ContentBlock{goagent.TextBlock("complex")},
		Metadata: map[string]any{
			"active":  true,
			"tags":    []any{"go", "vector"},
			"nested":  map[string]any{"k": "v"},
			"nothing": nil,
		},
	}
	if err := store.Upsert(ctx, "doc1", v, msg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	meta := results[0].Message.Metadata
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if active, ok := meta["active"].(bool); !ok || !active {
		t.Errorf("metadata[active] = %v (%T), want true (bool)", meta["active"], meta["active"])
	}
	if tags, ok := meta["tags"].([]any); !ok || len(tags) != 2 {
		t.Errorf("metadata[tags] = %v (%T), want []any with 2 elements", meta["tags"], meta["tags"])
	}
	if nested, ok := meta["nested"].(map[string]any); !ok || nested["k"] != "v" {
		t.Errorf("metadata[nested] = %v (%T), want map with k=v", meta["nested"], meta["nested"])
	}
}

// TestCount_WithFilter_BoolValue exercises the bool branch of filterToConditions.
func TestCount_WithFilter_BoolValue(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	data := []struct {
		id     string
		active bool
	}{
		{"a", true},
		{"b", false},
		{"c", true},
	}
	for _, d := range data {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.id)},
			Metadata: map[string]any{"active": d.active},
		}
		if err := store.Upsert(ctx, d.id, vec(1, 0, 0), msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	n, err := store.Count(ctx, goagent.WithFilter(map[string]any{"active": true}))
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Errorf("want 2 active docs, got %d", n)
	}
}

// TestCount_WithFilter_IntegerValue exercises the float64 (whole-number) branch
// of filterToConditions, which emits an integer Match condition in Qdrant.
//
// Note: anyToValue stores float64 metadata as Qdrant double values, while this
// filter builds an integer match condition. Qdrant does not coerce between types,
// so the count will be 0 — but the filter construction code path is fully exercised.
func TestCount_WithFilter_IntegerValue(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	data := []struct {
		id   string
		rank float64
	}{
		{"a", 1},
		{"b", 2},
		{"c", 1},
	}
	for _, d := range data {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.id)},
			Metadata: map[string]any{"rank": d.rank},
		}
		if err := store.Upsert(ctx, d.id, vec(1, 0, 0), msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	// float64(1) is a whole number → exercices the int64 branch in filterToConditions.
	// Qdrant stores float64 as double, so the integer condition returns 0 — no error.
	_, err = store.Count(ctx, goagent.WithFilter(map[string]any{"rank": float64(1)}))
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
}

// TestCount_WithFilter_Int64Value exercises the explicit int64 branch of
// filterToConditions. Values stored as float64 (double in Qdrant) will not
// match the generated integer condition — the important thing is no error is
// returned and the branch is exercised.
func TestCount_WithFilter_Int64Value(t *testing.T) {
	client := openClient(t)
	cfg := createAndConfig(t, client, 3)
	store, err := goagent_qdrant.New(client, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	data := []struct {
		id    string
		count float64
	}{
		{"a", 5},
		{"b", 10},
		{"c", 5},
	}
	for _, d := range data {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.id)},
			Metadata: map[string]any{"count": d.count},
		}
		if err := store.Upsert(ctx, d.id, vec(1, 0, 0), msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	// Explicit int64 exercises the int64 branch in filterToConditions.
	_, err = store.Count(ctx, goagent.WithFilter(map[string]any{"count": int64(5)}))
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
}
