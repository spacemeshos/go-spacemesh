package checkpoint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
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
		atx := wire.ActivationTxFromWireV1(marriageATX)
		atx.SetReceived(time.Now().Local())
		atx.TickCount = 1
		require.NoError(t, atxs.Add(db, atx, marriageATX.Blob()))
		mAtxID := marriageATX.ID()

		watx := &wire.ActivationTxV2{
			PositioningATX: golden,
			SmesherID:      types.RandomNodeID(),
			MarriageATX:    &mAtxID,
		}
		atx = &types.ActivationTx{
			SmesherID: watx.SmesherID,
		}
		atx.SetID(watx.ID())
		atx.SetReceived(time.Now())
		require.NoError(t, atxs.Add(db, atx, watx.Blob()))

		// marry the two IDs
		err := marriage.Add(db, marriage.Info{ID: 1, ATX: marriageATX.ID(), NodeID: marriageATX.SmesherID})
		require.NoError(t, err)
		err = marriage.Add(db, marriage.Info{ID: 1, ATX: marriageATX.ID(), NodeID: marriageATX.SmesherID})
		require.NoError(t, err)

		all := map[types.ATXID]struct{}{golden: {}}
		deps := map[types.ATXID]*AtxDep{}

		require.NoError(t, collect(db, atx.ID(), all, deps))

		require.ElementsMatch(t, maps.Keys(deps), []types.ATXID{marriageATX.ID(), atx.ID()})
	})
}
