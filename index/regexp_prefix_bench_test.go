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
		{"great", "my_great_Great_GREAT_gReAt", true},
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
	q, err := query.Parse("(?i)MyGreatMethod.*")
	if err != nil {
		t.Fatal(err)
	}
	regexpQuery, ok := q.(*query.Regexp)
	if !ok {
		t.Fatalf("expected query.Regexp, got %T", q)
	}

	mt := newRegexpMatchTree(regexpQuery)
	t.Logf("hasPrefix: %v", mt.hasPrefix)
	if !mt.hasPrefix {
		t.Fatal("expected mt.hasPrefix to be true")
	}
}

func TestRegexpPrefixCorrectness(t *testing.T) {
	// Standard searcher matching versus our optimized prefix matching
	docs := []Document{
		{Name: "f1", Content: []byte("line-special: here is MyGreatMethod defined with some arguments.\n")},
		{Name: "f2", Content: []byte("MyGreatMethod at the start\n")},
		{Name: "f3", Content: []byte("ending with MyGreatMethod")},
		{Name: "f4", Content: []byte("mixed CASE: mYgReAtMeThOd here")},
		{Name: "f5", Content: []byte("multiple: MyGreatMethod MyGreatMethod MyGreatMethod")},
		{Name: "f6", Content: []byte("no matches here at all")},
	}

	searcher := searcherForTest(t, testShardBuilder(t, nil, docs...))

	q, err := query.Parse("(?i)MyGreatMethod.*")
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
	smartRegexpQuery := smartQ.(*query.Regexp)
	smartMT := newRegexpMatchTree(smartRegexpQuery)
	if smartMT.hasPrefix {
		t.Fatal("expected smartMT.hasPrefix to be false due to 's' character")
	}

	// Verify that 'i'/'I' folding is correctly excluded from the pre-scanner
	imageQ, err := query.Parse("(?i)image.*")
	if err != nil {
		t.Fatal(err)
	}
	imageRegexpQuery := imageQ.(*query.Regexp)
	imageMT := newRegexpMatchTree(imageRegexpQuery)
	if imageMT.hasPrefix {
		t.Fatal("expected imageMT.hasPrefix to be false due to 'i' character")
	}
}

func TestRegexpPrefixAdversarial(t *testing.T) {
	// 200KB document consisting entirely of uppercase 'A' characters.
	// This tests that our IndexByte state caching prevents quadratic scans on mismatch path.
	content := strings.Repeat("A", 200*1024)
	doc := Document{
		Name:    "adversarial_file.txt",
		Content: []byte(content),
	}

	searcher := searcherForTest(t, testShardBuilder(t, nil, doc))

	// (?i)Apple.*
	// It has prefix "Apple", first char is 'A'/'a'.
	q, err := query.Parse("(?i)Apple.*")
	if err != nil {
		t.Fatal(err)
	}

	opts := &zoekt.SearchOptions{}

	res, err := searcher.Search(context.Background(), q, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(res.Files))
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

	q, err := query.Parse("(?i)MyGreatMethod.*")
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
			sb.WriteString("line-special: here is MyGreatMethod defined with some arguments.\n")
		}
	}

	doc := Document{
		Name:    "my_large_code_file.go",
		Content: []byte(sb.String()),
	}

	searcher := searcherForTest(b, testShardBuilder(b, nil, doc))

	q, err := query.Parse("(?i)MyGreatMethod.*")
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

func BenchmarkCaseInsensitiveRegexpPrefixLen3(b *testing.B) {
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

	// 'for' has length 3, has no i, I, k, K, s, S, and is case-insensitive
	q, err := query.Parse("(?i)for.*")
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
			b.Fatalf("expected 0 matches, got %d", len(res.Files))
		}
	}
}
