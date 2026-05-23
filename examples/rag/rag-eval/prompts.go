package main

// ragasEvaluatorPrompt instructs the LLM to evaluate four quality dimensions.
// The caller can modify this prompt to adjust scoring criteria to their domain.
//
// MaxTokens for this prompt: 120 — enough for a JSON of four floats.
const ragasEvaluatorPrompt = `You are a RAG system quality evaluator.
Given a question, retrieved context, and generated answer, score each dimension
from 0.0 to 1.0.

Scoring criteria:
- faithfulness:      every factual claim in the answer is supported by the context (0=hallucination, 1=fully grounded)
- answer_relevancy:  the answer directly and completely addresses the question (0=off-topic, 1=fully answers)
- context_precision: the context chunks provided are relevant to answering the question (0=all noise, 1=all signal)
- context_recall:    the context contains all information needed to produce a complete answer (0=missing everything, 1=nothing missing)

Output ONLY a valid JSON object with exactly these four keys and float values between 0.0 and 1.0.
Example output (do NOT copy — evaluate the actual inputs above):
{"faithfulness": 0.8, "answer_relevancy": 0.9, "context_precision": 0.7, "context_recall": 1.0}`

// answerSystemPrompt instructs the answer agent to ground its response in the
// retrieved context without hallucinating.
const answerSystemPrompt = `You are a technical documentation assistant for the goagent Go framework.
Answer questions based only on the provided context.
Be concise and precise. If the context does not contain enough information, say so.`

// evalModel is the Ollama model used for answer generation.
// qwen2.5:7b balances quality and speed on consumer hardware (RTX 2060, 6 GB VRAM).
const evalModel = "qwen2.5:7b"

// ragasModel is the Ollama model used as LLM judge in RAGAS evaluation.
// qwen2.5:7b follows structured JSON instructions reliably and scores
// consistently — better signal-to-noise than a reasoning model for this task.
const ragasModel = "qwen2.5:7b"

// rerankerModel is the Ollama model used for LLM reranking.
// qwen2.5:7b has enough capacity to judge relevance on technical Go content;
// smaller models (3B) hurt retrieval metrics instead of improving them.
// Use a small rerankN (≤5) locally: Ollama serializes requests, so the
// LLMReranker's parallel fan-out does not help — fewer calls reduce latency.
const rerankerModel = "qwen2.5:7b"
