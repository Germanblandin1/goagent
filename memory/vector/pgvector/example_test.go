package pgvector_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Germanblandin1/goagent/memory/vector/pgvector"
)

func ExampleNew() {
	db, err := sql.Open("pgx", "postgres://user:pass@localhost/mydb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := pgvector.New(db, pgvector.TableConfig{
		Table:          "embeddings",
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata", // optional; omit if your table has no metadata column
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = store
	fmt.Println("store created")
}

func ExampleMigrate() {
	ctx := context.Background()
	db, err := sql.Open("pgx", "postgres://user:pass@localhost/mydb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	cfg := pgvector.MigrateConfig{
		TableName: "goagent_embeddings",
		Dims:      1536, // match your embedding model (e.g. text-embedding-3-small)
	}
	if err := pgvector.Migrate(ctx, db, cfg); err != nil {
		log.Fatal(err)
	}

	store, err := pgvector.New(db, pgvector.TableConfig{
		Table:          cfg.TableName,
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata",
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = store
	fmt.Println("store ready")
}
