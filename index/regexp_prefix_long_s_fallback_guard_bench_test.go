package index

import (
	"context"
	"strings"
	"testing"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/internal/hybridre2"
	"github.com/sourcegraph/zoekt/query"
)

func BenchmarkCaseInsensitiveRegexpPrefixLongSFoldFallback(b *testing.B) {
	const (
		pattern = "(?i)smartprefixxx.*"
		size    = 1 << 20
		line    = "func generated(value int) int { return value + 1 } // ordinary source text\n"
		trigger = "smartprefixxx ascii trigger\n"
		target  = "ſmartprefixxx unicode fallback\n"
	)

	var content strings.Builder
	content.Grow(size)
	content.WriteString(trigger)
	for content.Len()+len(line)+len(target) <= size {
		content.WriteString(line)
	}
	content.WriteString(strings.Repeat("x", size-content.Len()-len(target)))
	content.WriteString(target)

	benchmarkLongSFoldFallbackSearch(b, []byte(content.String()), pattern)
}

func benchmarkLongSFoldFallbackSearch(b *testing.B, content []byte, pattern string) {
	b.Helper()

	searcher := searcherForTest(b, testShardBuilder(b, nil, Document{
		Name:    "regexp-prefix-long-s-fallback.go",
		Content: content,
	}))
	q, err := query.Parse(pattern)
	if err != nil {
		b.Fatal(err)
	}
	want := hybridre2.MustCompile(pattern).FindAllIndex(content, -1)
	if len(want) == 0 {
		b.Fatal("reference regexp did not match the benchmark document")
	}
	ctx := context.Background()
	opts := &zoekt.SearchOptions{ChunkMatches: true}

	run := func(checkRanges bool) int {
		res, err := searcher.Search(ctx, q, opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Files) != 1 || res.MatchCount != len(want) {
			b.Fatalf("got %d files and %d matches, want 1 file and %d matches", len(res.Files), res.MatchCount, len(want))
		}
		if checkRanges {
			var got [][2]uint32
			for _, file := range res.Files {
				for _, chunk := range file.ChunkMatches {
					for _, r := range chunk.Ranges {
						got = append(got, [2]uint32{r.Start.ByteOffset, r.End.ByteOffset})
					}
				}
			}
			if len(got) != len(want) {
				b.Fatalf("got %d ranges, want %d", len(got), len(want))
			}
			for i, idx := range want {
				if got[i] != [2]uint32{uint32(idx[0]), uint32(idx[1])} {
					b.Fatalf("range %d = %v, want [%d %d]", i, got[i], idx[0], idx[1])
				}
			}
		}
		return res.MatchCount
	}

	run(true)
	b.SetBytes(int64(len(content)))
	b.ResetTimer()
	lastMatches := 0
	for b.Loop() {
		lastMatches = run(false)
	}
	b.StopTimer()
	b.ReportMetric(float64(lastMatches), "matches/op")
	b.ReportMetric(float64(len(content))/float64(lastMatches), "bytes/match")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(content)), "ns/input-byte")
}
