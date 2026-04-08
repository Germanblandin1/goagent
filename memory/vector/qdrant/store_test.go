package qdrant_test

import (
	"context"
	"testing"

	goagent_qdrant "github.com/Germanblandin1/goagent/memory/vector/qdrant"
)

// TestNew_MissingCollectionName and TestCreateCollection_ZeroDimsReturnsError
// do not require a Qdrant instance — they only validate constructor error paths.

func TestNew_MissingCollectionName(t *testing.T) {
	_, err := goagent_qdrant.New(nil, goagent_qdrant.Config{})
	if err == nil {
		t.Fatal("expected error for empty CollectionName, got nil")
	}
}

func TestCreateCollection_ZeroDimsReturnsError(t *testing.T) {
	err := goagent_qdrant.CreateCollection(context.Background(), nil, goagent_qdrant.CollectionConfig{})
	if err == nil {
		t.Fatal("expected error for zero Dims, got nil")
	}
}
