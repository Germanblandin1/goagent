// Package pgvector implements goagent.VectorStore over PostgreSQL with the
// pgvector extension.
//
// The caller describes their existing table via TableConfig — this package does
// not impose any schema. For a quick start without an existing table, use Migrate.
//
// If you need filtering by custom columns or JOINs with other tables,
// implement goagent.VectorStore directly or use a PostgreSQL view:
//
//	CREATE VIEW my_filtered_docs AS
//	    SELECT * FROM my_table WHERE category = 'technical';
//
// and pass that view as TableConfig.Table.
package pgvector
