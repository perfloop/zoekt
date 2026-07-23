package index

import (
	"strings"
	"testing"
)

func BenchmarkCaseInsensitiveRegexpPrefixLongCommonMatches(b *testing.B) {
	const (
		prefix      = "LongErrorName"
		occurrences = 200
	)

	var content strings.Builder
	content.Grow(200 << 10)
	for i := 0; i < occurrences*10; i++ {
		content.WriteString("func generated() int { return 0 } // ordinary source content\n")
		if i%10 == 0 {
			content.WriteString(prefix)
			content.WriteString(" appears in this generated source line\n")
		}
	}

	benchmarkRegexpPrefixSearch(b, []byte(content.String()), "(?i)LongErrorName.*", 1)
}
