package tortoise

import (
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// prioritizeBallots will sort ballots inplace according to internal prioritization.
func prioritizeBallots(
	ballots []types.BallotID,
	disagreements map[types.BallotID]types.LayerID,
	ballotLayer map[types.BallotID]types.LayerID,
) {
	sort.Slice(ballots, func(i, j int) bool {
		ibid := ballots[i]
		jbid := ballots[j]
		// prioritize ballots with less disagreements to a local opinion
		if disagreements[ibid] != disagreements[jbid] {
			return disagreements[ibid].After(disagreements[jbid])
		}
		// prioritize ballots from higher layers
		if ballotLayer[ibid] != ballotLayer[jbid] {
			return ballotLayer[ibid].After(ballotLayer[jbid])
		}
		// otherwise just sort deterministically
		return ibid.Compare(jbid)
	})
}
