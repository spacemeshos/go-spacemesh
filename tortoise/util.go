package tortoise

import (
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	support sign = 1
	against sign = -1
	abstain sign = 0

	neutral = abstain
)

type sign int8

func (a sign) String() string {
	switch a {
	case 1:
		return "support"
	case -1:
		return "against"
	case 0:
		return "abstain"
	default:
		panic("sign should be 0/-1/1")
	}
}

type voteReason string

func (v voteReason) String() string {
	return string(v)
}

const (
	reasonHareOutput     voteReason = "hare"
	reasonValidity       voteReason = "validity"
	reasonLocalThreshold voteReason = "local_threshold"
	reasonCoinflip       voteReason = "coinflip"
	reasonMissingData    voteReason = "missing data"
)

func maxLayer(i, j types.LayerID) types.LayerID {
	if i.After(j) {
		return i
	}
	return j
}

func verifyLayer(logger log.Log, blocks []*blockInfo, getDecision func(*blockInfo) sign) bool {
	// order blocks by height in ascending order
	// if there is a support before any abstain
	// and a previous height is lower than the current one
	// the layer is verified
	//
	// it will modify original slice
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].height < blocks[j].height
	})
	var (
		decisions = make([]sign, 0, len(blocks))
		supported *blockInfo
	)
	for i, block := range blocks {
		decision := getDecision(block)
		logger.With().Debug("decision for a block",
			log.Int("ith", i),
			block.id,
			block.layer,
			log.Stringer("decision", decision),
			log.Stringer("weight", block.margin),
			log.Uint64("height", block.height),
		)

		if decision == abstain {
			// all blocks with the same height should be finalized
			if supported != nil && block.height > supported.height {
				decision = against
			} else {
				return false
			}
		} else if decision == support {
			supported = block
		}
		decisions = append(decisions, decision)
	}
	changes := false
	for i, decision := range decisions {
		if blocks[i].validity != decision {
			changes = true
		}
		blocks[i].validity = decision
	}
	if changes {
		logger.With().Info("candidate layer is verified",
			log.Array("blocks",
				log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
					for i := range blocks {
						encoder.AppendObject(log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
							encoder.AddString("decision", blocks[i].validity.String())
							encoder.AddString("id", blocks[i].id.String())
							encoder.AddString("weight", blocks[i].margin.String())
							encoder.AddUint64("height", blocks[i].height)
							encoder.AddBool("data", blocks[i].data)
							return nil
						}))
					}
					return nil
				})),
		)
	}
	return true
}
