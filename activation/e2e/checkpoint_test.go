package activation_test

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	ae2e "github.com/spacemeshos/go-spacemesh/activation/e2e"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

// Test activation process after a checkpoint-recovery scenario.
//
// The tests check if ATXs can be built and published after a checkpoint.

func TestCheckpoint_PublishingSoloATXs(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	cfg := testPostConfig()
	db := statesql.InMemory()
	cdb := datastore.NewCachedDB(db, logger)

	opts := testPostSetupOpts(t)
	svc := grpcserver.NewPostService(logger, grpcserver.PostServiceQueryInterval(100*time.Millisecond))
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	initPost(t, cfg, opts, sig, goldenATX, grpcCfg, svc)
	syncer := syncedSyncer(t)

	poetDb, err := activation.NewPoetDb(db, logger.Named("poetDb"))
	require.NoError(t, err)
	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })

	validator := activation.NewValidator(db, poetDb, cfg, opts.Scrypt, verifier)

	epoch := layerDuration * time.Duration(layersPerEpoch)
	poetCfg := activation.PoetConfig{
		PhaseShift:  epoch,
		CycleGap:    3 * epoch / 4,
		GracePeriod: epoch / 4,
	}
	client := ae2e.NewTestPoetClient(1, poetCfg)
	poetService := activation.NewPoetServiceWithClient(poetDb, client, poetCfg, logger)

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	clock, err := timesync.NewClock(
		timesync.WithGenesisTime(genesis),
		timesync.WithLayerDuration(layerDuration),
		timesync.WithTickInterval(100*time.Millisecond),
		timesync.WithLogger(zap.NewNop()),
	)
	require.NoError(t, err)
	t.Cleanup(clock.Close)

	localDB := localsql.InMemory()
	nb, err := activation.NewNIPostBuilder(
		localDB,
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		clock,
		validator,
		activation.WithPoetServices(poetService),
	)
	require.NoError(t, err)

	atxdata := atxsdata.New()
	atxVersions := activation.AtxVersions{0: types.AtxV2}
	edVerifier := signing.NewEdVerifier()
	mpub := mocks.NewMockPublisher(ctrl)
	mFetch := smocks.NewMockFetcher(ctrl)
	mBeacon := activation.NewMockatxReceiver(ctrl)
	mTortoise := smocks.NewMockTortoise(ctrl)

	atxHdlr := activation.NewHandler(
		"local",
		cdb,
		atxdata,
		edVerifier,
		clock,
		mpub,
		mFetch,
		goldenATX,
		validator,
		mBeacon,
		mTortoise,
		logger,
		activation.WithAtxVersions(atxVersions),
	)

	tab := activation.NewBuilder(
		activation.Config{GoldenATXID: goldenATX},
		db,
		atxdata,
		localDB,
		mpub,
		nb,
		clock,
		syncer,
		logger,
		activation.WithPoetConfig(poetCfg),
		activation.WithValidator(validator),
		activation.BuilderAtxVersions(atxVersions),
	)
	tab.Register(sig)

	var atx0ID types.ATXID
	mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(ctx context.Context, p string, msg []byte) error {
			var watx wire.ActivationTxV2
			codec.MustDecode(msg, &watx)
			atx0ID = watx.ID()
			peer := peer.ID(p)
			mFetch.EXPECT().RegisterPeerHashes(peer, gomock.Any())
			mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
			mBeacon.EXPECT().OnAtx(gomock.Any())
			mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
			err := atxHdlr.HandleGossipAtx(ctx, peer, msg)
			require.NoError(t, err)
			return err
		},
	)

	require.NoError(t, tab.BuildInitialPost(ctx, sig.NodeID()))
	require.NoError(t, tab.PublishActivationTx(ctx, sig))

	// Execute checkpoint-recovery
	// 1. Generate checkpoint
	require.NoError(t, accounts.Update(db, &types.Account{}))
	snapshot := clock.CurrentLayer()
	fs := afero.NewMemMapFs()
	dir, err := afero.TempDir(fs, "", "Generate")
	require.NoError(t, err)
	err = checkpoint.Generate(ctx, fs, db, dir, snapshot, 1)
	require.NoError(t, err)

	// 2. Recover from checkpoint
	recoveryCfg := checkpoint.RecoverConfig{
		GoldenAtx: goldenATX,
		DataDir:   t.TempDir(),
		DbFile:    "db.sql",
		NodeIDs:   []types.NodeID{sig.NodeID()},
		Restore:   snapshot,
	}
	filename := checkpoint.SelfCheckpointFilename(dir, snapshot)

	data, err := checkpoint.RecoverFromLocalFile(ctx, logger, db, localDB, fs, &recoveryCfg, filename)
	require.NoError(t, err)
	require.Nil(t, data)

	newDB, err := statesql.Open("file:" + recoveryCfg.DbPath())
	require.NoError(t, err)
	defer newDB.Close()

	// 3. Spawn new ATX handler and builder using the new DB
	poetDb, err = activation.NewPoetDb(newDB, logger.Named("poetDb"))
	require.NoError(t, err)
	cdb = datastore.NewCachedDB(newDB, logger)
	atxdata, err = atxsdata.Warm(newDB, 1, logger)
	poetService = activation.NewPoetServiceWithClient(poetDb, client, poetCfg, logger)
	validator = activation.NewValidator(newDB, poetDb, cfg, opts.Scrypt, verifier)
	require.NoError(t, err)
	atxHdlr = activation.NewHandler(
		"local",
		cdb,
		atxdata,
		edVerifier,
		clock,
		mpub,
		mFetch,
		goldenATX,
		validator,
		mBeacon,
		mTortoise,
		logger,
		activation.WithAtxVersions(atxVersions),
	)

	nb, err = activation.NewNIPostBuilder(
		localDB,
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		clock,
		validator,
		activation.WithPoetServices(poetService),
	)
	require.NoError(t, err)

	tab = activation.NewBuilder(
		activation.Config{GoldenATXID: goldenATX},
		newDB,
		atxdata,
		localDB,
		mpub,
		nb,
		clock,
		syncer,
		logger,
		activation.WithPoetConfig(poetCfg),
		activation.WithValidator(validator),
		activation.BuilderAtxVersions(atxVersions),
	)
	tab.Register(sig)

	// Publish ATX after recovery
	mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(ctx context.Context, p string, msg []byte) error {
			var watx wire.ActivationTxV2
			codec.MustDecode(msg, &watx)
			require.Nil(t, watx.Initial)
			require.Len(t, watx.PreviousATXs, 1)
			assert.Equal(t, atx0ID, watx.PreviousATXs[0])

			peer := peer.ID(p)
			mFetch.EXPECT().RegisterPeerHashes(peer, gomock.Any())
			mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
			mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
			mBeacon.EXPECT().OnAtx(gomock.Any())
			mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
			err := atxHdlr.HandleGossipAtx(ctx, peer, msg)
			require.NoError(t, err)
			return err
		},
	)
	require.NoError(t, tab.PublishActivationTx(ctx, sig))
}
