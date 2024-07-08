package atxsdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/mocks"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func gatx(
	id types.ATXID,
	epoch types.EpochID,
	smesher types.NodeID,
	nonce types.VRFPostIndex,
) types.ActivationTx {
	atx := &types.ActivationTx{
		NumUnits:     1,
		PublishEpoch: epoch,
		SmesherID:    smesher,
		VRFNonce:     nonce,
		TickCount:    1,
	}
	atx.SetID(id)
	atx.SetReceived(time.Time{}.Add(1))
	return *atx
}

func TestWarmup(t *testing.T) {
	types.SetLayersPerEpoch(3)
	t.Run("sanity", func(t *testing.T) {
		db := statesql.InMemory()
		applied := types.LayerID(10)
		nonce := types.VRFPostIndex(1)
		data := []types.ActivationTx{
			gatx(types.ATXID{1, 1}, 1, types.NodeID{1}, nonce),
			gatx(types.ATXID{1, 2}, 1, types.NodeID{2}, nonce),
			gatx(types.ATXID{2, 1}, 2, types.NodeID{1}, nonce),
			gatx(types.ATXID{2, 2}, 2, types.NodeID{2}, nonce),
			gatx(types.ATXID{3, 2}, 3, types.NodeID{2}, nonce),
			gatx(types.ATXID{3, 3}, 3, types.NodeID{3}, nonce),
		}
		for i := range data {
			require.NoError(t, atxs.Add(db, &data[i], types.AtxBlob{}))
		}
		require.NoError(t, layers.SetApplied(db, applied, types.BlockID{1}))

		c, err := Warm(db, 1)
		require.NoError(t, err)
		for _, atx := range data[2:] {
			require.NotNil(t, c.Get(atx.TargetEpoch(), atx.ID()))
		}
	})
	t.Run("no data", func(t *testing.T) {
		c, err := Warm(statesql.InMemory(), 1)
		require.NoError(t, err)
		require.NotNil(t, c)
	})
	t.Run("closed db", func(t *testing.T) {
		db := statesql.InMemory()
		require.NoError(t, db.Close())
		c, err := Warm(db, 1)
		require.Error(t, err)
		require.Nil(t, c)
	})
	t.Run("db failures", func(t *testing.T) {
		db := statesql.InMemory()
		nonce := types.VRFPostIndex(1)
		data := gatx(types.ATXID{1, 1}, 1, types.NodeID{1}, nonce)
		require.NoError(t, atxs.Add(db, &data, types.AtxBlob{}))

		exec := mocks.NewMockExecutor(gomock.NewController(t))
		call := 0
		fail := 0
		tx, err := db.Tx(context.Background())
		require.NoError(t, err)
		exec.EXPECT().
			Exec(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(q string, enc sql.Encoder, dec sql.Decoder) (int, error) {
				if call == fail {
					return 0, errors.New("test")
				}
				call++
				return tx.Exec(q, enc, dec)
			}).
			AnyTimes()
		for range 3 {
			c := New()
			require.Error(t, Warmup(exec, c, 1))
			fail++
			call = 0
		}
	})
}
