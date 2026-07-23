package index

import (
	"strings"
	"testing"
)

func BenchmarkCaseInsensitiveRegexpPrefixFiveByteModerateMatches(b *testing.B) {
	const (
		prefix  = "Alpha"
		size    = 1 << 20
		matches = 200
	)

	var content strings.Builder
	content.Grow(size)
	const line = "func generated() int { return 0 } // ordinary source content\n"
	for i := 0; i < matches; i++ {
		content.WriteString(strings.Repeat(line, 80))
		content.WriteString(prefix + " generated match\n")
	}
	content.WriteString(strings.Repeat("x", size-content.Len()))

	benchmarkRegexpPrefixMeasuredSearch(b, []byte(content.String()), "(?i)Alpha.*", matches)
}

func BenchmarkCaseInsensitiveRegexpPrefixFiveByteBudgetFallback(b *testing.B) {
	const (
		match = "Alpha final match\n"
		size  = 1 << 20
	)
	nearMiss := "Alphx" + strings.Repeat("x", 55)

	var content strings.Builder
	content.Grow(size)
	for content.Len()+len(nearMiss)+len(match) <= size {
		content.WriteString(nearMiss)
	}
	content.WriteString(strings.Repeat("x", size-content.Len()-len(match)))
	content.WriteString(match)

	benchmarkRegexpPrefixMeasuredSearch(b, []byte(content.String()), "(?i)Alpha.*", 1)
}
