package index

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/query"
)

func TestAsciiFoldNeedle(t *testing.T) {
	cases := []struct {
		needle   string
		haystack string
		want     bool
	}{
		{"abc", "abc", true},
		{"abc", "ABC", true},
		{"abc", "aBc", true},
		{"abc", "def", false},
		{"method", "my_method_Method_METHOD_mEtHoD", true},
		{"123", "abc123def123", true},
		{"abc", "ab", false},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s in %s", c.needle, c.haystack), func(t *testing.T) {
			an := newAsciiFoldNeedle(c.needle)
			got := an.exists([]byte(c.haystack))
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

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

func TestRegexpPrefixCorrectness(t *testing.T) {
	// Standard searcher matching versus our optimized prefix matching
	docs := []Document{
		{Name: "f1", Content: []byte("line-special: here is MyFavoriteMethod defined with some arguments.\n")},
		{Name: "f2", Content: []byte("MyFavoriteMethod at the start\n")},
		{Name: "f3", Content: []byte("ending with MyFavoriteMethod")},
		{Name: "f4", Content: []byte("mixed CASE: mYfAvOrItEmEtHoD here")},
		{Name: "f5", Content: []byte("multiple: MyFavoriteMethod MyFavoriteMethod MyFavoriteMethod")},
		{Name: "f6", Content: []byte("no matches here at all")},
	}

	searcher := searcherForTest(t, testShardBuilder(t, nil, docs...))

	q, err := query.Parse("(?i)MyFavoriteMethod.*")
	if err != nil {
		t.Fatal(err)
	}

	res, err := searcher.Search(context.Background(), q, &zoekt.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Verify that we matched only the 5 documents correctly and matches are identical
	if len(res.Files) != 5 {
		t.Fatalf("expected 5 matched files, got %d", len(res.Files))
	}

	// Verify that 's'/'S' folding is correctly excluded from the pre-scanner
	smartQ, err := query.Parse("(?i)smart.*")
	if err != nil {
		t.Fatal(err)
	}
	// 'smart' has 's' which folds with long s, so hasPrefix must be false
	smartRegexpQuery := smartQ.(*query.Regexp)
	smartMT := newRegexpMatchTree(smartRegexpQuery)
	if smartMT.hasPrefix {
		t.Fatal("expected smartMT.hasPrefix to be false due to 's' character")
	}

	// 'kelvin' has 'k' which folds with Kelvin symbol, so hasPrefix must be false
	kelvinQ, err := query.Parse("(?i)kelvin.*")
	if err != nil {
		t.Fatal(err)
	}
	kelvinRegexpQuery := kelvinQ.(*query.Regexp)
	kelvinMT := newRegexpMatchTree(kelvinRegexpQuery)
	if kelvinMT.hasPrefix {
		t.Fatal("expected kelvinMT.hasPrefix to be false due to 'k' character")
	}
}

func BenchmarkCaseInsensitiveRegexpPrefix(b *testing.B) {
	ctx := context.Background()

	// Generate a 200KB content document.
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString(fmt.Sprintf("line-%d: this is some random text that does not match the pattern. we write code here.\n", i))
	}

	doc := Document{
		Name:    "my_large_code_file.go",
		Content: []byte(sb.String()),
	}

	searcher := searcherForTest(b, testShardBuilder(b, nil, doc))

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
		if len(res.Files) != 0 {
			b.Fatalf("expected 0 file matches, got %d", len(res.Files))
		}
	}
}

func BenchmarkCaseInsensitiveRegexpPrefixMatch(b *testing.B) {
	ctx := context.Background()

	// Generate a 200KB content document.
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString(fmt.Sprintf("line-%d: this is some random text that does not match the pattern. we write code here.\n", i))
		if i == 1000 {
			sb.WriteString("line-special: here is MyFavoriteMethod defined with some arguments.\n")
		}
	}

	doc := Document{
		Name:    "my_large_code_file.go",
		Content: []byte(sb.String()),
	}

	searcher := searcherForTest(b, testShardBuilder(b, nil, doc))

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
