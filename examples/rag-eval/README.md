# rag-eval — RAG Pipeline Evaluation

Measures RAG pipeline quality across two independent failure modes:

```
Query → [Retrieval] → [Generation] → Response

Failure 1: retrieval missed the relevant docs
           → the LLM never had the context to answer

Failure 2: retrieval found the docs
           → but the LLM ignored them or hallucinated
```

Without metrics you can't tell which one is failing. This example measures both.

## What each metric measures

### Layer 1 — Retrieval (Go-only, no LLM)

| Metric | Question answered |
|--------|------------------|
| **Precision@3** | Of the top-3 documents I retrieved, what fraction are actually relevant? |
| **Recall@3** | Of all relevant documents, how many did I find in the top-3? |
| **MRR** | Does the most useful document appear near the top of the ranking? |

High Precision → low noise in the context sent to the LLM.  
High Recall → important information is not missing.  
High MRR → the best document is ranked first.

### Layer 2 — Generation (RAGAS, LLM as judge)

| Metric | Question answered |
|--------|------------------|
| **Faithfulness** | Every claim in the answer is grounded in the retrieved context (0 = hallucination, 1 = fully grounded). |
| **Answer Relevancy** | The answer directly and completely addresses the question (0 = off-topic, 1 = fully answers). |
| **Context Precision** | The retrieved chunks are relevant to answering the question (0 = all noise, 1 = all signal). |
| **Context Recall** | The retrieved context contains all the information needed for a complete answer. |

## How to interpret the comparative report

The example runs two modes on the same dataset:

- **Mode A**: basic RAG pipeline (vector similarity only)
- **Mode B**: RAG + LLMReranker (over-fetches 10 candidates, re-scores, returns top 3)

A `+` delta in every metric confirms that the reranker improved both retrieval
precision and generation quality. A flat or negative delta means the reranker is
hurting — investigate query/document alignment or reranker model choice.

## Prerequisites

```bash
# Embedding model (local Ollama)
ollama pull nomic-embed-text

# Anthropic API key (for answer generation and RAGAS evaluation)
export ANTHROPIC_API_KEY=sk-ant-...
```

## Run

```bash
go run ./examples/rag-eval
```

## Expected output shape

```
=== Mode A: basic RAG (no reranker) ===
  Retrieval:
    Precision@3:       0.55
    Recall@3:          0.58
    MRR:               0.70
  Generation (RAGAS):
    Faithfulness:      0.68
    Answer Relevancy:  0.72
    Context Precision: 0.60
    Context Recall:    0.62

=== Mode B: RAG with LLMReranker (rerankN=10, topK=3) ===
  Retrieval:
    Precision@3:       0.78
    Recall@3:          0.74
    MRR:               0.88
  Generation (RAGAS):
    Faithfulness:      0.87
    Answer Relevancy:  0.84
    Context Precision: 0.80
    Context Recall:    0.79

=== Comparison A vs B (+ = improvement with reranker) ===
  Precision@3:       +0.23
  Recall@3:          +0.16
  MRR:               +0.18
  Faithfulness:      +0.19
  Answer Relevancy:  +0.12
  Context Precision: +0.20
  Context Recall:    +0.17
```

Exact numbers vary by model and embedding quality. The pattern — Mode B consistently
outperforming Mode A — should hold whenever the reranker model matches the domain.
