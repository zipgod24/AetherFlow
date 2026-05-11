package rag

import (
	"strings"
	"testing"
)

func BenchmarkChunk_400_60(b *testing.B) {
	body := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa ", 1200) // ~12k words
	opts := ChunkOptions{TargetTokens: 400, Overlap: 60}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Split(body, opts)
	}
}
