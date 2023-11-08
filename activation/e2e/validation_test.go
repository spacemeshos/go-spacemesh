package activation_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestValidator_Validate(t *testing.T) {
	ctrl := gomock.NewController(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := activation.DefaultPostConfig()
	cdb := datastore.NewCachedDB(sql.InMemory(), log.NewFromLog(logger))

	mgr, err := activation.NewPostSetupManager(sig.NodeID(), cfg, logger, cdb, goldenATX)
	require.NoError(t, err)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	initPost(t, logger.Named("manager"), mgr, opts)

	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:        epoch / 2,
		CycleGap:          epoch / 5,
		GracePeriod:       epoch / 5,
		RequestTimeout:    epoch / 5,
		RequestRetryDelay: epoch / 50,
		MaxRequestRetries: 10,
	}
	poetProver := spawnPoet(
		t,
		WithGenesis(time.Now()),
		WithEpochDuration(epoch),
		WithPhaseShift(poetCfg.PhaseShift),
		WithCycleGap(poetCfg.CycleGap),
	)

	mclock := activation.NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(postGenesisEpoch.FirstLayer()) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)

	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })

	poetDb := activation.NewPoetDb(sql.InMemory(), log.NewFromLog(logger).Named("poetDb"))

	svc := grpcserver.NewPostService(logger)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	t.Cleanup(launchPostSupervisor(t, logger, mgr, grpcCfg, opts))

	require.Eventually(t, func() bool {
		_, err := svc.Client(sig.NodeID())
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	nb, err := activation.NewNIPostBuilder(
		poetDb,
		svc,
		[]string{poetProver.RestURL().String()},
		t.TempDir(),
		logtest.New(t, zapcore.DebugLevel),
		sig,
		poetCfg,
		mclock,
	)
	require.NoError(t, err)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	challengeHash := challenge.Hash()

	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	require.NoError(t, err)

	v := activation.NewValidator(poetDb, cfg, opts.Scrypt, verifier)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost, challengeHash, opts.NumUnits)
	require.NoError(t, err)

	_, err = v.NIPost(
		context.Background(),
		sig.NodeID(),
		goldenATX,
		nipost,
		types.BytesToHash([]byte("lerner")),
		opts.NumUnits,
	)
	require.ErrorContains(t, err, "invalid membership proof")

	newNIPost := *nipost
	newNIPost.Post = &types.Post{}
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, &newNIPost, challengeHash, opts.NumUnits)
	require.ErrorContains(t, err, "invalid Post")

	newPostCfg := cfg
	newPostCfg.MinNumUnits = opts.NumUnits + 1
	v = activation.NewValidator(poetDb, newPostCfg, opts.Scrypt, nil)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost, challengeHash, opts.NumUnits)
	require.EqualError(
		t,
		err,
		fmt.Sprintf("invalid `numUnits`; expected: >=%d, given: %d", newPostCfg.MinNumUnits, opts.NumUnits),
	)

	newPostCfg = cfg
	newPostCfg.MaxNumUnits = opts.NumUnits - 1
	v = activation.NewValidator(poetDb, newPostCfg, opts.Scrypt, nil)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost, challengeHash, opts.NumUnits)
	require.EqualError(
		t,
		err,
		fmt.Sprintf("invalid `numUnits`; expected: <=%d, given: %d", newPostCfg.MaxNumUnits, opts.NumUnits),
	)

	newPostCfg = cfg
	newPostCfg.LabelsPerUnit = nipost.PostMetadata.LabelsPerUnit + 1
	v = activation.NewValidator(poetDb, newPostCfg, opts.Scrypt, nil)
	_, err = v.NIPost(context.Background(), sig.NodeID(), goldenATX, nipost, challengeHash, opts.NumUnits)
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
