package index

import (
	"strings"
	"testing"
)

func BenchmarkCaseInsensitiveRegexpPrefixUnicodeFoldFallback(b *testing.B) {
	const (
		pattern = "(?i)smart.*"
		target  = "ſmart fallback match\n"
		size    = 1 << 20
	)

	var content strings.Builder
	content.Grow(size)
	content.WriteString("smart ascii trigger\n")
	const line = "func generated() int { return 0 } // ordinary source content\n"
	for content.Len()+len(line)+len(target) <= size {
		content.WriteString(line)
	}
	content.WriteString(strings.Repeat("x", size-content.Len()-len(target)))
	content.WriteString(target)

	benchmarkRegexpPrefixSearch(b, []byte(content.String()), pattern, 1)
}
