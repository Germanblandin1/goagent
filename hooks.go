package goagent

import "time"

// Hooks permite observar eventos del loop ReAct sin modificar su comportamiento.
// Todos los campos son opcionales — un hook nil se ignora silenciosamente.
//
// Los hooks se invocan sincrónicamente dentro del loop. Si un hook necesita
// hacer trabajo pesado (ej: enviar a un servicio externo), debe lanzar una
// goroutine internamente para no bloquear el loop.
//
// El zero value de Hooks es funcional y no invoca ningún callback.
//
// Ejemplo:
//
//	agent := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithHooks(goagent.Hooks{
//	        OnToolCall: func(name string, args map[string]any) {
//	            fmt.Printf("🔧 %s\n", name)
//	        },
//	    }),
//	)
type Hooks struct {
	// OnIterationStart se invoca al inicio de cada iteración del loop ReAct,
	// antes de llamar al provider.
	// iteration es 0-indexed: la primera iteración es 0.
	OnIterationStart func(iteration int)

	// OnThinking se invoca cuando el modelo produce un bloque de thinking.
	// text es el contenido del razonamiento — puede ser un resumen en Claude 4+
	// o el razonamiento completo en modelos locales y Claude Sonnet 3.7.
	//
	// Se invoca una vez por cada thinking block en la respuesta del modelo.
	// Si la respuesta tiene múltiples thinking blocks (interleaved thinking),
	// se invoca una vez por cada uno, en orden.
	//
	// Solo se invoca si el agente tiene thinking habilitado (WithThinking,
	// WithAdaptiveThinking) o si el modelo local produce thinking.
	OnThinking func(text string)

	// OnToolCall se invoca cuando el modelo solicita ejecutar una herramienta,
	// antes de que el dispatcher la ejecute.
	// Se invoca una vez por cada tool call en la respuesta del modelo.
	// Si el modelo pide N tools en paralelo, se invoca N veces antes del dispatch.
	OnToolCall func(name string, args map[string]any)

	// OnToolResult se invoca después de que una herramienta termina de ejecutarse.
	// content es el resultado que se devolverá al modelo.
	// duration es el tiempo que tardó la ejecución.
	// err es nil si la tool ejecutó exitosamente, o el error si falló.
	//
	// Se invoca incluso cuando la tool falla — err contiene el error.
	// Se invoca una vez por cada tool call, después de que todas terminan.
	OnToolResult func(name string, content []ContentBlock, duration time.Duration, err error)

	// OnResponse se invoca cuando el modelo produce la respuesta final,
	// justo antes de que Run/RunBlocks retorne al caller.
	// text es la respuesta textual extraída (sin thinking blocks).
	// iterations es la cantidad total de iteraciones que usó el loop (1-indexed).
	//
	// También se invoca cuando el loop se agota (MaxIterationsError) —
	// text puede ser "" si la última iteración terminó en tool use.
	OnResponse func(text string, iterations int)
}
