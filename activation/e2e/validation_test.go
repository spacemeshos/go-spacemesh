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
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestValidator_Validate(t *testing.T) {
	ctrl := gomock.NewController(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := activation.DefaultPostConfig()
	db := sql.InMemory()

	validator := activation.NewMocknipostValidator(gomock.NewController(t))
	syncer := activation.NewMocksyncer(gomock.NewController(t))
	syncer.EXPECT().RegisterForATXSynced().AnyTimes().DoAndReturn(func() <-chan struct{} {
		synced := make(chan struct{})
		close(synced)
		return synced
	})

	mgr, err := activation.NewPostSetupManager(cfg, logger, db, atxsdata.New(), goldenATX, syncer, validator)
	require.NoError(t, err)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	initPost(t, mgr, opts, sig.NodeID())

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:        epoch / 2,
		CycleGap:          epoch / 4,
		GracePeriod:       epoch / 5,
		RequestTimeout:    epoch / 5,
		RequestRetryDelay: epoch / 50,
		MaxRequestRetries: 10,
	}
	poetProver := spawnPoet(
		t,
		WithGenesis(genesis),
		WithEpochDuration(epoch),
		WithPhaseShift(poetCfg.PhaseShift),
		WithCycleGap(poetCfg.CycleGap),
	)

	mclock := activation.NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)

	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })

	poetDb := activation.NewPoetDb(sql.InMemory(), log.NewFromLog(logger).Named("poetDb"))

	svc := grpcserver.NewPostService(logger)
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	t.Cleanup(launchPostSupervisor(t, logger, mgr, sig, grpcCfg, opts))

	require.Eventually(t, func() bool {
		_, err := svc.Client(sig.NodeID())
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := types.RandomHash()
	nb, err := activation.NewNIPostBuilder(
		localsql.InMemory(),
		poetDb,
		svc,
		[]types.PoetServer{{Address: poetProver.RestURL().String()}},
		logger.Named("nipostBuilder"),
		poetCfg,
		mclock,
	)
	require.NoError(t, err)

	nipost, err := nb.BuildNIPost(context.Background(), sig, postGenesisEpoch+2, challenge)
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
	require.ErrorContains(t, err, "invalid Post")

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
