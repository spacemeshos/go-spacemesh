package tortoise

import (
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// prioritizeBallots will sort ballots inplace according to internal prioritization.
func prioritizeBallots(
	ballots []*ballotInfo,
	disagreements map[types.BallotID]types.LayerID,
) {
	sort.Slice(ballots, func(i, j int) bool {
		iballot := ballots[i]
		jballot := ballots[j]
		// use ballots with bad beacons only as a last resort
		if iballot.conditions.badBeacon != jballot.conditions.badBeacon {
			return !iballot.conditions.badBeacon
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
