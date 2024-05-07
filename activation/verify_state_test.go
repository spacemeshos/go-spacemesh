package activation

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

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

	atx1 := newActivationTx(
		t,
		sig,
		0,
		prevATXID,
		goldenATXID,
		&goldenATXID,
		types.EpochID(2),
		0,
		100,
		types.GenerateAddress([]byte("aaaa")),
		100,
		nil,
	)
	require.NoError(t, atxs.Add(db, atx1))

	atx2 := newActivationTx(
		t,
		sig,
		1,
		prevATXID,
		goldenATXID,
		&goldenATXID,
		types.EpochID(3),
		0,
		100,
		types.GenerateAddress([]byte("aaaa")),
		100,
		nil,
	)
	require.NoError(t, atxs.Add(db, atx2))

	// create 100 random ATXs that are not malicious
	for i := 0; i < 100; i++ {
		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx := newActivationTx(
			t,
			otherSig,
			rand.Uint64(),
			types.RandomATXID(),
			types.RandomATXID(),
			nil,
			rand.N[types.EpochID](100),
			0,
			100,
			types.GenerateAddress([]byte("aaaa")),
			rand.Uint32(),
			nil,
		)
		require.NoError(t, atxs.Add(db, atx))
	}

	// Act
	err = CheckPrevATXs(context.Background(), logger, db)
	require.NoError(t, err)

	// Assert
	malicious, err := identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}
