package node

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestCheckDBValidity(t *testing.T) {
	t.Skip("This test is intended to be run manually to check the validity of malfeasance proofs in the database")

	db, err := statesql.Open("file:state.sql")
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	cfg := config.MainnetConfig()
	edVerifier := signing.NewEdVerifier(
		signing.WithVerifierPrefix(cfg.Genesis.GenesisID().Bytes()),
	)
	postVerifier, err := activation.NewPostVerifier(
		cfg.POST,
		logger.Named("post_verifier"),
	)
	require.NoError(t, err)

	malfeasanceLogger := logger.Named("malfeasance")
	activationMH := activation.NewMalfeasanceHandler(
		db,
		malfeasanceLogger,
		edVerifier,
	)
	meshMH := mesh.NewMalfeasanceHandler(
		db,
		edVerifier,
		mesh.WithMalfeasanceLogger(malfeasanceLogger),
	)
	hareMH := hare3.NewMalfeasanceHandler(
		db,
		edVerifier,
		hare3.WithMalfeasanceLogger(malfeasanceLogger),
	)
	invalidPostMH := activation.NewInvalidPostIndexHandler(
		db,
		edVerifier,
		postVerifier,
	)
	invalidPrevMH := activation.NewInvalidPrevATXHandler(db, edVerifier)

	nodeIDs := make([]types.NodeID, 0)

	ctrl := gomock.NewController(t)
	trtl := malfeasance.NewMocktortoise(ctrl)
	handler := malfeasance.NewHandler(
		datastore.NewCachedDB(db, logger.Named("cached_db")),
		malfeasanceLogger,
		"self",
		nodeIDs,
		trtl,
	)
	handler.RegisterHandler(malfeasance.MultipleATXs, activationMH)
	handler.RegisterHandler(malfeasance.MultipleBallots, meshMH)
	handler.RegisterHandler(malfeasance.HareEquivocation, hareMH)
	handler.RegisterHandler(malfeasance.InvalidPostIndex, invalidPostMH)
	handler.RegisterHandler(malfeasance.InvalidPrevATX, invalidPrevMH)

	i := 0
	err = identities.IterateOps(db, builder.Operations{}, func(nodeID types.NodeID, bytes []byte, _ time.Time) bool {
		proof := &wire.MalfeasanceProof{}
		err := codec.Decode(bytes, proof)
		require.NoError(t, err)

		id, err := handler.Validate(context.Background(), proof)
		require.NoError(t, err)
		require.Equal(t, nodeID, id)

		t.Logf("Proof %d is valid for %s\n", i, nodeID.ShortString())
		i++
		return true
	})
	require.NoError(t, err)
}
