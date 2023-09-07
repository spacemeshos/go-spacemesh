package tortoise

import (
	"sort"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

func verifyLayer(logger *zap.Logger, blocks []*blockInfo, getDecision func(*blockInfo) sign) (bool, bool) {
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
		logger.Debug("decision for a block",
			zap.Int("ith", i),
			zap.Stringer("block", block.id),
			zap.Stringer("lid", block.layer),
			zap.Stringer("decision", decision),
			zap.Stringer("weight", block.margin),
			zap.Uint64("height", block.height),
		)

		if decision == abstain {
			// all blocks with the same height should be finalized
			if supported != nil && block.height > supported.height {
				decision = against
			} else {
				return false, false
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
	return true, changes
}

func zapBlocks(blocks []*blockInfo) zap.Field {
	return zap.Array("blocks", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for i := range blocks {
			encoder.AppendObject(zapcore.ObjectMarshalerFunc(func(encoder zapcore.ObjectEncoder) error {
				encoder.AddString("decision", blocks[i].validity.String())
				encoder.AddString("id", blocks[i].id.String())
				encoder.AddString("weight", blocks[i].margin.String())
				encoder.AddUint64("height", blocks[i].height)
				encoder.AddBool("data", blocks[i].data)
				return nil
			}))
		}
		return nil
	}))
}
