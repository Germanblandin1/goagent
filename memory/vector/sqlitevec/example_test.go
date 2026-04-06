package sqlitevec_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent/memory/vector/sqlitevec"
)

func ExampleNew() {
	db, err := sqlitevec.Open("/path/to/mydb.sqlite")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
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
	db, err := sqlitevec.Open("/path/to/mydb.sqlite")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	cfg := sqlitevec.MigrateConfig{
		TableName: "goagent_embeddings",
		Dims:      1536, // match your embedding model (e.g. text-embedding-3-small)
	}
	if err := sqlitevec.Migrate(ctx, db, cfg); err != nil {
		log.Fatal(err)
	}

	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
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
