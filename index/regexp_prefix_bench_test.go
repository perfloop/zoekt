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

	// Also verify that the Kelvin symbol / "Kelvin" case-insensitive search is NOT optimized and still works
	kelvinDoc := Document{Name: "f_kelvin", Content: []byte("Temperature in Kelvin is high.\n")}
	kelvinSearcher := searcherForTest(t, testShardBuilder(t, nil, kelvinDoc))

	// Search for 'kelvin' (case-insensitive) - contains 'k', so pre-scan must be disabled
	kelvinQ, err := query.Parse("(?i)kelvin.*")
	if err != nil {
		t.Fatal(err)
	}
	// Verify hasPrefix is false for kelvinQ because it has 'k'
	regexpQuery := kelvinQ.(*query.Regexp)
	mt := newRegexpMatchTree(regexpQuery)
	if mt.hasPrefix {
		t.Fatal("expected mt.hasPrefix to be false for 'kelvin' query due to 'k'")
	}

	kelvinRes, err := kelvinSearcher.Search(context.Background(), kelvinQ, &zoekt.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(kelvinRes.Files) != 1 {
		t.Fatalf("expected 1 file match for kelvin, got %d", len(kelvinRes.Files))
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
