package checkpoint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestCollectingDeps(t *testing.T) {
	golden := types.RandomATXID()
	t.Run("collect marriage ATXs", func(t *testing.T) {
		db := statesql.InMemory()

		marriageATX := &wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PositioningATXID: golden,
					CommitmentATXID:  &golden,
				},
			},
			SmesherID: types.RandomNodeID(),
		}
		require.NoError(t, atxs.Add(db, fixture.ToAtx(t, marriageATX), marriageATX.Blob()))
		mAtxID := marriageATX.ID()

		watx := &wire.ActivationTxV2{
			PositioningATX: golden,
			SmesherID:      types.RandomNodeID(),
			MarriageATX:    &mAtxID,
		}
		atx := &types.ActivationTx{
			SmesherID: watx.SmesherID,
		}
		atx.SetID(watx.ID())
		atx.SetReceived(time.Now())
		require.NoError(t, atxs.Add(db, atx, watx.Blob()))

		// marry the two IDs
		err := identities.SetMarriage(db, marriageATX.SmesherID, &identities.MarriageData{ATX: marriageATX.ID()})
		require.NoError(t, err)
		err = identities.SetMarriage(db, atx.SmesherID, &identities.MarriageData{ATX: marriageATX.ID()})
		require.NoError(t, err)

		all := map[types.ATXID]struct{}{golden: {}}
		deps := map[types.ATXID]*AtxDep{}

		require.NoError(t, collect(db, atx.ID(), all, deps))

		require.ElementsMatch(t, maps.Keys(deps), []types.ATXID{marriageATX.ID(), atx.ID()})
	})
}
