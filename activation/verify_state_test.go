package activation

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func Test_CheckPrevATXs(t *testing.T) {
	db := sql.InMemory()
	logger := zaptest.NewLogger(t)

	// Arrange
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// create two ATXs with the same PrevATXID
	prevATXID := types.RandomATXID()
	goldenATXID := types.RandomATXID()

	atx1 := newInitialATXv1(t, goldenATXID, func(atx *wire.ActivationTxV1) {
		atx.PrevATXID = prevATXID
		atx.PublishEpoch = 2
	})
	atx1.Sign(sig)
	vAtx1 := toAtx(t, atx1)
	require.NoError(t, atxs.Add(db, vAtx1, atx1.Blob()))

	atx2 := newInitialATXv1(t, goldenATXID, func(atx *wire.ActivationTxV1) {
		atx.PrevATXID = prevATXID
		atx.PublishEpoch = 3
	})
	atx2.Sign(sig)
	vAtx2 := toAtx(t, atx2)
	require.NoError(t, atxs.Add(db, vAtx2, atx2.Blob()))

	// create 100 random ATXs that are not malicious
	for i := 0; i < 100; i++ {
		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx := newInitialATXv1(t, types.RandomATXID(), func(atx *wire.ActivationTxV1) {
			atx.PrevATXID = types.RandomATXID()
			atx.NumUnits = rand.Uint32()
			atx.PublishEpoch = rand.N[types.EpochID](100)
		})
		atx.Sign(otherSig)
		vAtx := toAtx(t, atx)
		require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))
	}

	// Act
	err = CheckPrevATXs(context.Background(), logger, db)
	require.NoError(t, err)

	// Assert
	malicious, err := identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}
