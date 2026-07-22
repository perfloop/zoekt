package index

import (
	"context"
	"strings"
	"testing"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/query"
)

func benchmarkRegexpPrefixSearch(b *testing.B, content []byte, pattern string, wantFiles int) {
	ctx := context.Background()
	searcher := searcherForTest(b, testShardBuilder(b, nil, Document{
		Name:    "regexp-prefix-benchmark.txt",
		Content: content,
	}))
	q, err := query.Parse(pattern)
	if err != nil {
		b.Fatal(err)
	}
	opts := &zoekt.SearchOptions{}

	b.ResetTimer()
	for b.Loop() {
		res, err := searcher.Search(ctx, q, opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Files) != wantFiles {
			b.Fatalf("got %d files, want %d", len(res.Files), wantFiles)
		}
	}
}

func BenchmarkCaseInsensitiveRegexpPrefixDenseMiss(b *testing.B) {
	const prefix = "MyAwesomeFunction"
	const occurrences = 1000

	var content strings.Builder
	content.Grow(1 << 20)
	padding := strings.Repeat("x", 1000)
	for range occurrences {
		content.WriteString(prefix)
		content.WriteByte('X')
		content.WriteString(padding)
		content.WriteByte('\n')
	}

	benchmarkRegexpPrefixSearch(b, []byte(content.String()), "(?i)MyAwesomeFunction[0-9]+", 0)
}

func BenchmarkCaseInsensitiveRegexpPrefixLen2(b *testing.B) {
	content := []byte(strings.Repeat("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", 6000))
	benchmarkRegexpPrefixSearch(b, content, "(?i)qz.*", 0)
}

func BenchmarkCaseInsensitiveRegexpPrefixRE2(b *testing.B) {
	var content strings.Builder
	for i := 0; i < 2000; i++ {
		content.WriteString("line: this is some random text that does not match the pattern. we write code here.\n")
		if i == 500 || i == 1500 {
			content.WriteString("line-special: here is MyAwesomeFunction defined with some arguments.\n")
		}
	}

	benchmarkRegexpPrefixSearch(b, []byte(content.String()), "(?i)MyAwesomeFunction.*", 1)
}
