// Multimodal Chatbot is a goagent example that combines short-term memory,
// long-term vector memory, and a filesystem tool to let a local Ollama model
// load and analyze images from disk.
//
// Requirements:
//   - Ollama running locally with qwen3.5:cloud and nomic-embed-text:latest
//
// Usage:
//
//	go run ./examples/multimodal-chatbot
//
// Then ask the agent to analyze an image, e.g.:
//
//	You: analyze the image at C:/Users/me/photos/cat.jpg
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/policy"
	"github.com/Germanblandin1/goagent/memory/storage"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// ANSI color codes for the step logger.
const (
	colorReset  = "\033[0m"
	colorGray   = "\033[90m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorBlue   = "\033[34m"
)

// logStep prints a timestamped, colored diagnostic line to stderr.
func logStep(color, tag, msg string) {
	fmt.Fprintf(os.Stderr, "%s[%s] %-10s %s%s\n",
		color, time.Now().Format("15:04:05.000"), tag, msg, colorReset)
}

func main() {
	// --- Shared Ollama HTTP client ---
	// Both the chat provider and the embedder reuse the same underlying
	// connection pool instead of opening two independent ones.
	client := ollama.NewClient()

	// --- Short-term memory: sliding window of the last 20 messages ---
	shortMem := memory.NewShortTerm(
		memory.WithStorage(storage.NewInMemory()),
		memory.WithPolicy(policy.NewFixedWindow(20)),
	)
	logStep(colorGray, "SETUP", "short-term: InMemory + FixedWindow(20)")

	// --- Long-term memory: in-process vector store backed by nomic-embed-text ---
	// MinLength(10) prevents storing very short or empty exchanges that would
	// pollute the vector index without adding retrieval value.
	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel("nomic-embed-text:latest"),
	)
	longMem, err := memory.NewLongTerm(
		memory.WithEmbedder(embedder),
		memory.WithVectorStore(vector.NewInMemoryStore()),
		memory.WithTopK(3),
	)
	if err != nil {
		log.Fatal(err)
	}
	logStep(colorGray, "SETUP", "long-term: InMemoryStore + nomic-embed-text:latest, topK=3")

	// --- Agent ---
	agent, err := goagent.New(
		goagent.WithProvider(ollama.NewWithClient(client)),
		goagent.WithModel("qwen3.5:cloud"),
		goagent.WithName("multimodal-chatbot"),
		goagent.WithTool(NewScanDirTool()),
		goagent.WithTool(NewLoadFileTool()),
		goagent.WithShortTermMemory(shortMem),
		goagent.WithLongTermMemory(longMem),
		goagent.WithWritePolicy(goagent.MinLength(10)),
		goagent.WithSystemPrompt(
			"You are a helpful multimodal assistant running locally via Ollama.\n"+
				"You have two filesystem tools:\n"+
				"  - scan_dir: list images and documents in a directory (use this to explore)\n"+
				"  - load_file: load a specific image or document for analysis\n"+
				"Supported images: jpg, png, gif, webp.\n"+
				"Supported documents: pdf, txt.\n"+
				"When the user asks to find or list files, use scan_dir first.\n"+
				"When the user asks to analyze a file, use load_file.\n"+
				"Describe images in detail: objects, colors, layout, any visible text.\n"+
				"Summarize documents concisely, highlighting key points.\n"+
				"Be concise for conversational questions, detailed for file analysis.",
		),
		goagent.WithHooks(goagent.Hooks{
			OnIterationStart: func(i int) {
				logStep(colorBlue, "LOOP", fmt.Sprintf("iteration %d", i+1))
			},
			OnThinking: func(text string) {
				preview := text
				if len(preview) > 100 {
					preview = preview[:100] + "…"
				}
				logStep(colorGray, "THINKING", preview)
			},
			OnToolCall: func(name string, args map[string]any) {
				b, _ := json.Marshal(args)
				logStep(colorYellow, "TOOL→", fmt.Sprintf("%s(%s)", name, b))
			},
			OnToolResult: func(name string, content []goagent.ContentBlock, d time.Duration, err error) {
				if err != nil {
					logStep(colorRed, "TOOL←", fmt.Sprintf("%s ERR [%s]: %v", name, d.Round(time.Millisecond), err))
					return
				}
				var textBytes, images int
				var preview string
				for _, b := range content {
					switch b.Type {
					case goagent.ContentText:
						textBytes += len(b.Text)
						if preview == "" {
							preview = b.Text
							if len(preview) > 60 {
								preview = preview[:60]
							}
						}
					case goagent.ContentImage:
						images++
					}
				}
				logStep(colorGreen, "TOOL←", fmt.Sprintf(
					"%s OK [%s] text=%dB images=%d %q",
					name, d.Round(time.Millisecond), textBytes, images, preview,
				))
			},
			OnShortTermLoad: func(results int, d time.Duration, err error) {
				if err != nil {
					logStep(colorRed, "STM←", fmt.Sprintf("load ERR [%s]: %v", d.Round(time.Millisecond), err))
					return
				}
				logStep(colorGray, "STM←", fmt.Sprintf("load OK [%s] msgs=%d", d.Round(time.Millisecond), results))
			},
			OnShortTermAppend: func(msgs int, d time.Duration, err error) {
				if err != nil {
					logStep(colorRed, "STM→", fmt.Sprintf("append ERR [%s]: %v", d.Round(time.Millisecond), err))
					return
				}
				logStep(colorGray, "STM→", fmt.Sprintf("append OK [%s] msgs=%d", d.Round(time.Millisecond), msgs))
			},
			OnLongTermRetrieve: func(results int, d time.Duration, err error) {
				if err != nil {
					logStep(colorRed, "LTM←", fmt.Sprintf("retrieve ERR [%s]: %v", d.Round(time.Millisecond), err))
					return
				}
				logStep(colorGray, "LTM←", fmt.Sprintf("retrieve OK [%s] results=%d", d.Round(time.Millisecond), results))
			},
			OnLongTermStore: func(msgs int, d time.Duration, err error) {
				if err != nil {
					logStep(colorRed, "LTM→", fmt.Sprintf("store ERR [%s]: %v", d.Round(time.Millisecond), err))
					return
				}
				logStep(colorGray, "LTM→", fmt.Sprintf("store OK [%s] msgs=%d", d.Round(time.Millisecond), msgs))
			},
			OnResponse: func(text string, iterations int) {
				logStep(colorCyan, "DONE", fmt.Sprintf("iterations=%d len=%d", iterations, len(text)))
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("\nMultimodal Chatbot")
	fmt.Println("  chat model  : qwen3.5:cloud (Ollama)")
	fmt.Println("  embed model : nomic-embed-text:latest")
	fmt.Println("  memory      : short-term (FixedWindow 20) + long-term (vector, topK=3)")
	fmt.Println()
	fmt.Println("Tip: 'analyze the image at /path/to/photo.jpg'")
	fmt.Println("     'summarize the document at /path/to/report.pdf'")
	fmt.Println("     'what files are in C:/Users/me/docs?'")
	fmt.Println("Type 'exit' to quit.")
	fmt.Println()

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

		fmt.Println()
		reply, err := agent.Run(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Fprintf(os.Stderr, colorRed+"error: %v"+colorReset+"\n", err)
			fmt.Println()
			continue
		}

		fmt.Printf("\nBot: %s\n\n", reply)
	}

	fmt.Println("Goodbye!")
}
