package miner

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
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

func activesFromFirstBlock(
	cdb *datastore.CachedDB,
	signer *signing.VRFSigner,
	target types.EpochID,
	ownAtx types.ATXID,
	ownWeight uint64,
) (uint64, []types.ATXID, error) {
	set, err := ActiveSetFromEpochFirstBlock(cdb, target)
	if err != nil {
		return 0, nil, err
	}
	var (
		totalWeight uint64
		ownIncluded bool
	)
	for _, id := range set {
		ownIncluded = ownIncluded || id == ownAtx
		atx, err := cdb.GetAtxHeader(id)
		if err != nil {
			return 0, nil, err
		}
		totalWeight += atx.GetWeight()
	}
	if !ownIncluded {
		// miner is not included in the active set derived from the epoch's first block
		set = append(set, ownAtx)
		totalWeight += ownWeight
	}
	return totalWeight, set, nil
}

func generateActiveSet(
	logger log.Log,
	cdb *datastore.CachedDB,
	signer *signing.VRFSigner,
	target types.EpochID,
	epochStart time.Time,
	goodAtxPercent int,
	networkDelay time.Duration,
	ownAtx types.ATXID,
	ownWeight uint64,
) (uint64, []types.ATXID, error) {
	var (
		totalWeight uint64
		set         []types.ATXID
		numOmitted  = 0
	)
	if err := cdb.IterateEpochATXHeaders(target, func(header *types.ActivationTxHeader) error {
		grade, err := GradeAtx(cdb, header.NodeID, header.Received, epochStart, networkDelay)
		if err != nil {
			return err
		}
		if grade != Good && header.NodeID != signer.NodeID() {
			logger.With().Info("atx omitted from active set",
				header.ID,
				log.Int("grade", int(grade)),
				log.Stringer("smesher", header.NodeID),
				log.Bool("own", header.NodeID == signer.NodeID()),
				log.Time("received", header.Received),
				log.Time("epoch_start", epochStart),
			)
			numOmitted++
			return nil
		}
		totalWeight += header.GetWeight()
		set = append(set, header.ID)
		return nil
	}); err != nil {
		return 0, nil, err
	}

	if total := numOmitted + len(set); total == 0 {
		return 0, nil, fmt.Errorf("empty active set")
	} else if numOmitted*100/total > 100-goodAtxPercent {
		// if the node is not synced during `targetEpoch-1`, it doesn't have the correct receipt timestamp
		// for all the atx and malfeasance proof. this active set is not usable.
		// TODO: change after timing info of ATXs and malfeasance proofs is sync'ed from peers as well
		var err error
		totalWeight, set, err = activesFromFirstBlock(cdb, signer, target, ownAtx, ownWeight)
		if err != nil {
			return 0, nil, err
		}
		logger.With().Info("miner not synced during prior epoch, active set from first block",
			log.Int("all atx", total),
			log.Int("num omitted", numOmitted),
			log.Int("num block atx", len(set)),
		)
	} else {
		logger.With().Info("active set selected for proposal using grades",
			log.Int("num atx", len(set)),
			log.Int("num omitted", numOmitted),
			log.Int("min atx good pct", goodAtxPercent),
		)
	}
	sort.Slice(set, func(i, j int) bool {
		return bytes.Compare(set[i].Bytes(), set[j].Bytes()) < 0
	})
	return totalWeight, set, nil
}

// calcEligibilityProofs calculates the eligibility proofs of proposals for the miner in the given epoch
// and returns the proofs along with the epoch's active set.
func calcEligibilityProofs(
	signer *signing.VRFSigner,
	epoch types.EpochID,
	beacon types.Beacon,
	nonce types.VRFPostIndex,
	slots uint32,
	layersPerEpoch uint32,
) map[types.LayerID][]types.VotingEligibility {
	proofs := map[types.LayerID][]types.VotingEligibility{}
	for counter := uint32(0); counter < slots; counter++ {
		vrf := signer.Sign(proposals.MustSerializeVRFMessage(beacon, epoch, nonce, counter))
		layer := proposals.CalcEligibleLayer(epoch, layersPerEpoch, vrf)
		proofs[layer] = append(proofs[layer], types.VotingEligibility{
			J:   counter,
			Sig: vrf,
		})
	}
	return proofs
}
