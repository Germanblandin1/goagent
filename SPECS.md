# goagent — Especificaciones para agentes IA

Documento de referencia prescriptivo. Orientado a un agente IA que necesita escribir código correcto para este proyecto sin leer el código fuente completo.

**Complementa a [`ARCHITECTURE.md`](ARCHITECTURE.md)** — ese documento explica *cómo funciona internamente*. Este explica *qué reglas seguir para contribuir*.

---

## 1. Reglas no-negociables

Estas reglas se aplican a **todo** el código del proyecto. Violarlas es un error aunque el código compile.

### 1.1 Firmas de función

1. **`context.Context` es siempre el primer parámetro** en toda operación bloqueante (I/O, llamadas a LLM, acceso a memoria, ejecución de tools). Sin excepciones.
2. **`error` es siempre el último valor de retorno** cuando una función puede fallar.
3. Los parámetros variádicos de opciones (`...Option`) van siempre al final: `func New(required T, opts ...Option)`.

### 1.2 Interfaces

4. **Las interfaces tienen ≤ 3 métodos.** Si necesitas más, divide en interfaces más pequeñas que se composgan.
5. **Las interfaces se definen donde se consumen**, no donde se implementan. El paquete `goagent` define `Provider`, `Tool`, `Embedder` — los paquetes `providers/` los implementan.
6. **Solo exporta interfaces que el caller necesita implementar.** Interfaces puramente internas van en `internal/`.
7. **Las extensiones opcionales van como interfaces separadas**, no en la interfaz principal. El caller hace type assertion en runtime. Ejemplo: `BulkVectorStore` extiende `VectorStore`; `BatchEmbedder` extiende `Embedder`.

### 1.3 Configuración

8. **Functional options** es el único patrón de configuración. Los constructores reciben `...Option`; las opciones son `func(*options)`.
9. **El zero-value de structs de configuración debe ser funcional** o equivalente al default documentado. Ejemplo: `Hooks{}` es un no-op válido.
10. **Los valores por defecto se documentan en el GoDoc de la opción**, no en el nombre ni en el tipo.

### 1.4 Errores

11. **Todos los errores del paquete son tipados** — sentinel (`var ErrX = errors.New(...)`) o struct (`type XError struct{...}`). Nunca `fmt.Errorf("...")` directo en código de biblioteca.
12. **Los errores wrapeables implementan `Unwrap() error`** para compatibilidad con `errors.Is` / `errors.As`.
13. **Los errores se envuelven con contexto usando `%w`**: `fmt.Errorf("operacion: %w", err)`.
14. **Nunca uses `panic`** en código de biblioteca. Si una función no puede continuar, devuelve `error`.

### 1.5 Estado y concurrencia

15. **Cero estado global mutable.** Sin `var` de paquete que se modifiquen en runtime. Sin `init()`.
16. **`Agent` es inmutable después de `New()`**. Todos los campos se escriben una vez; `Run` es seguro para llamadas concurrentes.
17. **Las goroutines deben tener ciclo de vida documentado.** Si lanzas una goroutine, documenta cuándo termina y cómo se cancela.
18. **Respeta `ctx.Done()`** en bucles y operaciones largas. La cancelación debe propagarse.

### 1.6 Testing

19. **Tests con race detector:** `go test -race ./...`. Todo commit debe pasar.
20. **Tests table-driven** como patrón principal. Cada caso tiene nombre descriptivo.
21. **Black-box preferred:** `package foo_test` salvo que necesites acceso a internals.
22. **Sin frameworks externos de mocking.** Implementa las interfaces directamente en `internal/testutil/`.
23. **Cobertura mínima:** ≥ 80 % en paquetes core, ≥ 70 % en sub-paquetes.

### 1.7 Documentación

24. **Todo símbolo exportado tiene GoDoc.** El comentario empieza con el nombre del símbolo.
25. **Sin comentarios que repitan el código.** El comentario explica el *por qué* o el *contrato de comportamiento*, no el *qué* hace el código.
26. **`Example*` functions** para todo constructor y función principal exportada — aparecen en pkg.go.dev.

### 1.8 Imports

27. **Tres grupos, separados por línea en blanco:** stdlib / dependencias externas / paquetes internos del proyecto.

---

## 2. Interfaces — registro completo

### 2.1 Interfaces principales (paquete `goagent`)

#### `Provider`
```go
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
```
Abstracción del backend LLM. `req.Model` nunca está vacío — el agent lo establece antes de llamar.

#### `StreamingProvider` _(opcional — type assertion)_
```go
type StreamingProvider interface {
    CompleteStream(ctx context.Context, req CompletionRequest) (Stream, error)
}
```
El agent detecta soporte en runtime: `if sp, ok := provider.(StreamingProvider); ok { ... }`. No implementarlo no es un error; el agent hace fallback a `Provider.Complete`.

#### `Stream`
```go
type Stream interface {
    Next(ctx context.Context) bool
    Event() StreamEvent
    Err() error
    Close() error
}
```
Iterador de eventos de streaming. Patrón de uso:
```go
for stream.Next(ctx) {
    ev := stream.Event()
}
if err := stream.Err(); err != nil { ... }
```
`Close()` debe llamarse siempre (defer). Seguro llamar múltiples veces.

#### `Tool`
```go
type Tool interface {
    Definition() ToolDefinition
    Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error)
}
```
`Definition()` no recibe ctx — es una constante. `Execute` recibe `args` ya deserializados del JSON del modelo. Los errores de `Execute` se reportan al modelo como texto; no abortan el loop ni los otros tools.

#### `ShortTermMemory`
```go
type ShortTermMemory interface {
    Messages(ctx context.Context) ([]Message, error)
    Append(ctx context.Context, msgs ...Message) error
}
```
Historia de conversación dentro de una sesión. El filtrado (FixedWindow, TokenWindow) ocurre en `Messages`, nunca en `Append`.

#### `LongTermMemory`
```go
type LongTermMemory interface {
    Store(ctx context.Context, msgs ...Message) error
    Retrieve(ctx context.Context, query []ContentBlock, topK int, opts ...SearchOption) ([]ScoredMessage, error)
}
```
Recuperación semántica entre sesiones. `opts` se pasan al `VectorStore.Search` subyacente.

#### `VectorStore`
```go
type VectorStore interface {
    Upsert(ctx context.Context, id string, vector []float32, msg Message) error
    Search(ctx context.Context, vector []float32, topK int, opts ...SearchOption) ([]ScoredMessage, error)
    Delete(ctx context.Context, id string) error
}
```
`Delete` es no-op si el ID no existe. `Search` devuelve score en [0.0, 1.0] para vectores normalizados con cosine similarity.

#### `Embedder`
```go
type Embedder interface {
    Embed(ctx context.Context, content []ContentBlock) ([]float32, error)
}
```
Recibe `[]ContentBlock` completo para soporte multimodal. Devuelve `vector.ErrNoEmbeddeableContent` cuando no hay bloques embeddables.

#### `TransientError` _(clasificación de errores — type assertion)_
```go
type TransientError interface {
    IsTransient() bool
}
```
Los providers pueden implementarla en sus errores para que `RetryProvider` sepa si reintentar.

### 2.2 Interfaces opcionales (extensión por type assertion)

```go
// BulkVectorStore — operaciones batch; más barato que N Upsert individuales
type BulkVectorStore interface {
    VectorStore
    BulkUpsert(ctx context.Context, entries []UpsertEntry) error
    BulkDelete(ctx context.Context, ids []string) error
}

// BatchEmbedder — N embeddings en una sola llamada HTTP
type BatchEmbedder interface {
    Embedder
    BatchEmbed(ctx context.Context, inputs [][]ContentBlock) ([][]float32, error)
}

// CountableStore — count sin query vectorial (health checks, monitoreo)
type CountableStore interface {
    Count(ctx context.Context, opts ...SearchOption) (int64, error)
}
```

**Regla:** el código que las usa siempre hace fallback si la assertion falla:
```go
if bulk, ok := store.(BulkVectorStore); ok {
    return bulk.BulkUpsert(ctx, entries)
}
// fallback: loop de Upsert individuales
```

### 2.3 Interfaces de sub-paquetes

#### `memory/storage.Storage`
```go
type Storage interface {
    Load(ctx context.Context) ([]Message, error)
    Save(ctx context.Context, msgs []Message) error
    Append(ctx context.Context, msgs ...Message) error
}
```

#### `memory/policy.Policy`
```go
type Policy interface {
    Apply(ctx context.Context, msgs []Message) []Message
}
```
Lee el historial, devuelve el subconjunto que verá el provider. Nunca modifica en escritura.

#### `memory/vector.Chunker`
```go
type Chunker interface {
    Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error)
}
```

#### `memory/vector.SizeEstimator`
```go
type SizeEstimator interface {
    Estimate(text string) int
}
```

#### `memory/vector.PDFExtractor`
```go
type PDFExtractor interface {
    ExtractPages(ctx context.Context, data []byte) ([]PDFPage, error)
}
```

#### `orchestration.Executor`
```go
type Executor interface {
    RunWithContext(ctx context.Context, sc *StageContext) error
}
```
Toda primitiva de orquestación implementa `Executor`. Hace composición posible sin type assertions.

#### `orchestration.NodeFunc` _(tipo función, no interface)_
```go
type NodeFunc func(ctx context.Context, sc *StageContext) (next string, err error)
```
Devolver `""` como `next` termina el grafo.

---

## 3. Opciones de configuración

### 3.1 Opciones del Agent (`goagent.Option`)

| Opción | Tipo del valor | Default | Descripción |
|--------|---------------|---------|-------------|
| `WithProvider(p)` | `Provider` | — | **Requerido.** Backend LLM |
| `WithModel(m)` | `string` | `""` | **Requerido.** ID del modelo enviado en `CompletionRequest` |
| `WithTool(t)` | `Tool` | — | Registra un tool (repetible) |
| `WithSystemPrompt(s)` | `string` | `""` | Instrucción de sistema para cada run |
| `WithMaxIterations(n)` | `int` | `10` | Presupuesto de iteraciones del loop ReAct |
| `WithName(name)` | `string` | `""` | Identidad del agente; namespace de sesión para LongTermMemory |
| `WithLogger(l)` | `*slog.Logger` | `slog.Default()` | Logger estructurado |
| `WithShortTermMemory(m)` | `ShortTermMemory` | `nil` | Historia de conversación |
| `WithLongTermMemory(m)` | `LongTermMemory` | `nil` | Recuperación semántica entre sesiones |
| `WithWritePolicy(p)` | `WritePolicy` | `StoreAlways` | Qué persiste en LongTermMemory |
| `WithLongTermTopK(k)` | `int` | `3` | Mensajes a recuperar de LongTermMemory por run |
| `WithShortTermTraceTools(b)` | `bool` | `true` | Incluir traza completa de tools en memoria |
| `WithThinking(budget)` | `int` | — | Extended thinking, budget fijo en tokens |
| `WithAdaptiveThinking()` | — | — | Extended thinking, budget decidido por el modelo |
| `WithEffort(level)` | `string` | `""` | `"high"` / `"medium"` / `"low"` / `""` (default del modelo) |
| `WithToolTimeout(d)` | `time.Duration` | `0` (off) | Deadline por tool; cancela ctx del tool tras `d` |
| `WithCircuitBreaker(n, d)` | `int, time.Duration` | — | Abre el circuito tras `n` fallos consecutivos; resetea tras `d` |
| `WithDispatchMiddleware(mw)` | `DispatchMiddleware` | — | Middleware custom en la cadena de dispatch (repetible) |
| `WithHooks(h)` | `Hooks` | `Hooks{}` | Callbacks de observabilidad |
| `WithRunResult(dst)` | `*RunResult` | `nil` | Destino síncrono de métricas del run |
| `WithMCPConnector(fn)` | `MCPConnectorFn` | — | Conexión MCP de bajo nivel |

### 3.2 Opciones de búsqueda (`SearchOption`)

| Opción | Descripción |
|--------|-------------|
| `WithScoreThreshold(min float64)` | Descarta resultados con score < min |
| `WithFilter(f map[string]any)` | Filtra por metadata (semántica AND) |
| `WithTokenBudget(budget int, est func)` | Tope de tokens en resultados de `Retrieve` |

### 3.3 Opciones de streaming (`StreamOption`)

| Opción | Default | Descripción |
|--------|---------|-------------|
| `WithShowThinkingText(show bool)` | `true` | Reenvía tokens de razonamiento al handler |

### 3.4 Tipos de función reutilizables

```go
// WritePolicy decide qué persiste en LongTermMemory tras cada turn.
// nil → descartar; slice no-nil → almacenar exactamente esos mensajes.
type WritePolicy func(prompt, response Message) []Message

var StoreAlways WritePolicy  // siempre persiste [prompt, response]
func MinLength(n int) WritePolicy  // solo persiste si texto combinado > n chars

// DispatchFunc es la firma base de la cadena de middleware de tools.
type DispatchFunc func(ctx context.Context, name string, args map[string]any) ([]ContentBlock, error)

// DispatchMiddleware envuelve DispatchFunc.
type DispatchMiddleware func(next DispatchFunc) DispatchFunc

// StreamHandler es el callback de RunStream por cada StreamEvent.
type StreamHandler func(event StreamEvent) error

// MCPConnectorFn es la firma de una función de conexión MCP de bajo nivel.
type MCPConnectorFn func(ctx context.Context, logger *slog.Logger) ([]Tool, io.Closer, error)
```

---

## 4. Tipos de error

### 4.1 Errores sentinela

```go
var ErrToolNotFound     = errors.New("tool not found")
var ErrUnsupportedContent = errors.New("unsupported content type")
var ErrInvalidMediaType = errors.New("invalid media type")
```

Uso: `errors.Is(err, goagent.ErrToolNotFound)`.

### 4.2 Errores tipados (struct)

| Tipo | Campos clave | Wrappable | Cuándo |
|------|-------------|-----------|--------|
| `MaxIterationsError` | `Iterations int`, `LastThought string` | No | El loop agotó su presupuesto |
| `ToolExecutionError` | `ToolName string`, `Args map[string]any`, `Cause error` | Sí (`Unwrap`) | Un tool falló; envuelve `CircuitOpenError` cuando el circuito está abierto |
| `CircuitOpenError` | `Tool string`, `OpenUntil time.Time` | No | Tool rechazado por circuit breaker |
| `ToolPanicError` | `ToolName string`, `Value any`, `Stack []byte` | No | Tool entró en panic (recuperado) |
| `ProviderError` | `Provider string`, `Cause error` | Sí (`Unwrap`) | El provider devolvió un error |
| `UnsupportedContentError` | `ContentType`, `Provider string`, `Reason string` | Sí (`Unwrap`) | Content type no soportado por el provider |

Uso:
```go
var toolErr *goagent.ToolExecutionError
if errors.As(err, &toolErr) {
    fmt.Println(toolErr.ToolName)
}
```

### 4.3 Errores de sub-paquetes

| Paquete | Error | Cuándo |
|---------|-------|--------|
| `mcp` | `*MCPConnectionError` | Handshake MCP fallido |
| `mcp` | `*MCPDiscoveryError` | `tools/list` fallido |
| `memory` | `ErrMissingVectorStore` | `NewLongTerm` sin store |
| `memory` | `ErrMissingEmbedder` | `NewLongTerm` sin embedder |
| `memory/vector` | `ErrNoEmbeddeableContent` | `Embed` sin bloques de texto |
| `orchestration` | `PanicError` | Stage de ParallelGroup entró en panic |
| `orchestration` | `MaxRetriesError` | RetryMiddleware agotó intentos |

---

## 5. Patrones de código (recetas)

### 5.1 Implementar un `Tool`

```go
// Opción A: ToolFunc para tools que devuelven texto
calc := goagent.ToolFunc("calculator", "Evalúa expresiones aritméticas",
    goagent.SchemaFrom(struct {
        Op string  `json:"op"  jsonschema_enum:"add,sub,mul,div"`
        A  float64 `json:"a"`
        B  float64 `json:"b"`
    }{}),
    func(ctx context.Context, args map[string]any) (string, error) {
        // args ya están deserializados
        return compute(args), nil
    },
)

// Opción B: ToolBlocksFunc para tools que devuelven ContentBlock (multimodal)
img := goagent.ToolBlocksFunc("screenshot", "Captura la pantalla",
    goagent.SchemaFrom(struct{}{}),
    func(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
        data, err := captureScreen()
        if err != nil {
            return nil, err
        }
        return []goagent.ContentBlock{goagent.ImageBlock(data, "image/png")}, nil
    },
)

// Opción C: struct que implementa Tool directamente
type MyTool struct{ client *http.Client }

func (t *MyTool) Definition() goagent.ToolDefinition {
    return goagent.ToolDefinition{
        Name:        "my_tool",
        Description: "Descripción para el modelo.",
        Parameters:  goagent.SchemaFrom(myParams{}),
    }
}

func (t *MyTool) Execute(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
    // respeta ctx.Done()
    result, err := t.client.Get(ctx, args["url"].(string))
    if err != nil {
        return nil, err  // el agent reporta el error al modelo como texto
    }
    return []goagent.ContentBlock{goagent.TextBlock(result)}, nil
}
```

### 5.2 Implementar un `Provider`

```go
type MyProvider struct {
    client *mySDK.Client
}

func (p *MyProvider) Complete(ctx context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
    // req.Model nunca está vacío
    // req.Tools es nil si no hay tools registrados
    // req.Thinking.Enabled indica si usar extended thinking
    resp, err := p.client.Chat(ctx, toSDKRequest(req))
    if err != nil {
        return goagent.CompletionResponse{}, &goagent.ProviderError{
            Provider: "myprovider",
            Cause:    err,
        }
    }
    return fromSDKResponse(resp), nil
}

// StreamingProvider es opcional — no implementarlo es válido
func (p *MyProvider) CompleteStream(ctx context.Context, req goagent.CompletionRequest) (goagent.Stream, error) {
    stream, err := p.client.ChatStream(ctx, toSDKRequest(req))
    if err != nil {
        return nil, &goagent.ProviderError{Provider: "myprovider", Cause: err}
    }
    return &myStream{inner: stream}, nil
}
```

### 5.3 Implementar un `VectorStore`

```go
type MyStore struct {
    mu   sync.RWMutex
    data map[string]entry
}

func (s *MyStore) Upsert(ctx context.Context, id string, vector []float32, msg goagent.Message) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.data[id] = entry{vector: vector, msg: msg}
    return nil
}

func (s *MyStore) Search(ctx context.Context, vector []float32, topK int, opts ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
    o := goagent.ApplySearchOptions(opts) // usa el helper del paquete si existe, o aplica manualmente
    s.mu.RLock()
    defer s.mu.RUnlock()
    // ... cosine similarity, aplicar o.ScoreThreshold, o.Filter, limitar a topK
    return results, nil
}

func (s *MyStore) Delete(ctx context.Context, id string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.data, id) // no-op si no existe
    return nil
}

// Extensión opcional: BulkVectorStore
func (s *MyStore) BulkUpsert(ctx context.Context, entries []goagent.UpsertEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    for _, e := range entries {
        s.data[e.ID] = entry{vector: e.Vector, msg: e.Message}
    }
    return nil
}
```

### 5.4 Agregar un `DispatchMiddleware`

```go
// La cadena (outermost → innermost): logging → timeout → circuit breaker → custom → Execute
func metricsMiddleware(next goagent.DispatchFunc) goagent.DispatchFunc {
    return func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
        start := time.Now()
        result, err := next(ctx, name, args)
        recordToolMetric(name, time.Since(start), err)
        return result, err
    }
}

agent, _ := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithDispatchMiddleware(metricsMiddleware),
)
```

### 5.5 Implementar un `WritePolicy`

```go
// Solo persiste turns que mencionan al usuario por nombre
func onlyPersonalized(prompt, response goagent.Message) []goagent.Message {
    if !strings.Contains(goagent.TextFrom(prompt.Content), "German") {
        return nil // descarta
    }
    return []goagent.Message{prompt, response}
}

agent, _ := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithLongTermMemory(ltm),
    goagent.WithWritePolicy(onlyPersonalized),
)
```

### 5.6 Extender con interface opcional

Patrón estándar para no romper implementaciones existentes:

```go
// Al llamar:
if bulk, ok := store.(goagent.BulkVectorStore); ok {
    if err := bulk.BulkUpsert(ctx, entries); err != nil {
        return err
    }
} else {
    for _, e := range entries {
        if err := store.Upsert(ctx, e.ID, e.Vector, e.Message); err != nil {
            return err
        }
    }
}
```

### 5.7 Implementar un `NodeFunc` en orchestration

```go
graph, err := orchestration.NewGraph(
    orchestration.WithStart("generate"),
    orchestration.WithNode("generate", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
        output, err := coderAgent.Run(ctx, sc.Goal)
        if err != nil {
            return "", err
        }
        sc.SetOutput("code", output)
        return "review", nil         // nombre del siguiente nodo
    }, orchestration.WithToNodes("review")),
    orchestration.WithNode("review", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
        // ...
        if approved {
            return "", nil           // "" termina el grafo
        }
        return "generate", nil
    }, orchestration.WithToNodes("generate", "")),
)
```

---

## 6. Anti-patrones

### ❌ No hagas esto

```go
// 1. Estado global mutable
var globalProvider Provider  // ❌

// 2. panic en código de biblioteca  
func mustLoad(path string) []byte {
    data, err := os.ReadFile(path)
    if err != nil {
        panic(err)  // ❌ devuelve error
    }
    return data
}

// 3. Interface con demasiados métodos
type Memory interface {
    Load(ctx) error
    Save(ctx) error
    Append(ctx) error
    Clear(ctx) error
    Export(ctx) error  // ❌ divide en interfaces más pequeñas
}

// 4. Contexto ignorado en operaciones bloqueantes
func (p *MyProvider) Complete(req CompletionRequest) (CompletionResponse, error) {  // ❌ falta ctx
    return callHTTP(req)
}

// 5. Error sin tipo en código de biblioteca
return fmt.Errorf("tool failed: %s", name)  // ❌ usa ToolExecutionError

// 6. Modificar args antes de pasarlos downstream
func (mw myMiddleware) Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
    args["injected"] = "value"  // ❌ args es del modelo; no lo mutes
    return mw.next.Execute(ctx, args)
}

// 7. Constructor sin functional options
func NewTool(name, desc string, timeout int) *MyTool {  // ❌
    return &MyTool{name: name, desc: desc, timeout: timeout}
}
// ✅ correcto:
func NewTool(name, desc string, opts ...ToolOption) *MyTool { ... }

// 8. Usar RoleDocument en mensajes que van al provider
messages = append(messages, Message{Role: RoleDocument, ...})  // ❌ RoleDocument solo vive en VectorStore
provider.Complete(ctx, CompletionRequest{Messages: messages})   // el provider devuelve error

// 9. Compartir *RunResult entre runs concurrentes
var result goagent.RunResult
go agent.Run(ctx1, "query1")  // ❌ data race
go agent.Run(ctx2, "query2")
// WithRunResult apunta al mismo *result

// 10. Silenciar errores de memoria
_ = mem.Append(ctx, msgs...)  // ❌ loguea o devuelve el error
```

---

## Apéndice — Tipos clave de datos

```go
// Mensaje en el historial de conversación
type Message struct {
    Role       Role           // RoleUser, RoleAssistant, RoleTool, RoleSystem, RoleDocument
    Content    []ContentBlock // texto, imagen o documento
    ToolCalls  []ToolCall     // solo en mensajes assistant con tool_use
    ToolCallID string         // solo en mensajes RoleTool — enlaza resultado con request
    Metadata   map[string]any // opcional; usado por chunks de RAG (source, chunk_index)
}

// Bloque de contenido (union type)
// Constructores: TextBlock(s), ImageBlock(data, mimeType), DocumentBlock(data, mimeType)
type ContentBlock struct { /* opaco */ }

// Resultado con score de similitud
type ScoredMessage struct {
    Message Message
    Score   float64  // [0.0, 1.0] para cosine similarity con vectores normalizados
}

// Definición de tool (enviada al modelo)
type ToolDefinition struct {
    Name        string
    Description string
    Parameters  map[string]any  // JSON Schema válido
}

// Métricas de un run completo
type RunResult struct {
    Duration   time.Duration
    Iterations int
    TotalUsage Usage
    ToolCalls  int
    ToolTime   time.Duration
    Err        error
}

// Roles válidos
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
    RoleSystem    Role = "system"
    RoleDocument  Role = "document"  // solo en VectorStore — nunca al provider
)
```

---

> **Relación con otros documentos:**
> - [`ARCHITECTURE.md`](ARCHITECTURE.md) — cómo funciona el ReAct loop, el dispatcher, la memoria y la orquestación internamente
> - [`CLAUDE.md`](CLAUDE.md) — instrucciones específicas para Claude Code en este repo
> - [`README.md`](README.md) — guía de uso para consumidores del framework
