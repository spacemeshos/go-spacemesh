package activation_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func Test_BuilderWithMultipleClients(t *testing.T) {
	ctrl := gomock.NewController(t)

	numSigners := 3
	signers := make(map[types.NodeID]*signing.EdSigner, numSigners)
	for range numSigners {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		signers[sig.NodeID()] = sig
	}

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := activation.DefaultPostConfig()
	db := sql.InMemory()
	cdb := datastore.NewCachedDB(db, log.NewFromLog(logger))

	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		synced := make(chan struct{})
		close(synced)
		return synced
	}).AnyTimes()

	svc := grpcserver.NewPostService(logger)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	var eg errgroup.Group
	i := uint32(1)
	for _, sig := range signers {
		opts := opts
		opts.DataDir = t.TempDir()
		opts.NumUnits = min(i*2, cfg.MaxNumUnits)
		i += 1
		eg.Go(func() error {
			validator := activation.NewMocknipostValidator(ctrl)
			mgr, err := activation.NewPostSetupManager(cfg, logger, cdb, goldenATX, syncer, validator)
			require.NoError(t, err)

			initPost(t, mgr, opts, sig.NodeID())
			t.Cleanup(launchPostSupervisor(t, logger, mgr, sig, grpcCfg, opts))

			require.Eventually(t, func() bool {
				_, err := svc.Client(sig.NodeID())
				return err == nil
			}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	layerDuration := 3 * time.Second
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
		timesync.WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(clock.Close)

	poetDb := activation.NewPoetDb(db, log.NewFromLog(logger).Named("poetDb"))

	postStates := activation.NewMockPostStates(ctrl)
	localDB := localsql.InMemory()
	nb, err := activation.NewNIPostBuilder(
		localDB,
		poetDb,
		svc,
		[]types.PoetServer{{Address: poetProver.RestURL().String()}},
		logger.Named("nipostBuilder"),
		poetCfg,
		clock,
		activation.NipostbuilderWithPostStates(postStates),
	)
	require.NoError(t, err)

	conf := activation.Config{
		GoldenATXID:      goldenATX,
		RegossipInterval: 0,
	}

	var atxMtx sync.Mutex
	atxs := make(map[types.NodeID]types.ActivationTx)
	endChan := make(chan struct{})
	mpub := mocks.NewMockPublisher(ctrl)
	mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(ctx context.Context, topic string, got []byte) error {
			atxMtx.Lock()
			defer atxMtx.Unlock()
			var gotAtx types.ActivationTx
			require.NoError(t, codec.Decode(got, &gotAtx))
			atxs[gotAtx.SmesherID] = gotAtx
			if len(atxs) == numSigners {
				close(endChan)
			}
			return nil
		},
	).Times(numSigners)

	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })
	v := activation.NewValidator(nil, poetDb, cfg, opts.Scrypt, verifier)
	tab := activation.NewBuilder(
		conf,
		cdb,
		localDB,
		mpub,
		nb,
		clock,
		syncer,
		logger,
		activation.WithPoetConfig(poetCfg),
		activation.WithValidator(v),
		activation.WithPostStates(postStates),
	)
	for _, sig := range signers {
		gomock.InOrder(
			// it starts by setting to IDLE
			postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
			// initial proof
			postStates.EXPECT().Set(sig.NodeID(), types.PostStateProving),
			postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
			// post proof
			postStates.EXPECT().Set(sig.NodeID(), types.PostStateProving),
			postStates.EXPECT().Set(sig.NodeID(), types.PostStateIdle),
		)
		tab.Register(sig)
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	<-endChan
	require.NoError(t, tab.StopSmeshing(false))

	for _, sig := range signers {
		atx := atxs[sig.NodeID()]

		_, err = v.NIPost(
			context.Background(),
			sig.NodeID(),
			*atx.CommitmentATX,
			atx.NIPost,
			atx.NIPostChallenge.Hash(),
			atx.NumUnits,
		)
		require.NoError(t, err)

		err := v.VRFNonce(
			sig.NodeID(),
			*atx.CommitmentATX,
			atx.VRFNonce,
			atx.NIPost.PostMetadata,
			atx.NumUnits,
		)
		require.NoError(t, err)

		require.Equal(t, postGenesisEpoch, atx.NIPostChallenge.TargetEpoch())
		require.Equal(t, types.EmptyATXID, atx.NIPostChallenge.PrevATXID)
		require.Equal(t, goldenATX, atx.NIPostChallenge.PositioningATX)
		require.Equal(t, uint64(0), atx.NIPostChallenge.Sequence)

		require.Equal(t, types.Address{}, atx.Coinbase)
		require.Equal(t, sig.NodeID(), *atx.NodeID)
	}
}
