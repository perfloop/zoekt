package index

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
)

// TestPostingsBuilderDeltaEncoding checks that postings remain encoded exactly
// like binary.PutUvarint at the one-, two-, and three-byte boundaries.
func TestPostingsBuilderDeltaEncoding(t *testing.T) {
	for _, delta := range []uint32{
		1,
		2,
		3,
		0x7e,
		0x7f,
		0x80,
		0x81,
		0x3ffe,
		0x3fff,
		0x4000,
	} {
		t.Run("delta="+strconv.FormatUint(uint64(delta), 10), func(t *testing.T) {
			data, trigram := repeatedTrigram(delta)
			pb := newPostingsBuilder(defaultShardMax)
			if _, _, err := pb.newSearchableString(data, nil); err != nil {
				t.Fatal(err)
			}

			pl := pb.asciiPostings[asciiNgramIndex(trigram[0], trigram[1], trigram[2])]
			if pl == nil {
				t.Fatal("missing posting list")
			}

			var buf [binary.MaxVarintLen64]byte
			want := append([]byte{0}, buf[:binary.PutUvarint(buf[:], uint64(delta))]...)
			if !bytes.Equal(pl.data, want) {
				t.Fatalf("encoded delta: got %x, want %x", pl.data, want)
			}
		})
	}
}

func repeatedTrigram(delta uint32) ([]byte, [3]byte) {
	switch delta {
	case 1:
		return []byte("aaaa"), [3]byte{'a', 'a', 'a'}
	case 2:
		return []byte("ababa"), [3]byte{'a', 'b', 'a'}
	default:
		return []byte("abc" + strings.Repeat("x", int(delta)-3) + "abc"), [3]byte{'a', 'b', 'c'}
	}
}
