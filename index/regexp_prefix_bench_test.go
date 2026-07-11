package index

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/query"
)

func TestRegexpPrefixHasPrefix(t *testing.T) {
	q, err := query.Parse("(?i)MyFavoriteMethod.*")
	if err != nil {
		t.Fatal(err)
	}
	regexpQuery, ok := q.(*query.Regexp)
	if !ok {
		t.Fatalf("expected query.Regexp, got %T", q)
	}

	mt := newRegexpMatchTree(regexpQuery)
	t.Logf("hasPrefix: %v, prefix: %q", mt.hasPrefix, mt.prefix)
	if !mt.hasPrefix {
		t.Fatal("expected mt.hasPrefix to be true")
	}
}

func BenchmarkCaseInsensitiveRegexpPrefix(b *testing.B) {
	ctx := context.Background()

	// Generate a 200KB content document.
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString(fmt.Sprintf("line-%d: this is some random text that does not match the pattern. we write code here.\n", i))
		if i == 500 || i == 1500 {
			sb.WriteString("line-special: here is MyFavoriteMethod defined with some arguments.\n")
		}
	}

	doc := Document{
		Name:    "my_large_code_file.go",
		Content: []byte(sb.String()),
	}

	searcher := searcherForTest(b, testShardBuilder(b, nil, doc))

	// (?i)MyFavoriteMethod.*
	// It has a case-insensitive literal prefix "MyFavoriteMethod" of length > 3
	q, err := query.Parse("(?i)MyFavoriteMethod.*")
	if err != nil {
		b.Fatal(err)
	}

	opts := &zoekt.SearchOptions{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := searcher.Search(ctx, q, opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Files) != 1 {
			b.Fatalf("expected 1 file match, got %d", len(res.Files))
		}
	}
}
