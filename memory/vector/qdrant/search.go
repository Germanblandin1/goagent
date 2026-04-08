package qdrant

// SearchOption configures an individual Search call.
// Options are applied in order before the query is executed.
//
// Currently no built-in options are defined. The variadic parameter
// exists to allow future options (score threshold, metadata filters, etc.)
// without breaking the existing call sites.
type SearchOption func(*searchConfig)

// searchConfig holds the resolved configuration for a single Search call.
// All fields have zero values that represent "no constraint".
type searchConfig struct{} // expanded in future versions
