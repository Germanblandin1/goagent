# goagent/providers/openai

OpenAI provider for [goagent](https://github.com/Germanblandin1/goagent).

Implements `goagent.Provider` and `goagent.StreamingProvider` over the OpenAI Chat Completions API. Works with the official API or any OpenAI-compatible endpoint (a proxy, Azure OpenAI, or a local server) via `WithBaseURL`.

```bash
go get github.com/Germanblandin1/goagent/providers/openai
```

Reads the API key from `OPENAI_API_KEY` by default, or set it explicitly with `WithAPIKey`. The model is selected at the agent level with `goagent.WithModel`.

```go
agent, _ := goagent.New(
    goagent.WithProvider(openai.New()),
    goagent.WithModel("gpt-4o-mini"),
)
```

Supports text and images; reasoning effort maps to `reasoning_effort` for o-series models. Document content is not supported, and the API does not expose reasoning in the response.

## Documentation

- [pkg.go.dev/github.com/Germanblandin1/goagent/providers/openai](https://pkg.go.dev/github.com/Germanblandin1/goagent/providers/openai)
- [Root module](https://pkg.go.dev/github.com/Germanblandin1/goagent)
