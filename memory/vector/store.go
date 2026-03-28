package vector

// This file intentionally contains no executable code.
// Session filtering in InMemoryStore.Search is controlled by the
// internal/session package: session.WithID injects a session ID into the
// context, and session.IDFromContext reads it back.
//
// External callers use:
//   - vector.ExtractText(blocks)        — concatenate text blocks
//   - vector.ChunkToMessage(orig, chunk) — build a Message from a chunk
