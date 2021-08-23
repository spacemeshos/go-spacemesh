package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NOTE(dshulyak) there is a bug in xdr. without specifying size xdr will cut the value
// down to int32
type vec struct {
	Support, Against uint64
	Flushed          bool
}

const (
	globalThreshold = 0.6
)

var ( //correction vectors type
	// Opinion vectors
	support = vec{Support: 1, Against: 0}
	against = vec{Support: 0, Against: 1}
	abstain = vec{Support: 0, Against: 0}
)

// Field returns a log field. Implements the LoggableField interface.
func (a vec) Field() log.Field {
	return log.String("vote_vector", fmt.Sprintf("[%d,%d]", a.Support, a.Against))
}

func (a vec) Add(v vec) vec {
	a.Support += v.Support
	a.Against += v.Against
	a.Flushed = false
	return a
}

func (a vec) Multiply(x uint64) vec {
	a.Against *= x
	a.Support *= x
	a.Flushed = false
	return a
}

func simplifyVote(v vec) vec {
	if v.Support > v.Against {
		return support
	}

	if v.Against > v.Support {
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

func calculateGlobalOpinion(logger log.Log, v vec, layerSize int, delta float64) vec {
	threshold := globalThreshold * delta * float64(layerSize)
	logger.With().Debug("global opinion", v, log.String("threshold", fmt.Sprint(threshold)))
	if float64(v.Support) > threshold {
		return support
	} else if float64(v.Against) > threshold {
		return against
	} else {
		return abstain
	}
}

// Opinion is opinions on other blocks.
type Opinion map[types.BlockID]vec
