package vector_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// rcContent builds a ChunkContent with a single text block.
func rcContent(s string) vector.ChunkContent {
	return vector.ChunkContent{
		Blocks: []goagent.ContentBlock{goagent.TextBlock(s)},
	}
}

// rcContentMeta builds a ChunkContent with a single text block and metadata.
func rcContentMeta(s string, meta map[string]any) vector.ChunkContent {
	return vector.ChunkContent{
		Blocks:   []goagent.ContentBlock{goagent.TextBlock(s)},
		Metadata: meta,
	}
}

// rcChar is a character-counting SizeEstimator for deterministic tests.
type rcChar struct{}

func (rcChar) Measure(_ context.Context, text string) int { return len(text) }

func TestRecursiveChunkerSingleChunk(t *testing.T) {
	c := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(500),
		vector.WithRCEstimator(rcChar{}),
		vector.WithRCOverlap(0),
	)
	got, err := c.Chunk(context.Background(), rcContent("short text"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Blocks[0].Text != "short text" {
		t.Errorf("text = %q, want %q", got[0].Blocks[0].Text, "short text")
	}
	// No chunk_index when only one chunk.
	if _, ok := got[0].Metadata["chunk_index"]; ok {
		t.Error("chunk_index should not be present for single chunk")
	}
}

func TestRecursiveChunkerParagraphSplit(t *testing.T) {
	// Three paragraphs, each ~20 chars. maxSize=30 forces splits at \n\n,
	// not in the middle of a sentence.
	para1 := "First paragraph here."
	para2 := "Second paragraph here."
	para3 := "Third paragraph here."
	text := para1 + "\n\n" + para2 + "\n\n" + para3

	c := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(30),
		vector.WithRCEstimator(rcChar{}),
		vector.WithRCOverlap(0),
	)
	got, err := c.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	// Each chunk should be one of the original paragraphs (possibly combined if
	// they fit), never a partial paragraph.
	for _, ch := range got {
		chText := ch.Blocks[0].Text
		if strings.Contains(chText, "\n\n") {
			// Two paragraphs merged is fine only if their combined size <= maxSize.
			size := len(chText)
			if size > 30 {
				t.Errorf("chunk exceeds maxSize: %q (%d chars)", chText, size)
			}
		}
	}
	// Verify no chunk splits any of the original paragraph texts.
	for _, ch := range got {
		chText := ch.Blocks[0].Text
		for _, para := range []string{para1, para2, para3} {
			if strings.Contains(chText, para[:5]) && !strings.Contains(chText, para) {
				// chunk starts with this paragraph's text but doesn't contain the whole thing
				t.Errorf("paragraph split in the middle: chunk=%q, para=%q", chText, para)
			}
		}
	}
}

func TestRecursiveChunkerFallsBackToSentences(t *testing.T) {
	// One long paragraph with sentence boundaries (". ") but no \n\n.
	// maxSize forces splits, but they should land at ". " not mid-sentence.
	sentences := []string{
		"The quick brown fox jumps over the lazy dog.",
		"Pack my box with five dozen liquor jugs.",
		"How vexingly quick daft zebras jump.",
	}
	text := strings.Join(sentences, " ")

	c := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(55),
		vector.WithRCEstimator(rcChar{}),
		vector.WithRCOverlap(0),
	)
	got, err := c.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	// Each chunk must end with a complete sentence, not a half-sentence.
	// Verify that no chunk contains a word split — every word in every chunk
	// must be a complete word from the original text.
	originalWords := strings.Fields(text)
	for _, ch := range got {
		for _, w := range strings.Fields(ch.Blocks[0].Text) {
			found := false
			for _, ow := range originalWords {
				if strings.Trim(w, ".,!?") == strings.Trim(ow, ".,!?") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("chunk contains suspicious word %q not in original", w)
			}
		}
	}
}

func TestRecursiveChunkerFallsBackToWords(t *testing.T) {
	// A single long "word soup" with no separators except spaces.
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet"}
	text := strings.Join(words, " ")

	c := vector.NewRecursiveChunker(
		vector.WithRCSeparators([]string{"\n\n", "\n"}), // no sentence/word seps
		vector.WithRCMaxSize(15),
		vector.WithRCEstimator(rcChar{}),
		vector.WithRCOverlap(0),
	)
	got, err := c.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks (word fallback), got %d", len(got))
	}
	// Invariant: no chunk should contain a string that isn't one of the original words.
	for _, ch := range got {
		for _, w := range strings.Fields(ch.Blocks[0].Text) {
			if !slices.Contains(words, w) {
				t.Errorf("word %q in chunk is not a complete original word", w)
			}
		}
	}
}

func TestRecursiveChunkerOverlapContainsTail(t *testing.T) {
	para1 := "First paragraph with enough words to fill a chunk."
	para2 := "Second paragraph with enough words to fill a chunk."
	para3 := "Third paragraph with enough words to fill a chunk."
	text := para1 + "\n\n" + para2 + "\n\n" + para3

	c := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(60),
		vector.WithRCEstimator(rcChar{}),
		vector.WithRCOverlap(15),
	)
	got, err := c.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	// chunk[i+1] must start with text that also appears at the end of chunk[i].
	for i := 0; i < len(got)-1; i++ {
		prevText := got[i].Blocks[0].Text
		nextText := got[i+1].Blocks[0].Text

		prevWords := strings.Fields(prevText)
		nextWords := strings.Fields(nextText)

		if len(prevWords) == 0 || len(nextWords) == 0 {
			continue
		}

		// The first word of chunk[i+1] (after the overlap prefix) or the overlap
		// prefix itself should appear somewhere in chunk[i].
		firstWordOfNext := nextWords[0]
		foundInPrev := strings.Contains(prevText, firstWordOfNext)
		if !foundInPrev {
			t.Errorf("chunk[%d] starts with %q which does not appear in chunk[%d]: %q",
				i+1, firstWordOfNext, i, prevText)
		}
	}
}

func TestRecursiveChunkerMetadata(t *testing.T) {
	words := make([]string, 30)
	for i := range words {
		words[i] = "word"
	}
	text := strings.Join(words, " ")

	origMeta := map[string]any{"source": "doc.md", "page": 1}
	content := rcContentMeta(text, origMeta)

	c := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(20),
		vector.WithRCEstimator(rcChar{}),
		vector.WithRCOverlap(0),
	)
	got, err := c.Chunk(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}

	total := len(got)
	for i, ch := range got {
		if ch.Metadata["chunk_index"] != i {
			t.Errorf("chunk[%d].chunk_index = %v, want %d", i, ch.Metadata["chunk_index"], i)
		}
		if ch.Metadata["chunk_total"] != total {
			t.Errorf("chunk[%d].chunk_total = %v, want %d", i, ch.Metadata["chunk_total"], total)
		}
		if ch.Metadata["source"] != "doc.md" {
			t.Errorf("chunk[%d]: original metadata 'source' not propagated", i)
		}
		if ch.Metadata["page"] != 1 {
			t.Errorf("chunk[%d]: original metadata 'page' not propagated", i)
		}
	}

	// Original metadata must not be mutated.
	if _, ok := origMeta["chunk_index"]; ok {
		t.Error("original metadata was mutated: chunk_index added")
	}
}

func TestRecursiveChunkerEmptyInput(t *testing.T) {
	c := vector.NewRecursiveChunker()

	t.Run("empty string", func(t *testing.T) {
		got, err := c.Chunk(context.Background(), rcContent(""))
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("no blocks", func(t *testing.T) {
		got, err := c.Chunk(context.Background(), vector.ChunkContent{})
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		got, err := c.Chunk(context.Background(), rcContent("   \n\n  "))
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

// ── Additional tests ─────────────────────────────────────────────────────────

func TestRecursiveChunkerContentPreservation(t *testing.T) {
	var words []string
	for i := 0; i < 150; i++ {
		words = append(words, fmt.Sprintf("palabra%d", i))
	}
	p1 := strings.Join(words[:50], " ")
	p2 := strings.Join(words[50:100], " ")
	p3 := strings.Join(words[100:], " ")
	text := p1 + "\n\n" + p2 + "\n\n" + p3

	chunker := vector.NewRecursiveChunker(vector.WithRCMaxSize(60), vector.WithRCOverlap(0))
	chunks, err := chunker.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatalf("Chunk() error inesperado: %v", err)
	}

	var allContent strings.Builder
	for _, c := range chunks {
		allContent.WriteString(vector.ExtractText(c.Blocks))
		allContent.WriteString(" ")
	}
	combined := allContent.String()

	for _, w := range words {
		if !strings.Contains(combined, w) {
			t.Errorf("palabra %q se perdió en el chunking", w)
		}
	}
}

func TestRecursiveChunkerNoInfiniteLoop(t *testing.T) {
	giant := strings.Repeat("x", 300)

	chunker := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(50),
		vector.WithRCEstimator(&vector.CharEstimator{}),
	)

	done := make(chan error, 1)
	go func() {
		_, err := chunker.Chunk(context.Background(), rcContent(giant))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Chunk() retornó error inesperado: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Chunk() entró en bucle infinito con token que supera maxSize")
	}
}

func TestRecursiveChunkerChunkTotalConsistency(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "Sección %d: contenido de la sección con palabras adicionales para que sea suficientemente largo.\n\n", i)
	}
	text := sb.String()

	chunker := vector.NewRecursiveChunker(vector.WithRCMaxSize(50), vector.WithRCOverlap(5))
	chunks, err := chunker.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatalf("Chunk() error inesperado: %v", err)
	}
	if len(chunks) <= 1 {
		t.Skip("el texto no produjo múltiples chunks con esta configuración")
	}

	expectedTotal := len(chunks)
	for i, c := range chunks {
		gotTotal, ok := c.Metadata["chunk_total"]
		if !ok {
			t.Errorf("chunk %d: falta chunk_total en metadata", i)
			continue
		}
		if gotTotal != expectedTotal {
			t.Errorf("chunk %d: chunk_total=%v, quiero %d", i, gotTotal, expectedTotal)
		}

		gotIndex, ok := c.Metadata["chunk_index"]
		if !ok {
			t.Errorf("chunk %d: falta chunk_index en metadata", i)
			continue
		}
		if gotIndex != i {
			t.Errorf("chunk %d: chunk_index=%v, quiero %d", i, gotIndex, i)
		}
	}
}

func TestRecursiveChunkerOverlapNoDuplication(t *testing.T) {
	text := "Primer párrafo con contenido propio y específico de la primera sección.\n\n" +
		"Segundo párrafo con contenido propio y específico de la segunda sección.\n\n" +
		"Tercer párrafo con contenido propio y específico de la tercera sección."

	chunker := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(80),
		vector.WithRCOverlap(15),
		vector.WithRCEstimator(&vector.CharEstimator{}),
	)
	chunks, err := chunker.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatalf("Chunk() error inesperado: %v", err)
	}
	if len(chunks) <= 1 {
		t.Skip("el texto no produjo múltiples chunks con esta configuración")
	}

	for i := 1; i < len(chunks); i++ {
		curr := vector.ExtractText(chunks[i].Blocks)
		prev := vector.ExtractText(chunks[i-1].Blocks)

		if curr == prev {
			t.Errorf("chunk %d es idéntico al chunk %d", i, i-1)
		}

		if len(prev) > 0 {
			commonLen := longestCommonPrefixLen(curr, prev)
			ratio := float64(commonLen) / float64(len(prev))
			if ratio > 0.7 {
				t.Errorf("chunk %d tiene overlap excesivo con chunk %d: %.0f%% del chunk anterior",
					i, i-1, ratio*100)
			}
		}
	}
}

// longestCommonPrefixLen returns the length of the longest common prefix of a and b.
func longestCommonPrefixLen(a, b string) int {
	maxLen := len(a)
	if len(b) < maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return maxLen
}

func TestRecursiveChunkerDeterminism(t *testing.T) {
	text := "Primera sección con su contenido.\n\n" +
		"Segunda sección con más contenido relevante.\n\n" +
		"Tercera sección con el cierre del documento."

	chunker := vector.NewRecursiveChunker(vector.WithRCMaxSize(40), vector.WithRCOverlap(5))
	content := rcContent(text)

	chunks1, err1 := chunker.Chunk(context.Background(), content)
	chunks2, err2 := chunker.Chunk(context.Background(), content)

	if err1 != nil || err2 != nil {
		t.Fatalf("Chunk() errores: %v / %v", err1, err2)
	}
	if len(chunks1) != len(chunks2) {
		t.Fatalf("cantidad de chunks no es determinista: %d vs %d",
			len(chunks1), len(chunks2))
	}
	for i := range chunks1 {
		t1 := vector.ExtractText(chunks1[i].Blocks)
		t2 := vector.ExtractText(chunks2[i].Blocks)
		if t1 != t2 {
			t.Errorf("chunk %d no es determinista entre llamadas:\n  llamada 1: %q\n  llamada 2: %q",
				i, t1, t2)
		}
	}
}

func TestRecursiveChunkerMultipleTextBlocks(t *testing.T) {
	chunker := vector.NewRecursiveChunker(vector.WithRCMaxSize(200), vector.WithRCOverlap(0))

	chunks, err := chunker.Chunk(context.Background(), vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.TextBlock("Bloque alfa con contenido propio."),
			goagent.TextBlock("Bloque beta con contenido propio."),
			goagent.TextBlock("Bloque gamma con contenido propio."),
		},
	})
	if err != nil {
		t.Fatalf("Chunk() error inesperado: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("Chunk() retornó cero chunks para input no vacío")
	}

	var allText strings.Builder
	for _, c := range chunks {
		allText.WriteString(vector.ExtractText(c.Blocks))
	}

	for _, expected := range []string{"Bloque alfa", "Bloque beta", "Bloque gamma"} {
		if !strings.Contains(allText.String(), expected) {
			t.Errorf("contenido %q no está presente en ningún chunk", expected)
		}
	}
}

func TestRecursiveChunkerMaxSizeRespected(t *testing.T) {
	maxSize := 80
	est := &vector.CharEstimator{}

	var sb strings.Builder
	sb.WriteString(strings.Repeat("corto ", 8))
	sb.WriteString("\n\n")
	sb.WriteString(strings.Repeat("largo ", 30))
	sb.WriteString("\n\n")
	sb.WriteString(strings.Repeat("medio ", 12))
	text := sb.String()

	chunker := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(maxSize),
		vector.WithRCOverlap(0),
		vector.WithRCEstimator(est),
	)
	chunks, err := chunker.Chunk(context.Background(), rcContent(text))
	if err != nil {
		t.Fatalf("Chunk() error inesperado: %v", err)
	}

	// 15% margin for words that cannot be split mid-word.
	maxAllowed := int(float64(maxSize) * 1.15)
	for i, c := range chunks {
		size := est.Measure(context.Background(), vector.ExtractText(c.Blocks))
		if size > maxAllowed {
			t.Errorf("chunk %d excede maxSize: tamaño=%d, maxSize=%d, margen=%d",
				i, size, maxSize, maxAllowed)
		}
	}
}

func TestRecursiveChunkerCustomSeparators(t *testing.T) {
	// Use "\nfunc " as separator to split Go-like source code by function.
	src := `package main

func foo() {
	println("foo")
}
func bar() {
	println("bar")
}
func baz() {
	println("baz")
}`

	c := vector.NewRecursiveChunker(
		vector.WithRCSeparators([]string{"\nfunc "}),
		vector.WithRCMaxSize(60), // each function block ~25 chars; two together exceed limit
		vector.WithRCEstimator(rcChar{}),
		vector.WithRCOverlap(0),
	)
	got, err := c.Chunk(context.Background(), rcContent(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks split by func, got %d", len(got))
	}
	// Every chunk after the first must contain a function body.
	foundFoo := false
	foundBar := false
	for _, ch := range got {
		if strings.Contains(ch.Blocks[0].Text, "foo") {
			foundFoo = true
		}
		if strings.Contains(ch.Blocks[0].Text, "bar") {
			foundBar = true
		}
	}
	if !foundFoo {
		t.Error("no chunk contains 'foo'")
	}
	if !foundBar {
		t.Error("no chunk contains 'bar'")
	}
}
