package runner

import (
	"context"
	"errors"
	"math"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
)

func goodProposals(
	ctx context.Context,
	logger log.Log,
	db *datastore.CachedDB,
	nodeID types.NodeID,
	lid types.LayerID,
	epochBeacon types.Beacon,
) []types.ProposalID {
	props, err := proposals.GetByLayer(db, lid)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			logger.With().Warning("no proposals found for hare, using empty set", log.Context(ctx), lid, log.Err(err))
		} else {
			logger.With().Error("failed to get proposals for hare", log.Context(ctx), lid, log.Err(err))
		}
		return []types.ProposalID{}
	}

	var (
		beacon        types.Beacon
		result        []types.ProposalID
		ownHdr        *types.ActivationTxHeader
		ownTickHeight = uint64(math.MaxUint64)
	)
	// a non-smesher will not filter out any proposals, as it doesn't have voting power
	// and only observes the consensus process.
	ownHdr, err = db.GetEpochAtx(lid.GetEpoch(), nodeID)
	if err != nil {
		logger.With().Error("failed to get own atx", log.Context(ctx), lid, log.Err(err))
		return []types.ProposalID{}
	}
	if ownHdr != nil {
		ownTickHeight = ownHdr.TickHeight()
	}
	atxs := map[types.ATXID]int{}
	for _, p := range props {
		atxs[p.AtxID]++
	}
	for _, p := range props {
		if p.IsMalicious() {
			logger.With().Warning("not voting on proposal from malicious identity",
				log.Stringer("id", p.ID()),
			)
			continue
		}
		if n := atxs[p.AtxID]; n > 1 {
			logger.With().Warning("proposal with same atx added several times in the recorded set",
				log.Int("n", n),
				log.Stringer("id", p.ID()),
				log.Stringer("atxid", p.AtxID),
			)
			continue
		}
		if ownHdr != nil {
			hdr, err := db.GetAtxHeader(p.AtxID)
			if err != nil {
				logger.With().Error("failed to get atx", log.Context(ctx), lid, p.AtxID, log.Err(err))
				return []types.ProposalID{}
			}
			if hdr.BaseTickHeight >= ownTickHeight {
				// does not vote for future proposal
				logger.With().Warning("proposal base tick height too high. skipping",
					log.Context(ctx),
					lid,
					log.Uint64("proposal_height", hdr.BaseTickHeight),
					log.Uint64("own_height", ownTickHeight),
				)
				continue
			}
		}
		if p.EpochData != nil {
			beacon = p.EpochData.Beacon
		} else if p.RefBallot == types.EmptyBallotID {
			logger.With().Error("proposal missing ref ballot", p.ID())
			return []types.ProposalID{}
		} else if refBallot, err := ballots.Get(db, p.RefBallot); err != nil {
			logger.With().Error("failed to get ref ballot", p.ID(), p.RefBallot, log.Err(err))
			return []types.ProposalID{}
		} else if refBallot.EpochData == nil {
			logger.With().Error("ref ballot missing epoch data", log.Context(ctx), lid, refBallot.ID())
			return []types.ProposalID{}
		} else {
			beacon = refBallot.EpochData.Beacon
		}

		if beacon == epochBeacon {
			result = append(result, p.ID())
		} else {
			logger.With().Warning("proposal has different beacon value",
				log.Context(ctx),
				lid,
				p.ID(),
				log.String("proposal_beacon", beacon.ShortString()),
				log.String("epoch_beacon", epochBeacon.ShortString()))
		}
	}
	return result
}
