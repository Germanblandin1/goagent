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

Output ONLY a JSON object. No explanation. No markdown.
{"faithfulness": 0.0, "answer_relevancy": 0.0, "context_precision": 0.0, "context_recall": 0.0}`

// answerSystemPrompt instructs the answer agent to ground its response in the
// retrieved context without hallucinating.
const answerSystemPrompt = `You are a technical documentation assistant for the goagent Go framework.
Answer questions based only on the provided context.
Be concise and precise. If the context does not contain enough information, say so.`

// evalModel is the Claude model used for both answer generation and RAGAS evaluation.
const evalModel = "claude-haiku-4-5-20251001"
