package activation_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	ae2e "github.com/spacemeshos/go-spacemesh/activation/e2e"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestValidator_Validate(t *testing.T) {
	ctrl := gomock.NewController(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := testPostConfig()
	db := statesql.InMemory()

	validator := activation.NewMocknipostValidator(gomock.NewController(t))

	opts := testPostSetupOpts(t)
	svc := grpcserver.NewPostService(logger, grpcserver.PostServiceQueryInterval(100*time.Millisecond))
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	initPost(t, cfg, opts, sig, goldenATX, grpcCfg, svc)

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:  epoch / 2,
		CycleGap:    3 * epoch / 4,
		GracePeriod: epoch / 4,
	}

	poetDb, err := activation.NewPoetDb(statesql.InMemory(), logger.Named("poetDb"))
	require.NoError(t, err)
	client := ae2e.NewTestPoetClient(1, poetCfg)
	poetService := activation.NewPoetServiceWithClient(poetDb, client, poetCfg, logger)

	mclock := activation.NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)

	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })

	challenge := types.RandomHash()
	nb, err := activation.NewNIPostBuilder(
		localsql.InMemory(),
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		mclock,
		validator,
		activation.WithPoetServices(poetService),
	)
	require.NoError(t, err)

	nipost, err := nb.BuildNIPost(context.Background(), sig, challenge,
		&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.NoError(t, err)

	v := activation.NewValidator(db, poetDb, cfg, opts.Scrypt, verifier)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost.NIPost, challenge, nipost.NumUnits)
	require.NoError(t, err)

	_, err = v.NIPost(
		context.Background(),
		sig.NodeID(),
		goldenATX,
		nipost.NIPost,
		types.BytesToHash([]byte("lerner")),
		nipost.NumUnits,
	)
	require.ErrorContains(t, err, "invalid membership proof")

	newNIPost := *nipost.NIPost
	newNIPost.Post = &types.Post{}
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, &newNIPost, challenge, nipost.NumUnits)
	require.ErrorContains(t, err, "validating Post: verifying PoST: proof indices are empty")

	newPostCfg := cfg
	newPostCfg.MinNumUnits = nipost.NumUnits + 1
	v = activation.NewValidator(db, poetDb, newPostCfg, opts.Scrypt, nil)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost.NIPost, challenge, nipost.NumUnits)
	require.EqualError(
		t,
		err,
		fmt.Sprintf("invalid `numUnits`; expected: >=%d, given: %d", newPostCfg.MinNumUnits, nipost.NumUnits),
	)

	newPostCfg = cfg
	newPostCfg.MaxNumUnits = nipost.NumUnits - 1
	v = activation.NewValidator(db, poetDb, newPostCfg, opts.Scrypt, nil)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost.NIPost, challenge, nipost.NumUnits)
	require.EqualError(
		t,
		err,
		fmt.Sprintf("invalid `numUnits`; expected: <=%d, given: %d", newPostCfg.MaxNumUnits, nipost.NumUnits),
	)

	newPostCfg = cfg
	newPostCfg.LabelsPerUnit = nipost.PostMetadata.LabelsPerUnit + 1
	v = activation.NewValidator(db, poetDb, newPostCfg, opts.Scrypt, nil)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost.NIPost, challenge, nipost.NumUnits)
	require.EqualError(
		t,
		err,
		fmt.Sprintf(
			"invalid `LabelsPerUnit`; expected: >=%d, given: %d",
			newPostCfg.LabelsPerUnit,
			nipost.PostMetadata.LabelsPerUnit,
		),
	)
}
