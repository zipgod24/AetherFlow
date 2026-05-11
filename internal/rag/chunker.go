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

// Split splits text into overlapping chunks at natural boundaries.
//
// Strategy:
//  1. Split on blank-line paragraphs.
//  2. Pack paragraphs into a chunk until reaching TargetTokens.
//  3. If a single paragraph itself exceeds TargetTokens, subdivide it by
//     token count so no chunk ever exceeds the target.
//  4. When closing a chunk, retain the last Overlap tokens for the next.
func Split(text string, opt ChunkOptions) []Chunk {
	if opt.TargetTokens <= 0 {
		opt = DefaultChunkOptions()
	}
	if opt.Overlap >= opt.TargetTokens {
		opt.Overlap = opt.TargetTokens / 4 // sanity: keep some forward progress
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
		if opt.Overlap > 0 && len(current) > opt.Overlap {
			current = append([]string{}, current[len(current)-opt.Overlap:]...)
		} else if opt.Overlap == 0 {
			current = current[:0]
		}
		// If Overlap > len(current), keep current as-is (smaller than overlap window).
	}

	for _, p := range paras {
		toks := tokens(p)
		for len(toks) > 0 {
			room := opt.TargetTokens - len(current)
			if room <= 0 {
				flush()
				continue
			}
			take := room
			if take > len(toks) {
				take = len(toks)
			}
			current = append(current, toks[:take]...)
			toks = toks[take:]
			if len(current) >= opt.TargetTokens {
				flush()
			}
		}
	}
	flush()

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
