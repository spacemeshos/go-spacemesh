package rangesync

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

// MinhashSampleItem represents an item of minhash sample subset.
type MinhashSampleItem uint32

func (m MinhashSampleItem) String() string {
	return fmt.Sprintf("0x%08x", uint32(m))
}

func (m MinhashSampleItem) Compare(other any) int {
	return cmp.Compare(m, other.(MinhashSampleItem))
}

// EncodeScale implements scale.Encodable.
func (m MinhashSampleItem) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeUint32(e, uint32(m))
}

// DecodeScale implements scale.Decodable.
func (m *MinhashSampleItem) DecodeScale(d *scale.Decoder) (int, error) {
	v, total, err := scale.DecodeUint32(d)
	*m = MinhashSampleItem(v)
	return total, err
}

// MinhashSampleItemFromKeyBytes uses lower 32 bits of a hash as a MinhashSampleItem.
func MinhashSampleItemFromKeyBytes(h types.KeyBytes) MinhashSampleItem {
	if len(h) < 4 {
		h = append(h, make(types.KeyBytes, 4-len(h))...)
	}
	l := len(h)
	return MinhashSampleItem(uint32(h[l-4])<<24 + uint32(h[l-3])<<16 + uint32(h[l-2])<<8 + uint32(h[l-1]))
}

// Sample retrieves min(count, sampleSize) items friom the ordered sequence, extracting
// MinhashSampleItem from each value.
func Sample(seq types.Seq, count, sampleSize int) ([]MinhashSampleItem, error) {
	sampleSize = min(count, sampleSize)
	if sampleSize == 0 {
		return nil, nil
	}
	items := make([]MinhashSampleItem, 0, sampleSize)
	for k, err := range seq {
		if err != nil {
			return nil, err
		}
		items = append(items, MinhashSampleItemFromKeyBytes(k))
		if len(items) == sampleSize {
			break
		}
	}
	return items, nil
}

// CalcSim estimates the Jaccard similarity coefficient between two sets based on the
// samples, which are derived from N lowest-valued elements of each set.
// The return value is in 0..1 range, with 0 meaning almost no intersection and 1 meaning
// the sets are mostly equal.
// The precision of the estimate will suffer if none of the sets are empty and they have
// different size, with return value tending to be lower than the actual J coefficient.
func CalcSim(a, b []MinhashSampleItem) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	} else if len(a) == 0 || len(b) == 0 {
		return 0
	}

	// The sample items contain the lowest bits of the actual hash values, so their
	// order is initially random. We sort them here for easier comparison of the
	// samples.
	slices.SortFunc(b, func(a, b MinhashSampleItem) int { return a.Compare(b) })
	slices.SortFunc(a, func(a, b MinhashSampleItem) int { return a.Compare(b) })

	numEq := 0
	for m, n := 0, 0; m < len(a) && n < len(b); {
		d := a[m].Compare(b[n])
		switch {
		case d < 0:
			m++
		case d == 0:
			numEq++
			m++
			n++
		default:
			n++
		}
	}
	sampleSize := max(len(a), len(b))
	return float64(numEq) / float64(sampleSize)
}
