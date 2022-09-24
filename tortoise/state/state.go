package state

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

type (
	Vote   uint8
	Weight util.Weight

	LayerInfo struct {
		ID             types.LayerID
		HareTerminated bool

		Empty  Weight
		Blocks []*BlockInfo

		Verifying VerifyingInfo
	}

	BlockInfo struct {
		ID     types.BlockID
		LID    types.LayerID
		Height uint64

		Hare     Vote
		Validity Vote
		Margin   Weight
	}

	VerifyingInfo struct {
		Good, Abstained Weight
		ReferenceHeight uint64
	}

	BlockVote struct {
		*BlockInfo
		Vote Vote
	}

	BaseInfo struct {
		ID  types.BallotID
		LID types.LayerID
	}

	BallotInfo struct {
		ID     types.BallotID
		Base   BaseInfo
		LID    types.LayerID
		Height uint64
		Weight Weight
		Beacon types.Beacon

		Votes Votes

		Goodness Condition
	}
)
