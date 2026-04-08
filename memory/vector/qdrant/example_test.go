package qdrant_test

import (
	"context"
	"fmt"
	"log"

	"github.com/qdrant/go-client/qdrant"

	goagent_qdrant "github.com/Germanblandin1/goagent/memory/vector/qdrant"
)

func ExampleNew() {
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: "localhost",
		Port: 6334,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
		CollectionName: "embeddings",
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = store
	fmt.Println("store created")
}

func ExampleCreateCollection() {
	ctx := context.Background()
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: "localhost",
		Port: 6334,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	cfg := goagent_qdrant.CollectionConfig{
		CollectionName: "goagent_embeddings",
		Dims:           1536, // match your embedding model (e.g. text-embedding-3-small)
	}
	if err := goagent_qdrant.CreateCollection(ctx, client, cfg); err != nil {
		log.Fatal(err)
	}

	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
		CollectionName: cfg.CollectionName,
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = store
	fmt.Println("store ready")
}
