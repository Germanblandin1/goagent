// Package sqlitevec implements goagent.VectorStore over SQLite with the
// sqlite-vec extension (https://github.com/asg017/sqlite-vec).
//
// # Build requirements
//
// This package requires CGO and a C compiler. The sqlite-vec extension needs
// sqlite3.h, which mattn/go-sqlite3 ships as sqlite3-binding.h. A shim header
// at csrc/sqlite3.h bridges the two. Run once from the repository root:
//
//	go env -w CGO_CFLAGS="-I$(pwd)/memory/vector/sqlitevec/csrc -I$(go env GOMODCACHE)/github.com/mattn/go-sqlite3@v1.14.40"
//
// The setting persists in your Go environment file (~/.config/go/env).
// Update the mattn/go-sqlite3 version suffix if that dependency is upgraded.
//
// # Schema
//
// This package uses two tables (created by Migrate or supplied by the caller):
//
//	goagent_embeddings      — regular table: id, content, metadata, created_at
//	goagent_embeddings_vec  — vec0 virtual table: rowid (FK to main table), embedding
//
// The vec0 table provides an indexed KNN search via sqlite-vec's MATCH operator.
//
// # Usage
//
// Call Register (or use Open) before opening any SQLite connection:
//
//	sqlitevec.Register()
//	db, err := sql.Open("sqlite3", "path/to/db.sqlite")
//
// Or use the convenience wrapper:
//
//	db, err := sqlitevec.Open("path/to/db.sqlite")
package sqlitevec
