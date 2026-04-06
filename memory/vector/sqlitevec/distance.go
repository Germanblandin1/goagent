package sqlitevec

// DistanceMetric selects the vector similarity function used in Search.
type DistanceMetric string

const (
	// L2 uses Euclidean distance via the sqlite-vec KNN index (MATCH … AND k = ?).
	// Score is 1 / (1 + distance), in (0, 1].
	// This is the default. Queries are index-accelerated.
	L2 DistanceMetric = "l2"

	// Cosine uses cosine distance via the vec_distance_cosine SQL function.
	// Score is 1 - distance; for unit-normalised vectors the range is [0, 1].
	// Queries perform a full scan — suitable for datasets up to tens of thousands
	// of rows. For larger datasets, normalise vectors before inserting and use L2
	// (L2 on unit vectors is equivalent to cosine similarity).
	Cosine DistanceMetric = "cosine"
)
