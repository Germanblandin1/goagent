# goagent/ratelimit

Token-bucket rate limiters for [goagent](https://github.com/Germanblandin1/goagent) tool dispatch.

Wraps `golang.org/x/time/rate` and plugs into the agent via `WithDispatchMiddleware`. Provides a global limiter and a per-tool limiter map.

```bash
go get github.com/Germanblandin1/goagent/ratelimit
```

## Documentation

- [pkg.go.dev/github.com/Germanblandin1/goagent/ratelimit](https://pkg.go.dev/github.com/Germanblandin1/goagent/ratelimit)
- [Root module](https://pkg.go.dev/github.com/Germanblandin1/goagent)
