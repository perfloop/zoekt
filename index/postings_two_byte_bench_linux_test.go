//go:build linux

package index

import (
	"bytes"
	"syscall"
	"testing"
)

// twoByteDeltaBlock is a cyclic order-three de Bruijn sequence over six ASCII
// bytes. Each trigram occurs once per 216-byte block, so repeating it makes
// nearly every posting delta exactly 216 and therefore a two-byte uvarint.
const twoByteDeltaBlock = "aaabaacaadaaeaafabbabcabdabeabfacbaccacdaceacfadbadcaddadeadfaebaecaedaeeaefafbafcafdafeaffbbbcbbdbbebbfbccbcdbcebcfbdcbddbdebdfbecbedbeebefbfcbfdbfebffcccdcceccfcddcdecdfcedceecefcfdcfecffdddeddfdeedefdfedffeeefefff"

func BenchmarkPostingsTwoByteDeltas(b *testing.B) {
	const repetitions = 4096
	data := bytes.Repeat([]byte(twoByteDeltaBlock), repetitions)
	pb := newPostingsBuilder(defaultShardMax)

	if _, _, err := pb.newSearchableString(data, nil); err != nil {
		b.Fatal(err)
	}
	pl := pb.asciiPostings[asciiNgramIndex('a', 'a', 'a')]
	if pl == nil {
		b.Fatal("missing aaa posting list")
	}
	if want := 1 + 2*(repetitions-1); len(pl.data) != want {
		b.Fatalf("aaa posting length: got %d, want %d", len(pl.data), want)
	}
	if len(pl.data) < 3 || pl.data[0] != 0 || pl.data[1] != 216|0x80 || pl.data[2] != 1 {
		b.Fatalf("aaa posting prefix: got %x, want 00d801", pl.data[:min(len(pl.data), 3)])
	}

	pb.reset()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()

	startCPU := benchmarkProcessCPUTime(b)
	var encodedBytes int
	for b.Loop() {
		if _, _, err := pb.newSearchableString(data, nil); err != nil {
			b.Fatal(err)
		}
		encodedBytes += len(pb.asciiPostings[asciiNgramIndex('a', 'a', 'a')].data)
		pb.reset()
	}
	cpuNanos := benchmarkProcessCPUTime(b) - startCPU
	b.ReportMetric(float64(cpuNanos)/float64(b.N), "cpu-ns/op")
	if encodedBytes == 0 {
		b.Fatal("benchmark produced no postings")
	}
}

func benchmarkProcessCPUTime(b *testing.B) int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		b.Fatal(err)
	}
	return usage.Utime.Sec*1e9 + usage.Utime.Usec*1e3 +
		usage.Stime.Sec*1e9 + usage.Stime.Usec*1e3
}
