package index

import (
	"strings"
	"testing"
)

func BenchmarkCaseInsensitiveRegexpPrefixPartialMatch(b *testing.B) {
	prefix := strings.Repeat("A", 127) + "B"
	nearMiss := strings.Repeat("A", 126) + "BC"
	content := []byte(strings.Repeat(nearMiss, 200*1024/len(nearMiss)+1))
	benchmarkRegexpPrefixSearch(b, content, "(?i)"+prefix+".*", 0)
}
