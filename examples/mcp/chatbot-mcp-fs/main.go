// Chatbot-mcp-fs is a goagent example that runs an interactive chatbot with
// access to the local filesystem via two MCP tools: list_dir and read_file.
//
// The agent can browse the directory where it is started and any
// subdirectories — it cannot access paths above the working directory.
// All user-supplied paths are sanitised before reaching the OS.
//
// # Architecture
//
// The binary is self-contained: the same executable serves as both the
// chatbot agent and the MCP filesystem server, depending on the --serve flag.
// When the agent starts, it spawns itself as a subprocess via mcp.WithStdio
// with --serve pointing to the working directory. The subprocess exposes the
// filesystem tools over stdin/stdout using the MCP stdio transport.
//
// Usage:
//
//	# Build first (needed for the self-spawn pattern to work with go run):
//	go build -o chatbot-mcp-fs ./examples/chatbot-mcp-fs
//	./chatbot-mcp-fs
//
//	# Or with go run (compiles to a temp binary that can spawn itself):
//	go run ./examples/chatbot-mcp-fs
//
// Requires Ollama running locally with the qwen3 model: https://ollama.com
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/mcp"
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/policy"
	"github.com/Germanblandin1/goagent/memory/storage"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	// --serve=<root> is an internal flag used when this binary is spawned as
	// the MCP filesystem server by the agent. End users never pass it directly.
	serveRoot := flag.String("serve", "", "internal: run as MCP server rooted at this path")
	flag.Parse()

	if *serveRoot != "" {
		if err := runServer(*serveRoot); err != nil {
			fmt.Fprintln(os.Stderr, "mcp server error:", err)
			os.Exit(1)
		}
		return
	}

	// Normal mode: interactive chatbot.
	root, err := os.Getwd()
	if err != nil {
		log.Fatal("cannot determine working directory:", err)
	}

	// Resolve the path to the current executable so the MCP subprocess can be
	// started reliably, whether the program was launched via `go run` or as a
	// compiled binary.
	exe, err := os.Executable()
	if err != nil {
		log.Fatal("cannot resolve executable path:", err)
	}

	runChatbot(root, exe)
}

func runChatbot(root, exe string) {
	mem := memory.NewShortTerm(
		memory.WithStorage(storage.NewInMemory()),
		memory.WithPolicy(policy.NewFixedWindow(20)),
	)

	agent, err := goagent.New(
		goagent.WithProvider(ollama.New()),
		goagent.WithModel("gpt-oss:120b-cloud"),
		goagent.WithShortTermMemory(mem),
		goagent.WithMaxIterations(10),
		goagent.WithSystemPrompt(fmt.Sprintf(
			"You are a helpful assistant with read-only access to the local filesystem.\n"+
				"The working directory is: %q\n"+
				"You can list directories and read files anywhere inside it.\n"+
				"You cannot access paths outside this directory.\n"+
				"When answering questions about files, always check the content first.",
			root,
		)),
		// Spawn this same binary as the MCP filesystem server.
		mcp.WithStdio(exe, "--serve="+root),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("Filesystem chatbot ready.\n")
	fmt.Printf("Root: %s\n", root)
	fmt.Printf("Type your message, or \"exit\" to quit.\n\n")

	for {
		fmt.Print("You: ")

		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			break
		}

		reply, err := agent.Run(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("error: %v\n", err)
			continue
		}

		fmt.Printf("Bot: %s\n\n", reply)
	}

	fmt.Println("Goodbye!")
}
