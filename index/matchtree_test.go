// Copyright 2018 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package index

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"reflect"
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/google/go-cmp/cmp"
	"github.com/grafana/regexp"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/internal/hybridre2"
	"github.com/sourcegraph/zoekt/query"
)

func Test_breakOnNewlines(t *testing.T) {
	type args struct {
		cm   *candidateMatch
		text []byte
	}
	tests := []struct {
		name string
		args args
		want []*candidateMatch
	}{
		{
			name: "trivial case",
			args: args{
				cm: &candidateMatch{
					byteOffset:  0,
					byteMatchSz: 0,
				},
				text: nil,
			},
			want: nil,
		},
		{
			name: "no newlines",
			args: args{
				cm: &candidateMatch{
					byteOffset:  0,
					byteMatchSz: 1,
				},
				text: []byte("a"),
			},
			want: []*candidateMatch{
				{
					byteOffset:  0,
					byteMatchSz: 1,
				},
			},
		},
		{
			name: "newline at start",
			args: args{
				cm: &candidateMatch{
					byteOffset:  0,
					byteMatchSz: 2,
				},
				text: []byte("\na"),
			},
			want: []*candidateMatch{
				{
					byteOffset:  1,
					byteMatchSz: 1,
				},
			},
		},
		{
			name: "newline at end",
			args: args{
				cm: &candidateMatch{
					byteOffset:  0,
					byteMatchSz: 2,
				},
				text: []byte("a\n"),
			},
			want: []*candidateMatch{
				{
					byteOffset:  0,
					byteMatchSz: 1,
				},
			},
		},
		{
			name: "newline in middle",
			args: args{
				cm: &candidateMatch{
					byteOffset:  0,
					byteMatchSz: 3,
				},
				text: []byte("a\nb"),
			},
			want: []*candidateMatch{
				{
					byteOffset:  0,
					byteMatchSz: 1,
				},
				{
					byteOffset:  2,
					byteMatchSz: 1,
				},
			},
		},
		{
			name: "two newlines",
			args: args{
				cm: &candidateMatch{
					byteOffset:  0,
					byteMatchSz: 5,
				},
				text: []byte("a\nb\nc"),
			},
			want: []*candidateMatch{
				{
					byteOffset:  0,
					byteMatchSz: 1,
				},
				{
					byteOffset:  2,
					byteMatchSz: 1,
				},
				{
					byteOffset:  4,
					byteMatchSz: 1,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := breakOnNewlines(tt.args.cm, tt.args.text); !reflect.DeepEqual(got, tt.want) {
				type PrintableCm struct {
					byteOffset  uint32
					byteMatchSz uint32
				}
				var got2, want2 []PrintableCm
				for _, g := range got {
					got2 = append(got2, PrintableCm{byteOffset: g.byteOffset, byteMatchSz: g.byteMatchSz})
				}
				for _, w := range tt.want {
					want2 = append(want2, PrintableCm{byteOffset: w.byteOffset, byteMatchSz: w.byteMatchSz})
				}
				t.Errorf("breakMatchOnNewlines() = %+v, want %+v", got2, want2)
			}
		})
	}
}

func TestEquivalentQuerySkipRegexpTree(t *testing.T) {
	tests := []struct {
		query string
		skip  bool
	}{
		{query: "^foo", skip: false},
		{query: "foo", skip: true},
		{query: "thread|needle|haystack", skip: true},
		{query: "contain(er|ing)", skip: false},
		{query: "thread (needle|haystack)", skip: true},
		{query: "thread (needle|)", skip: false},
		{query: `\bthread\b case:yes`, skip: true}, // word search
		{query: `\bthread\b case:no`, skip: false},
	}

	for _, tt := range tests {
		q, err := query.Parse(tt.query)
		if err != nil {
			t.Errorf("Error parsing query: %s", "sym:"+tt.query)
			continue
		}

		d := &indexData{}
		mt, err := d.newMatchTree(q, matchTreeOpt{})
		if err != nil {
			t.Errorf("Error creating match tree from query: %s", q)
			continue
		}

		visitMatchTree(mt, func(m matchTree) {
			if _, ok := m.(*regexpMatchTree); ok && tt.skip {
				t.Log(mt)
				t.Errorf("Expected regexpMatchTree to be skipped for query: %s", q)
			}
		})
	}
}

// Test whether we skip the regexp engine for queries like "\bLITERAL\b
// case:yes"
func TestWordSearchSkipRegexpTree(t *testing.T) {
	qStr := "\\bfoo\\b case:yes"
	q, err := query.Parse(qStr)
	if err != nil {
		t.Fatalf("Error parsing query: %s", "sym:"+qStr)
	}

	d := &indexData{}
	mt, err := d.newMatchTree(q, matchTreeOpt{})
	if err != nil {
		t.Fatalf("Error creating match tree from query: %s", q)
	}

	countRegexMatchTree, countWordMatchTree := 0, 0
	visitMatchTree(mt, func(m matchTree) {
		switch m.(type) {
		case *regexpMatchTree:
			countRegexMatchTree++
		case *wordMatchTree:
			countWordMatchTree++
		}
	})

	if countRegexMatchTree != 0 {
		t.Fatalf("expected to find 0 regexMatchTree, found %d", countRegexMatchTree)
	}

	if countWordMatchTree != 1 {
		t.Fatalf("expected to find 1 wordMatchTree, found %d", countWordMatchTree)
	}
}

func TestSymbolMatchTree(t *testing.T) {
	tests := []struct {
		query    string
		substr   string
		regex    string
		regexAll bool
	}{
		{query: "sym:.*", regex: "(?i)(?-s:.)*", regexAll: true},
		{query: "sym:(ab|cd)", regex: "(?i)ab|cd"},
		{query: "sym:b.r", regex: "(?i)b(?-s:.)r"},
		{query: "sym:horse", substr: "horse"},
		{query: `sym:\bthread\b case:yes`, regex: `\bthread\b`}, // check we disable word search opt
		{query: `sym:\bthread\b case:no`, regex: `(?i)\bthread\b`},
	}

	for _, tt := range tests {
		q, err := query.Parse(tt.query)
		if err != nil {
			t.Errorf("Error parsing query: %s", tt.query)
			continue
		}

		d := &indexData{}
		mt, err := d.newMatchTree(q, matchTreeOpt{})
		if err != nil {
			t.Errorf("Error creating match tree from query: %s", q)
			continue
		}

		var (
			substr   string
			regex    string
			regexAll bool
		)
		if substrMT, ok := mt.(*symbolSubstrMatchTree); ok {
			substr = substrMT.query.Pattern
		}
		if regexMT, ok := mt.(*symbolRegexpMatchTree); ok {
			regex = regexMT.regexp.String()
			regexAll = regexMT.all
		}

		if substr != tt.substr {
			t.Errorf("%s has unexpected substring:\nwant: %q\ngot:  %q", tt.query, tt.substr, substr)
		}
		if regex != tt.regex {
			t.Errorf("%s has unexpected regex:\nwant: %q\ngot:  %q", tt.query, tt.regex, regex)
		}
		if regexAll != tt.regexAll {
			t.Errorf("%s has unexpected regexAll: want=%t got=%t", tt.query, tt.regexAll, regexAll)
		}
	}
}

func TestRepoSet(t *testing.T) {
	d := &indexData{
		repoMetaData:    []zoekt.Repository{{Name: "r0"}, {Name: "r1"}, {Name: "r2"}, {Name: "r3"}},
		fileBranchMasks: []uint64{1, 1, 1, 1, 1, 1},
		repos:           []uint16{0, 0, 1, 2, 3, 3},
	}
	mt, err := d.newMatchTree(&query.RepoSet{Set: map[string]bool{"r1": true, "r3": true, "r99": true}}, matchTreeOpt{})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{2, 4, 5}
	for i := range want {
		nextDoc := mt.nextDoc()
		if nextDoc != want[i] {
			t.Fatalf("want %d, got %d", want[i], nextDoc)
		}
		mt.prepare(nextDoc)
	}
	if mt.nextDoc() != math.MaxUint32 {
		t.Fatalf("expected %d document, but got at least 1 more", len(want))
	}
}

func TestRepo(t *testing.T) {
	d := &indexData{
		repoMetaData:    []zoekt.Repository{{Name: "foo"}, {Name: "bar"}},
		fileBranchMasks: []uint64{1, 1, 1, 1, 1},
		repos:           []uint16{0, 0, 1, 0, 1},
	}
	mt, err := d.newMatchTree(&query.Repo{Regexp: regexp.MustCompile("ar")}, matchTreeOpt{})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{2, 4}
	for i := range want {
		nextDoc := mt.nextDoc()
		if nextDoc != want[i] {
			t.Fatalf("want %d, got %d", want[i], nextDoc)
		}
		mt.prepare(nextDoc)
	}
	if mt.nextDoc() != math.MaxUint32 {
		t.Fatalf("expect %d documents, but got at least 1 more", len(want))
	}
}

func TestBranchesRepos(t *testing.T) {
	d := &indexData{
		repoMetaData: []zoekt.Repository{
			{ID: hash("foo"), Name: "foo"},
			{ID: hash("bar"), Name: "bar"},
		},
		fileBranchMasks: []uint64{1, 1, 1, 2, 1, 2, 1},
		repos:           []uint16{0, 0, 1, 1, 1, 1, 1},
		branchIDs:       []map[string]uint{{"HEAD": 1}, {"HEAD": 1, "b1": 2}},
	}

	mt, err := d.newMatchTree(&query.BranchesRepos{List: []query.BranchRepos{
		{Branch: "b1", Repos: roaring.BitmapOf(hash("bar"))},
		{Branch: "b2", Repos: roaring.BitmapOf(hash("bar"))},
	}}, matchTreeOpt{})
	if err != nil {
		t.Fatal(err)
	}

	want := []uint32{3, 5}
	for i := range want {
		nextDoc := mt.nextDoc()
		if nextDoc != want[i] {
			t.Fatalf("want %d, got %d", want[i], nextDoc)
		}
		mt.prepare(nextDoc)
	}

	if mt.nextDoc() != math.MaxUint32 {
		t.Fatalf("expect %d documents, but got at least 1 more", len(want))
	}
}

func TestRepoIDs(t *testing.T) {
	d := &indexData{
		repoMetaData:    []zoekt.Repository{{Name: "r0", ID: 0}, {Name: "r1", ID: 1}, {Name: "r2", ID: 2}, {Name: "r3", ID: 3}},
		fileBranchMasks: []uint64{1, 1, 1, 1, 1, 1},
		repos:           []uint16{0, 0, 1, 2, 3, 3},
	}
	mt, err := d.newMatchTree(&query.RepoIDs{Repos: roaring.BitmapOf(1, 3, 99)}, matchTreeOpt{})
	if err != nil {
		t.Fatal(err)
	}

	want := []uint32{2, 4, 5}
	for i := range want {
		nextDoc := mt.nextDoc()
		if nextDoc != want[i] {
			t.Fatalf("want %d, got %d", want[i], nextDoc)
		}
		mt.prepare(nextDoc)
	}
	if mt.nextDoc() != math.MaxUint32 {
		t.Fatalf("expected %d document, but got at least 1 more", len(want))
	}
}

func TestIsRegexpAll(t *testing.T) {
	valid := []string{
		".*",
		"(.*)",
		"(?-s:.*)",
		"(?s:.*)",
		"(?i)(?-s:.*)",
	}
	invalid := []string{
		".",
		"foo",
		"(foo.*)",
	}

	for _, s := range valid {
		r, err := syntax.Parse(s, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		if !isRegexpAll(r) {
			t.Errorf("expected %q to match all", s)
		}
	}

	for _, s := range invalid {
		r, err := syntax.Parse(s, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		if isRegexpAll(r) {
			t.Errorf("expected %q to not match all", s)
		}
	}
}

func TestMetaQueryMatchTree(t *testing.T) {
	d := &indexData{
		repoMetaData: []zoekt.Repository{
			{Name: "r0", Metadata: map[string]string{"license": "Apache-2.0"}},
			{Name: "r1", Metadata: map[string]string{"license": "MIT"}},
			{Name: "r2"}, // no metadata
			{Name: "r3", Metadata: map[string]string{"haystack": "needle"}},
			{Name: "r4", Metadata: map[string]string{"note": "test"}},
		},
		fileBranchMasks:   []uint64{1, 1, 1, 1, 1}, // 5 docs
		repos:             []uint16{0, 1, 2, 3, 4}, // map docIDs to repos
		docMatchTreeCache: newDocMatchTreeCache(1), // small cache to test eviction
	}

	q := &query.Meta{
		Field: "license",
		Value: regexp.MustCompile("M.T"),
	}

	mt, err := d.newMatchTree(q, matchTreeOpt{})
	if err != nil {
		t.Fatalf("failed to build matchTree: %v", err)
	}

	// Check that the docMatchTree cache is populated correctly
	checksum := queryMetaChecksum("license", regexp.MustCompile("M.T"))
	cacheKeyField := "Meta"
	if _, ok := d.docMatchTreeCache.Get(cacheKeyField, checksum); !ok {
		t.Errorf("expected docMatchTreeCache to be populated for key (%q, %q)", cacheKeyField, checksum)
	}

	var matched []uint32
	for {
		doc := mt.nextDoc()
		if doc == math.MaxUint32 {
			break
		}
		matched = append(matched, doc)
		mt.prepare(doc)
	}

	want := []uint32{1} // only doc from r1 should match
	if !reflect.DeepEqual(matched, want) {
		t.Errorf("meta match failed: got %v, want %v", matched, want)
	}
}

func Test_queryMetaCacheKey(t *testing.T) {
	cases := []struct {
		field   string
		pattern string
		wantKey string
	}{
		{"metaField", "foo.*bar", "24e88a5ffec04af0"},
		{"metaField", "foo.*baz", "d8d6f6a7f0725b61"},
		{"otherField", "foo.*bar", "c9d07e17c028364"},
	}
	for _, tc := range cases {
		re := regexp.MustCompile(tc.pattern)
		key := queryMetaChecksum(tc.field, re)
		if key != tc.wantKey {
			t.Errorf("unexpected key for field=%q pattern=%q: got %q, want %q", tc.field, tc.pattern, key, tc.wantKey)
		}
	}
}

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
		{
			name:    "long literal budget fallback",
			pattern: "(?i)QwertyUiopAbcd.*",
			content: strings.Repeat("QwertyUiopAbcX\n", 6000) +
				"QwertyUiopAbcd final\n",
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

	mt := newRegexpMatchTree(re, id.metaData.PlainASCII, false)
	if !usePrefix {
		mt.foldedLiteral = nil
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
	if newRegexpMatchTree(re, true, false).foldedLiteral == nil {
		t.Fatal("expected direct regexp prefix path")
	}

	want := regexpMatchTreeRanges(t, pattern, content, false)
	got := regexpMatchTreeRanges(t, pattern, content, true)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("direct match ranges differ (-want +got):\n%s", diff)
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
			pattern: "(?i)KelvinPrefixX.*",
			content: "KelvinPrefixX scale\n",
		},
		{
			name:    "long s",
			pattern: "(?i)smartprefixxx.*",
			content: "ſmartprefixxx search\n",
		},
		{
			name:    "dotted I",
			pattern: "(?i)imageprefixxx.*",
			content: "İmageprefixxx search\n",
		},
		{
			name:    "dotless I",
			pattern: "(?i)impactprefixx.*",
			content: "ımpactprefixx search\n",
		},
		{
			name:    "interior Kelvin sign",
			pattern: "(?i)bakelongprefix.*",
			content: "baKelongprefix search\n",
		},
		{
			name:    "interior long s",
			pattern: "(?i)masklongprefix.*",
			content: "maſklongprefix search\n",
		},
		{
			name:    "interior dotted I",
			pattern: "(?i)milelongprefix.*",
			content: "mİlelongprefix search\n",
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

func TestRegexpFastPathIgnoresForgedPlainASCIIMetadata(t *testing.T) {
	if os.Getenv("ZOEKT_RE2_THRESHOLD_BYTES") != "0" {
		env := os.Environ()
		configured := false
		for i, entry := range env {
			if strings.HasPrefix(entry, "ZOEKT_RE2_THRESHOLD_BYTES=") {
				env[i] = "ZOEKT_RE2_THRESHOLD_BYTES=0"
				configured = true
				break
			}
		}
		if !configured {
			env = append(env, "ZOEKT_RE2_THRESHOLD_BYTES=0")
		}

		cmd := exec.Command(os.Args[0], "-test.run=^TestRegexpFastPathIgnoresForgedPlainASCIIMetadata$")
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("RE2 subprocess failed: %v\n%s", err, output)
		}
		return
	}

	const pattern = "(?i)KelvinPrefixX.*"
	content := []byte("KelvinPrefixX scale\n")
	builder := testShardBuilder(t, nil, Document{
		Name:    "kelvin.go",
		Content: content,
	})

	var referenceData bytes.Buffer
	if err := builder.Write(&referenceData); err != nil {
		t.Fatal(err)
	}
	reference, err := NewSearcher(&memSeeker{data: referenceData.Bytes()})
	if err != nil {
		t.Fatalf("NewSearcher(reference): %v", err)
	}
	defer reference.Close()

	if len(builder.contentPostings.endRunes) != 1 {
		t.Fatalf("content end-rune entries = %d, want 1", len(builder.contentPostings.endRunes))
	}
	builder.contentPostings.isPlainASCII = true
	builder.contentPostings.endRunes[0] = uint32(len(content))

	var forgedData bytes.Buffer
	if err := builder.Write(&forgedData); err != nil {
		t.Fatal(err)
	}
	forged, err := NewSearcher(&memSeeker{data: forgedData.Bytes()})
	if err != nil {
		t.Fatalf("NewSearcher(forged): %v", err)
	}
	defer forged.Close()

	q, err := query.Parse(pattern)
	if err != nil {
		t.Fatal(err)
	}
	findRanges := func(s zoekt.Searcher) [][2]uint32 {
		res, err := s.Search(context.Background(), q, &zoekt.SearchOptions{ChunkMatches: true})
		if err != nil {
			t.Fatal(err)
		}
		var ranges [][2]uint32
		for _, file := range res.Files {
			for _, chunk := range file.ChunkMatches {
				for _, r := range chunk.Ranges {
					ranges = append(ranges, [2]uint32{r.Start.ByteOffset, r.End.ByteOffset})
				}
			}
		}
		return ranges
	}

	want := findRanges(reference)
	if len(want) == 0 {
		t.Fatal("RE2 reference did not match the Kelvin-sign spelling")
	}
	if got := findRanges(forged); !reflect.DeepEqual(got, want) {
		t.Fatalf("ranges differ: got %v, want %v", got, want)
	}
}

func TestRegexpPrefixNonASCIIShardDoesNotEnableByteMatcher(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{pattern: "(?i)smartprefixxx.*", want: false},
		{pattern: "(?i)KelvinPrefixX.*", want: false},
		{pattern: "(?i)imageprefixxx.*", want: false},
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
			if got := newRegexpMatchTree(re, false, false).foldedLiteral != nil; got != tc.want {
				t.Fatalf("has direct needle = %v, want %v", got, tc.want)
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
		{pattern: "(?i)abcdefghijkl.*", want: false},
		{pattern: "(?i)" + strings.Repeat(`\.`, 12) + ".*", want: false},
		{pattern: "(?i)abcdefghijklm.*", want: true},
		{pattern: "(?i)ABCD.*", want: false},
		{pattern: "(?i)for.*", want: false},
		{pattern: "(?i)smart.*", want: false},
		{pattern: "(?i)kebab.*", want: false},
		{pattern: "(?i)image.*", want: false},
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
			if got := newRegexpMatchTree(re, true, false).foldedLiteral != nil; got != tc.want {
				t.Fatalf("has direct needle = %v, want %v", got, tc.want)
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
	}, true, false)
	if mt.foldedLiteral != nil {
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
			needle := asciiFoldNeedleFromRunes([]rune(tc.needle))
			data := []byte(tc.haystack)
			budget := len(data) * needle.len()
			cursor := asciiFoldCursor{nextLower: -1, nextUpper: -1}
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
				start = offset + needle.len()
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("offsets differ (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAsciiFoldNeedleSwitchesToSecondByteAnchor(t *testing.T) {
	needle := asciiFoldNeedleFromRunes([]rune("Abcdefghijklm"))
	data := []byte(strings.Repeat("Ax", firstByteMissLimit) + "aBcdefghijklm")
	budget := len(data)
	cursor := asciiFoldCursor{
		nextLower:       -1,
		nextUpper:       -1,
		nextSecondLower: -1,
		nextSecondUpper: -1,
	}

	got, exhausted := needle.find(data, 0, &budget, &cursor)
	if exhausted {
		t.Fatal("comparison budget exhausted")
	}
	if want := firstByteMissLimit * len("Ax"); got != want {
		t.Fatalf("match offset = %d, want %d", got, want)
	}
	if !cursor.useSecond {
		t.Fatal("expected the second-byte anchor after repeated first-byte misses")
	}
}

func TestAsciiFoldNeedleSecondAnchorChecksFirstByte(t *testing.T) {
	const pattern = "(?i)Abcdefghijklm.*"
	content := []byte(strings.Repeat("Ax", firstByteMissLimit) +
		"xBcdefghijklm forged\n" + strings.Repeat("z", 2048))
	needle := asciiFoldNeedleFromRunes([]rune("Abcdefghijklm"))
	budget := len(content) / 16
	cursor := asciiFoldCursor{
		nextLower:       -1,
		nextUpper:       -1,
		nextSecondLower: -1,
		nextSecondUpper: -1,
	}

	offset, exhausted := needle.find(content, 0, &budget, &cursor)
	if exhausted {
		t.Fatal("comparison budget exhausted")
	}
	if offset != -1 {
		t.Fatalf("forged second-anchor match offset = %d, want -1", offset)
	}
	if !cursor.useSecond {
		t.Fatal("expected the second-byte anchor after repeated first-byte misses")
	}
	if want := len(content)/16 - firstByteMissLimit - 1; budget != want {
		t.Fatalf("comparison budget = %d, want %d", budget, want)
	}

	want := regexpMatchTreeRanges(t, pattern, content, false)
	got := regexpMatchTreeRanges(t, pattern, content, true)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("second-anchor ranges differ (-want +got):\n%s", diff)
	}
}

func TestAsciiFoldNeedleCursorKeepsAbsentCase(t *testing.T) {
	needle := asciiFoldNeedleFromRunes([]rune("ABC"))
	data := []byte(strings.Repeat("ABC", 4))
	budget := len(data)
	cursor := asciiFoldCursor{nextLower: -1, nextUpper: -1}

	first, exhausted := needle.find(data, 0, &budget, &cursor)
	if exhausted || first != 0 {
		t.Fatalf("first match = %d, exhausted = %v", first, exhausted)
	}
	if want := len(data) - needle.len() + 1; cursor.nextLower != want {
		t.Fatalf("nextLower = %d, want %d", cursor.nextLower, want)
	}

	second, exhausted := needle.find(data, needle.len(), &budget, &cursor)
	if exhausted || second != needle.len() {
		t.Fatalf("second match = %d, exhausted = %v", second, exhausted)
	}
	if want := len(data) - needle.len() + 1; cursor.nextLower != want {
		t.Fatalf("nextLower after second match = %d, want %d", cursor.nextLower, want)
	}
}

func TestAsciiFoldNeedleFindBoundsPartialMatches(t *testing.T) {
	needle := asciiFoldNeedleFromRunes([]rune(strings.Repeat("A", 128) + "B"))
	data := []byte(strings.Repeat("A", 4096))
	budget := 64
	cursor := asciiFoldCursor{nextLower: -1, nextUpper: -1}
	_, exhausted := needle.find(data, 0, &budget, &cursor)
	if !exhausted {
		t.Fatal("expected the comparison budget to bound repeated partial matches")
	}
}
