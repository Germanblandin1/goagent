package rag_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/Germanblandin1/goagent/rag"
)

const eps = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < eps
}

func TestPrecisionAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		relevant  []string
		k         int
		want      float64
	}{
		{
			name:      "k exact",
			retrieved: []string{"doc1", "doc2", "doc3"},
			relevant:  []string{"doc1", "doc3"},
			k:         3,
			want:      2.0 / 3.0,
		},
		{
			name:      "k greater than len",
			retrieved: []string{"doc1", "doc2"},
			relevant:  []string{"doc1", "doc3"},
			k:         5,
			want:      0.50,
		},
		{
			name:      "k=1 no hit at top",
			retrieved: []string{"doc1", "doc2", "doc3"},
			relevant:  []string{"doc3"},
			k:         1,
			want:      0.00,
		},
		{
			name:      "no hits",
			retrieved: []string{"doc2", "doc4"},
			relevant:  []string{"doc1", "doc3"},
			k:         2,
			want:      0.00,
		},
		{
			name:      "empty retrieved",
			retrieved: []string{},
			relevant:  []string{"doc1"},
			k:         3,
			want:      0.00,
		},
		{
			name:      "k zero",
			retrieved: []string{"doc1", "doc2"},
			relevant:  []string{"doc1"},
			k:         0,
			want:      0.00,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rag.PrecisionAtK(tc.retrieved, tc.relevant, tc.k)
			if !approxEqual(got, tc.want) {
				t.Errorf("PrecisionAtK(%v, %v, %d) = %.6f, want %.6f",
					tc.retrieved, tc.relevant, tc.k, got, tc.want)
			}
		})
	}
}

func TestRecallAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		relevant  []string
		k         int
		want      float64
	}{
		{
			name:      "retrieves all",
			retrieved: []string{"doc1", "doc3", "doc7"},
			relevant:  []string{"doc1", "doc3", "doc7"},
			k:         3,
			want:      1.00,
		},
		{
			name:      "partial recall",
			retrieved: []string{"doc1", "doc2", "doc3"},
			relevant:  []string{"doc1", "doc3", "doc7"},
			k:         3,
			want:      2.0 / 3.0,
		},
		{
			name:      "small k",
			retrieved: []string{"doc1", "doc2", "doc3"},
			relevant:  []string{"doc1", "doc3", "doc7"},
			k:         1,
			want:      1.0 / 3.0,
		},
		{
			name:      "empty relevant",
			retrieved: []string{"doc1"},
			relevant:  []string{},
			k:         3,
			want:      0.00,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rag.RecallAtK(tc.retrieved, tc.relevant, tc.k)
			if !approxEqual(got, tc.want) {
				t.Errorf("RecallAtK(%v, %v, %d) = %.6f, want %.6f",
					tc.retrieved, tc.relevant, tc.k, got, tc.want)
			}
		})
	}
}

func TestReciprocalRank(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		relevant  []string
		want      float64
	}{
		{
			name:      "hit at position 1",
			retrieved: []string{"doc1", "doc2", "doc3"},
			relevant:  []string{"doc1"},
			want:      1.00,
		},
		{
			name:      "hit at position 2",
			retrieved: []string{"doc2", "doc1", "doc3"},
			relevant:  []string{"doc1"},
			want:      0.50,
		},
		{
			name:      "hit at position 3",
			retrieved: []string{"doc2", "doc5", "doc1"},
			relevant:  []string{"doc1", "doc3"},
			want:      1.0 / 3.0,
		},
		{
			name:      "no hit",
			retrieved: []string{"doc2", "doc4", "doc6"},
			relevant:  []string{"doc1", "doc3"},
			want:      0.00,
		},
		{
			name:      "empty relevant",
			retrieved: []string{"doc1", "doc2"},
			relevant:  []string{},
			want:      0.00,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rag.ReciprocalRank(tc.retrieved, tc.relevant)
			if !approxEqual(got, tc.want) {
				t.Errorf("ReciprocalRank(%v, %v) = %.6f, want %.6f",
					tc.retrieved, tc.relevant, got, tc.want)
			}
		})
	}
}

func TestMRR(t *testing.T) {
	tests := []struct {
		name  string
		cases []rag.EvalCase
		want  float64
	}{
		{
			name:  "empty cases",
			cases: []rag.EvalCase{},
			want:  0.00,
		},
		{
			name: "single case hit at position 2",
			cases: []rag.EvalCase{
				{Query: "q1", Retrieved: []string{"doc2", "doc1"}, Relevant: []string{"doc1"}},
			},
			want: 0.50,
		},
		{
			name: "three mixed cases",
			cases: []rag.EvalCase{
				{Query: "q1", Retrieved: []string{"doc1", "doc2"}, Relevant: []string{"doc1"}},       // RR=1.0
				{Query: "q2", Retrieved: []string{"doc5", "doc1", "doc3"}, Relevant: []string{"doc3"}}, // RR=1/3
				{Query: "q3", Retrieved: []string{"doc9", "doc8", "doc7"}, Relevant: []string{"doc1"}}, // RR=0.0
			},
			want: (1.0 + 1.0/3.0 + 0.0) / 3.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rag.MRR(tc.cases)
			if !approxEqual(got, tc.want) {
				t.Errorf("MRR(%v) = %.6f, want %.6f", tc.cases, got, tc.want)
			}
		})
	}
}

func ExamplePrecisionAtK() {
	retrieved := []string{"doc1", "doc2", "doc3", "doc4", "doc9"}
	relevant := []string{"doc1", "doc3", "doc7"}

	fmt.Printf("%.2f\n", rag.PrecisionAtK(retrieved, relevant, 5))
	fmt.Printf("%.2f\n", rag.PrecisionAtK(retrieved, relevant, 3))
	// Output:
	// 0.40
	// 0.67
}

func ExampleRecallAtK() {
	retrieved := []string{"doc1", "doc2", "doc3", "doc4", "doc9"}
	relevant := []string{"doc1", "doc3", "doc7"}

	fmt.Printf("%.2f\n", rag.RecallAtK(retrieved, relevant, 5))
	fmt.Printf("%.2f\n", rag.RecallAtK(retrieved, relevant, 1))
	// Output:
	// 0.67
	// 0.33
}

func ExampleReciprocalRank() {
	retrieved := []string{"doc2", "doc5", "doc1"}
	relevant := []string{"doc1", "doc3"}

	fmt.Printf("%.2f\n", rag.ReciprocalRank(retrieved, relevant))
	// Output:
	// 0.33
}

func ExampleMRR() {
	cases := []rag.EvalCase{
		{Query: "q1", Retrieved: []string{"doc1", "doc2"}, Relevant: []string{"doc1"}},
		{Query: "q2", Retrieved: []string{"doc2", "doc1"}, Relevant: []string{"doc1"}},
	}
	fmt.Printf("%.2f\n", rag.MRR(cases))
	// Output:
	// 0.75
}
