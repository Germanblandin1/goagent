//go:build integration

package pgvector_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestMain starts a PostgreSQL+pgvector container, injects PGVECTOR_TEST_DSN
// into the environment, runs all tests, then terminates the container.
func TestMain(m *testing.M) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "testcontainers: start pgvector: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if termErr := container.Terminate(ctx); termErr != nil {
			fmt.Fprintf(os.Stderr, "testcontainers: terminate: %v\n", termErr)
		}
	}()

	host, err := container.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testcontainers: host: %v\n", err)
		os.Exit(1)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		fmt.Fprintf(os.Stderr, "testcontainers: port: %v\n", err)
		os.Exit(1)
	}

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/testdb?sslmode=disable",
		host, port.Port())
	os.Setenv("PGVECTOR_TEST_DSN", dsn) //nolint:errcheck

	os.Exit(m.Run())
}
