package index

import (
	"context"
	"strings"
	"testing"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/query"
)

func benchmarkRegexpPrefixMeasuredSearch(b *testing.B, content []byte, pattern string, wantMatches int) {
	b.Helper()

	searcher := searcherForTest(b, testShardBuilder(b, nil, Document{
		Name:    "regexp-prefix-five-byte-benchmark.txt",
		Content: content,
	}))
	q, err := query.Parse(pattern)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	opts := &zoekt.SearchOptions{}

	lastMatchCount := 0
	run := func() {
		res, err := searcher.Search(ctx, q, opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Files) != 1 {
			b.Fatalf("got %d files, want 1", len(res.Files))
		}
		if res.MatchCount != wantMatches {
			b.Fatalf("got %d matches, want %d", res.MatchCount, wantMatches)
		}
		lastMatchCount = res.MatchCount
	}

	run()
	b.SetBytes(int64(len(content)))
	b.ResetTimer()
	for b.Loop() {
		run()
	}
	b.ReportMetric(float64(lastMatchCount), "matches/op")
}

func BenchmarkCaseInsensitiveRegexpPrefixFiveByteSparse(b *testing.B) {
	const (
		prefix = "Alpha"
		size   = 1 << 20
	)

	var content strings.Builder
	content.Grow(size)
	const line = "func generated() int { return 0 } // ordinary source content\n"
	for content.Len()+len(line)+2*(len(prefix)+32) <= size {
		content.WriteString(line)
	}
	content.WriteString(prefix + " first match\n")
	content.WriteString(strings.Repeat("x", size-content.Len()-len(prefix)-len(" final match\n")))
	content.WriteString(prefix + " final match\n")

	benchmarkRegexpPrefixMeasuredSearch(b, []byte(content.String()), "(?i)Alpha.*", 2)
}

func BenchmarkCaseInsensitiveRegexpPrefixFiveByteDenseFallback(b *testing.B) {
	const (
		nearMiss = "Alphx\n"
		match    = "Alpha final match\n"
		size     = 1 << 20
	)

	content := strings.Repeat(nearMiss, (size-len(match))/len(nearMiss))
	content += strings.Repeat("x", size-len(content)-len(match))
	content += match

	benchmarkRegexpPrefixMeasuredSearch(b, []byte(content), "(?i)Alpha.*", 1)
}
