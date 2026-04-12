# Dockerfile — PostgreSQL + pgvector for integration tests.
#
# Build:
#   docker build -t goagent-pgvector-test .
#
# Run (foreground):
#   docker run --rm -e POSTGRES_PASSWORD=postgres -p 5432:5432 goagent-pgvector-test
#
# Run (background):
#   docker run --rm -d -e POSTGRES_PASSWORD=postgres -p 5432:5432 --name pgvector-test goagent-pgvector-test
#
# Stop (background):
#   docker stop pgvector-test
#
# Connection string (after container starts):
#   postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
#
# Ports:
#   5432 — PostgreSQL (pgvector extension is pre-installed and auto-loaded)
FROM pgvector/pgvector:pg17
