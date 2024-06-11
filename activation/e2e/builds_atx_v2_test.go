package activation_test

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func testPostSetupOpts(t *testing.T) activation.PostSetupOpts {
	t.Helper()
	opts := activation.DefaultPostSetupOpts()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	opts.DataDir = t.TempDir()
	opts.NumUnits = 4
	return opts
}

func TestBuilder_SwitchesToBuildV2(t *testing.T) {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	coinbase := types.Address{1, 2, 3, 4, 5, 6, 7, 8}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	cfg := activation.DefaultPostConfig()
	db := sql.InMemory()
	cdb := datastore.NewCachedDB(db, logger)

	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		synced := make(chan struct{})
		close(synced)
		return synced
	}).AnyTimes()

	svc := grpcserver.NewPostService(logger)
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	poetDb := activation.NewPoetDb(db, logger.Named("poetDb"))
	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })
	opts := testPostSetupOpts(t)
	validator := activation.NewValidator(db, poetDb, cfg, opts.Scrypt, verifier)

	atxsdata := atxsdata.New()
	mgr, err := activation.NewPostSetupManager(cfg, logger, cdb, atxsdata, goldenATX, syncer, validator)
	require.NoError(t, err)

	initPost(t, mgr, opts, sig.NodeID())
	stop := launchPostSupervisor(t, logger, mgr, sig, grpcCfg, opts)
	t.Cleanup(stop)

	require.Eventually(t, func() bool {
		_, err := svc.Client(sig.NodeID())
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	layerDuration := 2 * time.Second
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:        epoch,
		CycleGap:          epoch / 2,
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

	clock, err := timesync.NewClock(
		timesync.WithGenesisTime(genesis),
		timesync.WithLayerDuration(layerDuration),
		timesync.WithTickInterval(100*time.Millisecond),
		timesync.WithLogger(zap.NewNop()),
	)
	require.NoError(t, err)
	t.Cleanup(clock.Close)

	client, err := activation.NewPoetClient(
		poetDb,
		poetProver.ServerCfg(),
		poetCfg,
		logger,
	)
	require.NoError(t, err)

	postStates := activation.NewMockPostStates(ctrl)
	localDB := localsql.InMemory()
	nb, err := activation.NewNIPostBuilder(
		localDB,
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		clock,
		activation.NipostbuilderWithPostStates(postStates),
		activation.WithPoetClients(client),
	)
	require.NoError(t, err)

	conf := activation.Config{
		GoldenATXID:      goldenATX,
		RegossipInterval: 0,
	}

	atxVersions := activation.AtxVersions{postGenesisEpoch: types.AtxV2}
	edVerifier := signing.NewEdVerifier()
	mpub := mocks.NewMockPublisher(ctrl)
	mFetch := smocks.NewMockFetcher(ctrl)
	mBeacon := activation.NewMockAtxReceiver(ctrl)
	mTortoise := smocks.NewMockTortoise(ctrl)

	atxHdlr := activation.NewHandler(
		"local",
		cdb,
		atxsdata,
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

	var previous *types.ActivationTx
	gomock.InOrder(
		mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ string, msg []byte) error {
				var watx wire.ActivationTxV1
				codec.MustDecode(msg, &watx)

				require.Equal(t, sig.NodeID(), watx.SmesherID)
				require.EqualValues(t, 1, watx.PublishEpoch)
				require.Equal(t, types.EmptyATXID, watx.PrevATXID)
				require.Equal(t, goldenATX, watx.PositioningATXID)
				require.Equal(t, coinbase, watx.Coinbase)

				mFetch.EXPECT().RegisterPeerHashes(peer.ID("peer"), gomock.Any())
				mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
				mBeacon.EXPECT().OnAtx(gomock.Any())
				mTortoise.EXPECT().OnAtx(watx.PublishEpoch+1, watx.ID(), gomock.Any())

				require.NoError(t, atxHdlr.HandleGossipAtx(ctx, "peer", msg))

				atx, err := atxs.Get(db, watx.ID())
				require.NoError(t, err)
				previous = atx
				return nil
			},
		),
		mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ string, msg []byte) error {
				var watx wire.ActivationTxV2
				codec.MustDecode(msg, &watx)

				require.Equal(t, sig.NodeID(), watx.SmesherID)
				require.EqualValues(t, previous.PublishEpoch+1, watx.PublishEpoch)
				require.Equal(t, previous.ID(), watx.PreviousATXs[0])
				require.Equal(t, previous.ID(), watx.PositioningATX)
				require.Equal(t, coinbase, watx.Coinbase)

				mFetch.EXPECT().RegisterPeerHashes(peer.ID("peer"), gomock.Any())
				mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
				mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
				mBeacon.EXPECT().OnAtx(gomock.Any())
				mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
				require.NoError(t, atxHdlr.HandleGossipAtx(ctx, "peer", msg))

				atx, err := atxs.Get(db, watx.ID())
				require.NoError(t, err)
				require.Equal(t, opts.NumUnits, atx.NumUnits)
				require.Equal(t, coinbase, atx.Coinbase)

				require.NotZero(t, atx.BaseTickHeight)
				require.NotZero(t, atx.TickCount)
				require.NotZero(t, atx.GetWeight())
				require.NotZero(t, atx.TickHeight())
				require.Equal(t, opts.NumUnits, atx.NumUnits)
				previous = atx
				return nil
			},
		).Times(2),
	)

	tab := activation.NewBuilder(
		conf,
		db,
		atxsdata,
		localDB,
		mpub,
		nb,
		clock,
		syncer,
		logger,
		activation.WithPoetConfig(poetCfg),
		activation.WithValidator(validator),
		activation.WithPostStates(postStates),
		activation.BuilderAtxVersions(atxVersions),
	)
	gomock.InOrder(
		// it starts by setting to IDLE
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
		// initial proof
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateProving),
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
		// post proof - 1st epoch
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateProving),
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
		// 2nd epoch
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateProving),
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
		// 3rd epoch
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateProving),
		postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
	)
	tab.Register(sig)

	require.NoError(t, tab.StartSmeshing(coinbase))
	require.Eventually(t, ctrl.Satisfied, epoch*4, time.Second)
	require.NoError(t, tab.StopSmeshing(false))
}
