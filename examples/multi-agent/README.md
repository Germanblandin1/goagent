# multi-agent — Pipeline supervisor/worker con PromptBuilder

Demuestra cómo usar un Pipeline de dos stages donde el segundo stage es un
Supervisor que lee el output del primero via PromptBuilder.

## Flujo

```
goal → [planner] → plan → [supervisor] → resultado
                              ├── researcher
                              ├── coder
                              └── reviewer
```

## Prerequisitos

Tener [Ollama](https://ollama.com) corriendo localmente y el modelo descargado:

```bash
ollama pull qwen3:latest
```

## Uso

Sin argumento usa el goal por defecto:

```bash
go run ./examples/multi-agent
```

Con un goal personalizado:

```bash
go run ./examples/multi-agent "Implement a Redis client in Go"
go run ./examples/multi-agent "Build a concurrent worker pool in Go"
```

## Lo que demuestra

- `orchestration.NewPipeline` con dos stages secuenciales
- `orchestration.AgentStage` para el planificador (stage 1)
- `orchestration.NewSupervisor` con `PromptBuilder` custom que inyecta
  el output del stage anterior en el input del supervisor
- `orchestration.Worker` con `InputDescription` específico por worker
- El supervisor LLM decide en runtime qué workers llamar y en qué orden
- Todos los agentes usan el provider de Ollama con `qwen3:latest`
