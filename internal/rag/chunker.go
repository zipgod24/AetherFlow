// Package rag is the retrieval-augmented-generation pipeline: chunker,
// embedder, pgvector store, hybrid retriever.
package rag

import (
	"strings"
	"unicode"
)

// Chunk is a unit of content fed to the embedder.
type Chunk struct {
	Ordinal int
	Content string
}

// ChunkOptions controls Chunk behavior.
type ChunkOptions struct {
	// TargetTokens is the rough target chunk size, measured in whitespace tokens.
	TargetTokens int
	// Overlap is the number of tokens reused from the end of one chunk at the
	// start of the next, smoothing boundary effects on retrieval.
	Overlap int
}

// DefaultChunkOptions: 400 tokens with 60 of overlap is a solid middle ground
// for security-doc corpora.
func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{TargetTokens: 400, Overlap: 60}
}

// Chunk splits text into overlapping chunks at natural boundaries.
//
// Strategy:
//  1. Split on blank-line paragraphs.
//  2. Pack paragraphs into a chunk until adding the next would exceed
//     TargetTokens.
//  3. When closing a chunk, retain the last Overlap tokens for the next.
func Chunk(text string, opt ChunkOptions) []Chunk {
	if opt.TargetTokens <= 0 {
		opt = DefaultChunkOptions()
	}
	paras := splitParagraphs(text)
	var chunks []Chunk

	var current []string // tokens in the current chunk
	flush := func() {
		if len(current) == 0 {
			return
		}
		chunks = append(chunks, Chunk{
			Ordinal: len(chunks),
			Content: strings.Join(current, " "),
		})
		// keep the tail for overlap
		if opt.Overlap > 0 && len(current) > opt.Overlap {
			current = append([]string{}, current[len(current)-opt.Overlap:]...)
		} else if opt.Overlap > 0 {
			// keep what we have (already smaller than the overlap window)
		} else {
			current = current[:0]
		}
	}

	for _, p := range paras {
		toks := tokens(p)
		if len(current)+len(toks) > opt.TargetTokens && len(current) > 0 {
			flush()
		}
		current = append(current, toks...)
	}
	flush()

	// Re-number ordinals contiguously.
	for i := range chunks {
		chunks[i].Ordinal = i
	}
	return chunks
}

func splitParagraphs(s string) []string {
	// Normalize CRLF, split on >=1 blank line.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r)
	})
}
