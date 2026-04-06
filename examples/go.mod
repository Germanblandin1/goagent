module github.com/Germanblandin1/goagent/examples

go 1.25.0

require (
	github.com/Germanblandin1/goagent v0.0.0
	github.com/Germanblandin1/goagent/mcp v0.0.0
	github.com/Germanblandin1/goagent/providers/ollama v0.0.0
	github.com/Germanblandin1/goagent/rag v0.0.0
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
)

require (
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mark3labs/mcp-go v0.46.0 // indirect
	github.com/sashabaranov/go-openai v1.41.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
)

replace github.com/Germanblandin1/goagent => ..

replace github.com/Germanblandin1/goagent/mcp => ../mcp

replace github.com/Germanblandin1/goagent/providers/anthropic => ../providers/anthropic

replace github.com/Germanblandin1/goagent/providers/ollama => ../providers/ollama

replace github.com/Germanblandin1/goagent/rag => ../rag
