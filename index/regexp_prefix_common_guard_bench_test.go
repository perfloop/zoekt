package index

import (
	"strings"
	"testing"
)

func BenchmarkCaseInsensitiveRegexpPrefixCommonMatches(b *testing.B) {
	line := "error: " + strings.Repeat("detail ", 30) + "\n"
	content := []byte(strings.Repeat(line, 1000))
	benchmarkRegexpPrefixSearch(b, content, "(?i)err.*", 1)
}
