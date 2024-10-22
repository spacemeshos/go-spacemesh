package rangesync

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var seqTestErr = errors.New("test error")

type fakeSeqItem struct {
	k    string
	err  error
	stop bool
}

func mkFakeSeqItem(s string) fakeSeqItem {
	switch s {
	case "!":
		return fakeSeqItem{err: seqTestErr}
	case "$":
		return fakeSeqItem{stop: true}
	default:
		return fakeSeqItem{k: s}
	}
}

type fakeSeq []fakeSeqItem

func mkFakeSeq(s string) fakeSeq {
	seq := make(fakeSeq, len(s))
	for n, c := range s {
		seq[n] = mkFakeSeqItem(string(c))
	}
	return seq
}

func (seq fakeSeq) items(startIdx int) SeqResult {
	if startIdx > len(seq) {
		panic("bad startIdx")
	}
	var err error
	return SeqResult{
		Seq: func(yield func(KeyBytes) bool) {
			err = nil
			if len(seq) == 0 {
				return
			}
			n := startIdx
			for {
				if n == len(seq) {
					n = 0
				}
				item := seq[n]
				if item.err != nil {
					err = item.err
					return
				}
				if item.stop || !yield(KeyBytes(item.k)) {
					return
				}
				n++
			}
		},
		Error: func() error {
			return err
		},
	}
}

func seqToStr(t *testing.T, sr SeqResult) string {
	var sb strings.Builder
	var firstK KeyBytes
	wrap := 0
	var s string
	for k := range sr.Seq {
		require.NoError(t, sr.Error())
		if wrap != 0 {
			// after wraparound, make sure the sequence is repeated
			if k.Compare(firstK) == 0 {
				// arrived to the element for the second time
				return s
			}
			require.Equal(t, s[wrap], k[0])
			wrap++
			continue
		}
		require.NotNil(t, k)
		if firstK == nil {
			firstK = k
		} else if k.Compare(firstK) == 0 {
			s = sb.String() // wraparound
			wrap = 1
			continue
		}
		sb.Write(k)
	}
	if err := sr.Error(); err != nil {
		require.Equal(t, seqTestErr, err)
		sb.WriteString("!") // error
		return sb.String()
	}
	return sb.String() + "$" // stop
}

func TestCombineSeqs(t *testing.T) {
	for _, tc := range []struct {
		// In each seq, $ means the end of sequence (lack of $ means wraparound),
		// and ! means an error.
		seqs          []string
		indices       []int
		result        string
		startingPoint string
	}{
		{
			seqs:          []string{"abcd"},
			indices:       []int{0},
			result:        "abcd",
			startingPoint: "a",
		},
		{
			seqs:          []string{"abcd"},
			indices:       []int{0},
			result:        "abcd",
			startingPoint: "c",
		},
		{
			seqs:          []string{"abcd"},
			indices:       []int{2},
			result:        "cdab",
			startingPoint: "c",
		},
		{
			seqs:          []string{"abcd$"},
			indices:       []int{0},
			result:        "abcd$",
			startingPoint: "a",
		},
		{
			seqs:          []string{"abcd!"},
			indices:       []int{0},
			result:        "abcd!",
			startingPoint: "a",
		},
		{
			seqs:          []string{"abcd", "efgh"},
			indices:       []int{0, 0},
			result:        "abcdefgh",
			startingPoint: "a",
		},
		{
			seqs:          []string{"aceg", "bdfh"},
			indices:       []int{0, 0},
			result:        "abcdefgh",
			startingPoint: "a",
		},
		{
			seqs:          []string{"abcd$", "efgh$"},
			indices:       []int{0, 0},
			result:        "abcdefgh$",
			startingPoint: "a",
		},
		{
			seqs:          []string{"aceg$", "bdfh$"},
			indices:       []int{0, 0},
			result:        "abcdefgh$",
			startingPoint: "a",
		},
		{
			seqs:          []string{"abcd!", "efgh!"},
			indices:       []int{0, 0},
			result:        "abcd!",
			startingPoint: "a",
		},
		{
			seqs:          []string{"aceg!", "bdfh!"},
			indices:       []int{0, 0},
			result:        "abcdefg!",
			startingPoint: "a",
		},
		{
			// wraparound:
			// "ac"+"bdefgh"
			// abcdefgh ==>
			//    defghabc
			// starting point is d.
			// Each sequence must either start after the starting point, or
			// all of its elements are considered to be below the starting
			// point.  "ac" is considered to be wrapped around initially
			seqs:          []string{"ac", "bdefgh"},
			indices:       []int{0, 1},
			result:        "defghabc",
			startingPoint: "d",
		},
		{
			seqs:          []string{"bc", "ae"},
			indices:       []int{0, 1},
			result:        "eabc",
			startingPoint: "d",
		},
		{
			seqs:          []string{"ac", "bfg", "deh"},
			indices:       []int{0, 0, 0},
			result:        "abcdefgh",
			startingPoint: "a",
		},
		{
			seqs:          []string{"abdefgh", "c"},
			indices:       []int{0, 0},
			result:        "abcdefgh",
			startingPoint: "a",
		},
	} {
		t.Run("", func(t *testing.T) {
			var seqs []SeqResult
			for n, s := range tc.seqs {
				seqs = append(seqs, mkFakeSeq(s).items(tc.indices[n]))
			}
			startingPoint := KeyBytes(tc.startingPoint)
			combined := CombineSeqs(startingPoint, seqs...)
			for range 3 { // make sure the sequence is reusable
				require.Equal(t, tc.result, seqToStr(t, combined),
					"combine %v (indices %v) starting with %s",
					tc.seqs, tc.indices, tc.startingPoint)
			}
		})
	}
}
