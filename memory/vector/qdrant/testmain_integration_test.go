//go:build integration

package qdrant_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestMain starts a Qdrant container, injects QDRANT_TEST_ADDR into the
// environment (host:grpcPort format expected by openClient), runs all tests,
// then terminates the container.
func TestMain(m *testing.M) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "qdrant/qdrant:v1.13.0",
		ExposedPorts: []string{"6333/tcp", "6334/tcp"},
		WaitingFor:   wait.ForHTTP("/healthz").WithPort("6333/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "testcontainers: start qdrant: %v\n", err)
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

	// The qdrant go-client connects via gRPC on port 6334.
	port, err := container.MappedPort(ctx, "6334")
	if err != nil {
		fmt.Fprintf(os.Stderr, "testcontainers: port: %v\n", err)
		os.Exit(1)
	}

	os.Setenv("QDRANT_TEST_ADDR", fmt.Sprintf("%s:%s", host, port.Port())) //nolint:errcheck

	os.Exit(m.Run())
}
