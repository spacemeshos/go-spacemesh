package tortoise

import "github.com/spacemeshos/go-spacemesh/common/types"

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
