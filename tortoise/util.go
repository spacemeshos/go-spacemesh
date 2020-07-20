package tortoise

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type vec [2]int
type patternID uint32 //this hash does not include the layer id

const ( //Threshold
	window          = 10
	globalThreshold = 0.6
	genesis         = 0
)

var ( //correction vectors type
	//Opinion
	support     = vec{1, 0}
	against     = vec{0, 1}
	abstain     = vec{0, 0}
	zeroPattern = votingPattern{}
)

func max(i types.LayerID, j types.LayerID) types.LayerID {
	if i > j {
		return i
	}
	return j
}

func (a vec) Add(v vec) vec {
	return vec{a[0] + v[0], a[1] + v[1]}
}

func (a vec) Negate() vec {
	a[0] = a[0] * -1
	a[1] = a[1] * -1
	return a
}

func (a vec) Multiply(x int) vec {
	a[0] = a[0] * x
	a[1] = a[1] * x
	return a
}

func simplifyVote(v vec) vec {
	if v[0] > v[1] {
		return support
	}

	if v[1] > v[0] {
		return against
	}

	return abstain
}

func (a vec) String() string {
	v := simplifyVote(a)

	if v == support {
		return "support"
	}

	if v == against {
		return "against"
	}

	return "abstain"
}

func globalOpinion(v vec, layerSize int, delta float64) vec {
	threshold := float64(globalThreshold*delta) * float64(layerSize)
	if float64(v[0]) > threshold {
		return support
	} else if float64(v[1]) > threshold {
		return against
	} else {
		return abstain
	}
}

type opinion struct {
	blockIDLayerTuple
	blocksOpinion map[types.BlockID]vec
}

type voteInput struct {
	support bool // true,false = support
	against bool // false,true = against
	// false,false = abstain
	// true,true = invalid
}

func voteFromVec(ve vec) voteInput {
	v := simplifyVote(ve)
	return voteInput{
		support: v[0] == 1,
		against: v[1] == 1,
	}
}

func (vi voteInput) Vec() vec {
	if vi.support && !vi.against {
		return support
	}

	if vi.against && !vi.support {
		return support
	}

	if !vi.support && !vi.against {
		return abstain
	}

	// both are true
	panic("WTF")
}

func voteFromBytes(b []byte) (voteInput, error) {
	if len(b) < 2 {
		return voteInput{true, true}, errors.New("not valid bytes for vote")
	}

	if b[0] == 1 {
		return voteInput{
			true,
			false,
		}, nil
	}

	if b[1] == 1 {
		return voteInput{
			false,
			true,
		}, nil
	}

	return voteInput{
		support: false,
		against: false,
	}, nil
}

func (vi voteInput) Bytes() []byte {
	if vi.support {
		return []byte{1, 0}
	}

	if vi.against {
		return []byte{0, 1}
	}

	return []byte{0, 0}
}

type retriever interface {
	Retrieve(key []byte, v interface{}) (interface{}, error)
}
