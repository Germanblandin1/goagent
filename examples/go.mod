module github.com/Germanblandin1/goagent/examples

go 1.26.2

require (
	github.com/Germanblandin1/goagent v0.0.0
	github.com/Germanblandin1/goagent/mcp v0.0.0
	github.com/Germanblandin1/goagent/memory/vector/qdrant v0.0.0
	github.com/Germanblandin1/goagent/memory/vector/sqlitevec v0.0.0
	github.com/Germanblandin1/goagent/providers/ollama v0.0.0
	github.com/Germanblandin1/goagent/rag v0.0.0
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
	github.com/qdrant/go-client v1.17.1
)

require (
	github.com/asg017/sqlite-vec-go-bindings v0.1.6 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mark3labs/mcp-go v0.46.0 // indirect
	github.com/mattn/go-sqlite3 v1.14.40 // indirect
	github.com/sashabaranov/go-openai v1.41.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260209200024-4cfbd4190f57 // indirect
	google.golang.org/grpc v1.78.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/Germanblandin1/goagent => ..

replace github.com/Germanblandin1/goagent/mcp => ../mcp

replace github.com/Germanblandin1/goagent/providers/anthropic => ../providers/anthropic

replace github.com/Germanblandin1/goagent/providers/ollama => ../providers/ollama

replace github.com/Germanblandin1/goagent/memory/vector/qdrant => ../memory/vector/qdrant

replace github.com/Germanblandin1/goagent/memory/vector/sqlitevec => ../memory/vector/sqlitevec

replace github.com/Germanblandin1/goagent/rag => ../rag
