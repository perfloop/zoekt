package index_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sourcegraph/zoekt"
	zoektindex "github.com/sourcegraph/zoekt/index"
	"github.com/sourcegraph/zoekt/query"
)

func newRegexpPrefixSearcher(b *testing.B, content []byte) zoekt.Searcher {
	b.Helper()

	builder, err := zoektindex.NewShardBuilder(nil)
	if err != nil {
		b.Fatal(err)
	}
	if err := builder.Add(zoektindex.Document{
		Name:    "regexp-prefix-benchmark.txt",
		Content: content,
	}); err != nil {
		b.Fatal(err)
	}

	file, err := os.CreateTemp(b.TempDir(), "regexp-prefix-*.zoekt")
	if err != nil {
		b.Fatal(err)
	}
	if err := builder.Write(file); err != nil {
		b.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		b.Fatal(err)
	}
	indexFile, err := zoektindex.NewIndexFile(file)
	if err != nil {
		b.Fatal(err)
	}
	searcher, err := zoektindex.NewSearcher(indexFile)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(searcher.Close)
	return searcher
}

func benchmarkRegexpPrefixSearch(b *testing.B, content []byte, pattern string, wantFiles int) {
	searcher := newRegexpPrefixSearcher(b, content)
	q, err := query.Parse(pattern)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	opts := &zoekt.SearchOptions{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := searcher.Search(ctx, q, opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Files) != wantFiles {
			b.Fatalf("got %d files, want %d", len(res.Files), wantFiles)
		}
	}
}

func BenchmarkCaseInsensitiveRegexpPrefixMatch(b *testing.B) {
	var content strings.Builder
	for i := 0; i < 2000; i++ {
		content.WriteString(fmt.Sprintf("line-%d: this is some random text that does not match the pattern. we write code here.\n", i))
		if i == 1000 {
			content.WriteString("line-special: here is MyGreatMethod defined with some arguments.\n")
		}
	}
	benchmarkRegexpPrefixSearch(b, []byte(content.String()), "(?i)MyGreatMethod.*", 1)
}

func BenchmarkCaseInsensitiveRegexpPrefixLen3(b *testing.B) {
	var content strings.Builder
	for i := 0; i < 2000; i++ {
		content.WriteString(fmt.Sprintf("line-%d: this is some random text that does not match the pattern. we write code here.\n", i))
	}
	benchmarkRegexpPrefixSearch(b, []byte(content.String()), "(?i)for.*", 0)
}

func BenchmarkCaseInsensitiveRegexpPrefixLateMatch(b *testing.B) {
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
	content.WriteString(prefix)
	content.WriteString("7\n")

	benchmarkRegexpPrefixSearch(b, []byte(content.String()), "(?i)MyAwesomeFunction[0-9]+", 1)
}
