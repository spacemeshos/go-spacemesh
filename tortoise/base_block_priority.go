package tortoise

import (
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// prioritizeBallots will sort ballots inplace according to internal prioritization.
func prioritizeBallots(
	ballots []*ballotInfoV2,
	disagreements map[types.BallotID]types.LayerID,
	badBeaconBallots map[types.BallotID]struct{},
) {
	sort.Slice(ballots, func(i, j int) bool {
		iballot := ballots[i]
		jballot := ballots[j]
		// use ballots with bad beacons only as a last resort
		_, ibad := badBeaconBallots[iballot.id]
		_, jbad := badBeaconBallots[jballot.id]
		if ibad != jbad {
			return !ibad
		}

		// prioritize ballots with less disagreements to a local opinion
		if disagreements[iballot.id] != disagreements[jballot.id] {
			return disagreements[iballot.id].After(disagreements[jballot.id])
		}
		// prioritize ballots from higher layers
		if iballot.layer != jballot.layer {
			return iballot.layer.After(jballot.layer)
		}
		// otherwise just sort deterministically
		return iballot.id.Compare(jballot.id)
	})
}
