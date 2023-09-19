package miner

import (
	"fmt"

	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func ActiveSetFromEpochFirstBlock(db sql.Executor, epoch types.EpochID) ([]types.ATXID, error) {
	bid, err := layers.FirstAppliedInEpoch(db, epoch)
	if err != nil {
		return nil, err
	}
	return activeSetFromBlock(db, bid)
}

func activeSetFromBlock(db sql.Executor, bid types.BlockID) ([]types.ATXID, error) {
	block, err := blocks.Get(db, bid)
	if err != nil {
		return nil, fmt.Errorf("actives get block: %w", err)
	}
	activeMap := make(map[types.ATXID]struct{})
	// the active set is the union of all active sets recorded in rewarded miners' ref ballot
	for _, r := range block.Rewards {
		activeMap[r.AtxID] = struct{}{}
		ballot, err := ballots.FirstInEpoch(db, r.AtxID, block.LayerIndex.GetEpoch())
		if err != nil {
			return nil, fmt.Errorf("actives get ballot: %w", err)
		}
		actives, err := activesets.Get(db, ballot.EpochData.ActiveSetHash)
		if err != nil {
			return nil, fmt.Errorf("actives get active hash for ballot %s: %w", ballot.ID().String(), err)
		}
		for _, id := range actives.Set {
			activeMap[id] = struct{}{}
		}
	}
	return maps.Keys(activeMap), nil
}
