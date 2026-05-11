package rag

import (
	"strconv"
	"strings"
)

// vectorText formats a []float32 in pgvector's text wire format: "[1,2,3]".
//
// pgvector accepts this on input and pgx passes the string through to the
// server, which casts it to the column's vector type. This lets us avoid a
// dependency on github.com/pgvector/pgvector-go for the trivial use case.
func vectorText(v []float32) string {
	var b strings.Builder
	b.Grow(2 + len(v)*8)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
