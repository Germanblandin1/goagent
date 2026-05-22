// Calculator is a minimal goagent example that exposes a calculator tool and
// asks the model to solve a math problem. Requires Ollama running locally with
// the qwen3 model: https://ollama.com
//
// Usage:
//
//	go run ./examples/calculator
package main

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	calc := goagent.ToolFunc(
		"calculator",
		"Performs basic arithmetic. Supported operations: add, sub, mul, div.",
		goagent.SchemaFrom(struct {
			Operation string  `json:"operation" jsonschema_enum:"add,sub,mul,div" jsonschema_description:"The arithmetic operation to perform."`
			A         float64 `json:"a" jsonschema_description:"First operand."`
			B         float64 `json:"b" jsonschema_description:"Second operand."`
		}{}),
		func(_ context.Context, args map[string]any) (string, error) {
			op, _ := args["operation"].(string)
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)

			var result float64
			switch op {
			case "add":
				result = a + b
			case "sub":
				result = a - b
			case "mul":
				result = a * b
			case "div":
				if b == 0 {
					return "", fmt.Errorf("division by zero")
				}
				result = a / b
			default:
				return "", fmt.Errorf("unknown operation: %s", op)
			}
			return strconv.FormatFloat(result, 'f', -1, 64), nil
		},
	)

	agent, err := goagent.New(
		goagent.WithProvider(ollama.New()),
		goagent.WithModel("gpt-oss:120b-cloud"),
		goagent.WithTool(calc),
		goagent.WithSystemPrompt("You are a helpful assistant. Use the calculator tool when asked to compute numbers."),
	)
	if err != nil {
		log.Fatal(err)
	}

	answer, err := agent.Run(context.Background(), "What is (123 * 456) + 789?")
	if err != nil {
		log.Fatalf("agent error: %v", err)
	}
	fmt.Println("Answer:", answer)
}
