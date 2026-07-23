package index

import (
	"strings"
	"testing"
)

func BenchmarkCaseInsensitiveRegexpPrefixLongBudgetFallback(b *testing.B) {
	const (
		prefix = "QwertyUiopAbcd"
		match  = prefix + " final match\n"
		size   = 1 << 20
	)
	// Each near miss shares thirteen bytes with the eligible literal, exhausting
	// the direct path's proportional comparison budget before the final match.
	nearMiss := "QwertyUiopAbcX" + strings.Repeat("x", 49)

	var content strings.Builder
	content.Grow(size)
	for content.Len()+len(nearMiss)+len(match) <= size {
		content.WriteString(nearMiss)
	}
	content.WriteString(strings.Repeat("x", size-content.Len()-len(match)))
	content.WriteString(match)

	benchmarkRegexpPrefixMeasuredSearch(b, []byte(content.String()), "(?i)"+prefix+".*", 1)
}
