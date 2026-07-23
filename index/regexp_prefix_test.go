package index

import (
	"context"
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/internal/hybridre2"
	"github.com/sourcegraph/zoekt/query"
)

func regexpPrefixRanges(t *testing.T, pattern string, content []byte) [][2]uint32 {
	t.Helper()

	searcher := searcherForTest(t, testShardBuilder(t, nil, Document{
		Name:    "regexp-prefix.txt",
		Content: content,
	}))
	q, err := query.Parse(pattern)
	if err != nil {
		t.Fatal(err)
	}
	res, err := searcher.Search(context.Background(), q, &chunkOpts)
	if err != nil {
		t.Fatal(err)
	}

	got := make([][2]uint32, 0)
	for _, file := range res.Files {
		for _, match := range file.ChunkMatches {
			for _, r := range match.Ranges {
				got = append(got, [2]uint32{r.Start.ByteOffset, r.End.ByteOffset})
			}
		}
	}
	return got
}

func regexpRanges(pattern string, content []byte) [][2]uint32 {
	idxs := hybridre2.MustCompile(pattern).FindAllIndex(content, -1)
	got := make([][2]uint32, len(idxs))
	for i, idx := range idxs {
		got[i] = [2]uint32{uint32(idx[0]), uint32(idx[1])}
	}
	return got
}

func TestRegexpPrefixMatchesHybridRegexp(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		content string
	}{
		{
			name:    "mixed case matches",
			pattern: "(?i)MyAwesomeFunction.*",
			content: "mYaWeSoMeFuNcTiOn one\nMyAwesomeFunction two\n",
		},
		{
			name:    "variable repetition",
			pattern: "(?i)(abc+)def",
			content: "abccdef\n",
		},
		{
			name:    "variable wildcard",
			pattern: "(?i)(abc.*)def",
			content: "abcxyzdef\n",
		},
		{
			name:    "character class after literal",
			pattern: "(?i)MyAwesomeFunction[0-9]+",
			content: "MyAwesomeFunctionX MyAwesomeFunction9\n",
		},
		{
			name:    "repeated character class misses",
			pattern: "(?i)MyAwesomeFunction[0-9]+",
			content: "MyAwesomeFunctionX MyAwesomeFunctionY MyAwesomeFunction9\n",
		},
		{
			name:    "non-greedy suffix",
			pattern: "(?i)foo.*?",
			content: "foo suffix\n",
		},
		{
			name:    "budget fallback after a match",
			pattern: "(?i)" + strings.Repeat("A", 127) + "B.*",
			content: strings.Repeat("A", 127) + "B\n" +
				strings.Repeat(strings.Repeat("A", 126)+"BC", 4) + strings.Repeat("x", 2048),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte(tc.content)
			want := regexpRanges(tc.pattern, content)
			got := regexpPrefixRanges(t, tc.pattern, content)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("match ranges differ (-want +got):\n%s", diff)
			}
		})
	}
}

func regexpMatchTreeRanges(t *testing.T, pattern string, content []byte, usePrefix bool) [][2]uint32 {
	t.Helper()

	q, err := query.Parse(pattern)
	if err != nil {
		t.Fatal(err)
	}
	re, ok := q.(*query.Regexp)
	if !ok {
		t.Fatalf("query type = %T, want *query.Regexp", q)
	}
	return regexpMatchTreeRangesForRegexp(t, re, content, usePrefix)
}

func regexpMatchTreeRangesForRegexp(t *testing.T, re *query.Regexp, content []byte, usePrefix bool) [][2]uint32 {
	t.Helper()

	searcher := searcherForTest(t, testShardBuilder(t, nil, Document{
		Name:    "regexp-prefix.txt",
		Content: content,
	}))
	id, ok := searcher.(*indexData)
	if !ok {
		t.Fatalf("searcher type = %T, want *indexData", searcher)
	}

	mt := newRegexpMatchTree(re)
	if !usePrefix {
		mt.hasPrefix = false
	}
	cp := &contentProvider{id: id, stats: &zoekt.Stats{}}
	cp.setDocument(0)
	mt.matches(cp, costRegexp, nil)

	got := make([][2]uint32, len(mt.found))
	for i, match := range mt.found {
		got[i] = [2]uint32{match.byteOffset, match.byteOffset + match.byteMatchSz}
	}
	return got
}

func TestRegexpPrefixDirectMatchRanges(t *testing.T) {
	const pattern = "(?i)MyAwesomeFunction.*"
	content := []byte(strings.Repeat("x", 1024) + "\n" +
		"mYaWeSoMeFuNcTiOn one\nMyAwesomeFunction two\n")
	q, err := query.Parse(pattern)
	if err != nil {
		t.Fatal(err)
	}
	re, ok := q.(*query.Regexp)
	if !ok {
		t.Fatalf("query type = %T, want *query.Regexp", q)
	}
	if !newRegexpMatchTree(re).hasPrefix {
		t.Fatal("expected direct regexp prefix path")
	}

	want := regexpMatchTreeRanges(t, pattern, content, false)
	got := regexpMatchTreeRanges(t, pattern, content, true)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("direct match ranges differ (-want +got):\n%s", diff)
	}
}

func TestRegexpPrefixDirectBareLiteralRanges(t *testing.T) {
	const pattern = "(?i)ABAB"
	content := []byte(strings.Repeat("x", 1024) + "aBaBaBaB")
	syntaxRe, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	re := &query.Regexp{Regexp: syntaxRe}
	if !newRegexpMatchTree(re).hasPrefix {
		t.Fatal("expected direct bare-literal regexp path")
	}

	want := regexpMatchTreeRangesForRegexp(t, re, content, false)
	got := regexpMatchTreeRangesForRegexp(t, re, content, true)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("bare literal match ranges differ (-want +got):\n%s", diff)
	}
}

func TestRegexpPrefixMatchesFullEngineForUnicodeFolds(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		content string
	}{
		{
			name:    "Kelvin sign",
			pattern: "(?i)kebab.*",
			content: "Kebab scale\n",
		},
		{
			name:    "long s",
			pattern: "(?i)smart.*",
			content: "ſmart search\n",
		},
		{
			name:    "dotted I",
			pattern: "(?i)image.*",
			content: "İmage search\n",
		},
		{
			name:    "dotless I",
			pattern: "(?i)impact.*",
			content: "ımpact search\n",
		},
		{
			name:    "interior Kelvin sign",
			pattern: "(?i)bake.*",
			content: "baKe search\n",
		},
		{
			name:    "interior long s",
			pattern: "(?i)mask.*",
			content: "maſk search\n",
		},
		{
			name:    "interior dotted I",
			pattern: "(?i)mile.*",
			content: "mİle search\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte(tc.content)
			want := regexpMatchTreeRanges(t, tc.pattern, content, false)
			got := regexpMatchTreeRanges(t, tc.pattern, content, true)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("match ranges differ (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRegexpPrefixEligibility(t *testing.T) {
	cases := []struct {
		pattern       string
		caseSensitive bool
		want          bool
	}{
		{pattern: "(?i)MyGreatMethod.*", want: true},
		{pattern: "(?i)for.*", want: false},
		{pattern: "(?i)smart.*", want: true},
		{pattern: "(?i)kebab.*", want: true},
		{pattern: "(?i)image.*", want: true},
		{pattern: "(?i)MyAwesomeFunction[0-9]+", want: false},
		{pattern: "(?i)MyAwesomeFunction.*?", want: false},
		{pattern: "(?i)éclair.*", want: false},
		{pattern: "(?i)^prefix.*", want: false},
		{pattern: "prefix.*", want: false},
		{pattern: "prefix.*", caseSensitive: true, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			q, err := query.Parse(tc.pattern)
			if err != nil {
				t.Fatal(err)
			}
			re, ok := q.(*query.Regexp)
			if !ok {
				t.Fatalf("query type = %T, want *query.Regexp", q)
			}
			re.CaseSensitive = tc.caseSensitive
			if got := newRegexpMatchTree(re).hasPrefix; got != tc.want {
				t.Fatalf("hasPrefix = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegexpPrefixScopedCaseDoesNotEnableByteMatcher(t *testing.T) {
	re, err := syntax.Parse("(?-i:foo).*", syntax.Perl|syntax.FoldCase)
	if err != nil {
		t.Fatal(err)
	}
	mt := newRegexpMatchTree(&query.Regexp{
		Regexp:        re,
		CaseSensitive: false,
	})
	if mt.hasPrefix {
		t.Fatal("scoped case-sensitive literal enabled the byte matcher")
	}
}

func TestAsciiFoldNeedleFind(t *testing.T) {
	cases := []struct {
		name     string
		needle   string
		haystack string
		want     []int
	}{
		{
			name:     "mixed case",
			needle:   "abc",
			haystack: "ABC abc aBc",
			want:     []int{0, 4, 8},
		},
		{
			name:     "overlapping candidates",
			needle:   "aab",
			haystack: "aaab aab",
			want:     []int{1, 5},
		},
		{
			name:     "no match",
			needle:   "Apple",
			haystack: strings.Repeat("A", 4096),
			want:     nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			needle := newAsciiFoldNeedle(tc.needle)
			data := []byte(tc.haystack)
			budget := len(data)
			cursor := newAsciiFoldCursor()
			var got []int
			for start := 0; ; {
				offset, exhausted := needle.find(data, start, &budget, &cursor)
				if exhausted {
					t.Fatal("comparison budget exhausted")
				}
				if offset < 0 {
					break
				}
				got = append(got, offset)
				start = offset + len(needle.targets)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("offsets differ (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAsciiFoldNeedleCursorKeepsAbsentCase(t *testing.T) {
	needle := newAsciiFoldNeedle("ABC")
	data := []byte(strings.Repeat("ABC", 4))
	budget := len(data)
	cursor := newAsciiFoldCursor()

	first, exhausted := needle.find(data, 0, &budget, &cursor)
	if exhausted || first != 0 {
		t.Fatalf("first match = %d, exhausted = %v", first, exhausted)
	}
	if want := len(data) - len(needle.targets) + 1; cursor.nextLower != want {
		t.Fatalf("nextLower = %d, want %d", cursor.nextLower, want)
	}

	second, exhausted := needle.find(data, len(needle.targets), &budget, &cursor)
	if exhausted || second != len(needle.targets) {
		t.Fatalf("second match = %d, exhausted = %v", second, exhausted)
	}
	if want := len(data) - len(needle.targets) + 1; cursor.nextLower != want {
		t.Fatalf("nextLower after second match = %d, want %d", cursor.nextLower, want)
	}
}

func TestAsciiFoldNeedleFindBoundsPartialMatches(t *testing.T) {
	needle := newAsciiFoldNeedle(strings.Repeat("A", 128) + "B")
	data := []byte(strings.Repeat("A", 4096))
	budget := len(data)
	cursor := newAsciiFoldCursor()
	_, exhausted := needle.find(data, 0, &budget, &cursor)
	if !exhausted {
		t.Fatal("expected the comparison budget to bound repeated partial matches")
	}
}
