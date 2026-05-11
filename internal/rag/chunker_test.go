package rag

import (
	"strings"
	"testing"
)

func TestChunkerRespectsTarget(t *testing.T) {
	// 1000 words, target 100 → expect ~10 chunks.
	words := make([]string, 1000)
	for i := range words {
		words[i] = "word"
	}
	text := strings.Join(words, " ")
	chunks := Chunk(text, ChunkOptions{TargetTokens: 100, Overlap: 0})
	if len(chunks) < 9 || len(chunks) > 12 {
		t.Fatalf("expected ~10 chunks, got %d", len(chunks))
	}
}

func TestChunkerOverlap(t *testing.T) {
	parts := []string{}
	for i := 0; i < 5; i++ {
		parts = append(parts, strings.Repeat("alpha ", 80))
	}
	text := strings.Join(parts, "\n\n")
	chunks := Chunk(text, ChunkOptions{TargetTokens: 100, Overlap: 20})
	if len(chunks) < 2 {
		t.Fatalf("expected >=2 chunks, got %d", len(chunks))
	}
	// First chunk's last 20 tokens should equal second chunk's first 20.
	first := strings.Fields(chunks[0].Content)
	second := strings.Fields(chunks[1].Content)
	if len(first) < 20 || len(second) < 20 {
		t.Fatalf("chunks too small for overlap check: %d, %d", len(first), len(second))
	}
	for i := 0; i < 20; i++ {
		if first[len(first)-20+i] != second[i] {
			t.Fatalf("overlap mismatch at %d: %q vs %q", i, first[len(first)-20+i], second[i])
		}
	}
}

func TestChunkerOrdinalsContiguous(t *testing.T) {
	text := strings.Repeat("alpha beta gamma delta\n\n", 30)
	chunks := Chunk(text, DefaultChunkOptions())
	for i, c := range chunks {
		if c.Ordinal != i {
			t.Fatalf("ordinal[%d] = %d", i, c.Ordinal)
		}
	}
}
