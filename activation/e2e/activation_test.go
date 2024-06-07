package activation_test

import (
	"context"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacemeshos/poet/registration"
	poetShared "github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func Test_BuilderWithMultipleClients(t *testing.T) {
	ctrl := gomock.NewController(t)

	const numEpochs = 3
	const numSigners = 3
	const totalAtxs = numEpochs * numSigners

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
	localDB := localsql.InMemory()

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

	var eg errgroup.Group
	i := uint32(1)
	opts := testPostSetupOpts(t)
	for _, sig := range signers {
		opts := opts
		opts.DataDir = t.TempDir()
		opts.NumUnits = min(i*2, cfg.MaxNumUnits)
		i += 1
		eg.Go(func() error {
			validator := activation.NewMocknipostValidator(ctrl)
			mgr, err := activation.NewPostSetupManager(cfg, logger, db, atxsdata.New(), goldenATX, syncer, validator)
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

	pubkey, address := spawnTestCertifier(
		t,
		cfg,
		func(id []byte) *poetShared.Cert {
			exp := time.Now().Add(epoch)
			return &poetShared.Cert{Pubkey: id, Expiration: &exp}
		},
		verifying.WithLabelScryptParams(opts.Scrypt),
	)

	poetProver := spawnPoet(
		t,
		WithGenesis(genesis),
		WithEpochDuration(epoch),
		WithPhaseShift(poetCfg.PhaseShift),
		WithCycleGap(poetCfg.CycleGap),
		WithCertifier(&registration.CertifierConfig{
			URL:    (&url.URL{Scheme: "http", Host: address.String()}).String(),
			PubKey: registration.Base64Enc(pubkey),
		}),
	)
	certClient := activation.NewCertifierClient(db, localDB, logger.Named("certifier"))
	certifier := activation.NewCertifier(localDB, logger, certClient)
	poetDb := activation.NewPoetDb(db, logger.Named("poetDb"))
	client, err := poetProver.Client(poetDb, poetCfg, logger, activation.WithCertifier(certifier))
	require.NoError(t, err)

	clock, err := timesync.NewClock(
		timesync.WithGenesisTime(genesis),
		timesync.WithLayerDuration(layerDuration),
		timesync.WithTickInterval(100*time.Millisecond),
		timesync.WithLogger(zap.NewNop()),
	)
	require.NoError(t, err)
	t.Cleanup(clock.Close)

	postStates := activation.NewMockPostStates(ctrl)
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

	data := atxsdata.New()
	var atxsPublished atomic.Uint32
	var atxMtx sync.Mutex
	gotAtxs := make(map[types.NodeID][]wire.ActivationTxV1)
	endChan := make(chan struct{})
	mpub := mocks.NewMockPublisher(ctrl)
	mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(ctx context.Context, topic string, got []byte) error {
			atxMtx.Lock()
			defer atxMtx.Unlock()

			var gotAtx wire.ActivationTxV1
			codec.MustDecode(got, &gotAtx)
			gotAtxs[gotAtx.SmesherID] = append(gotAtxs[gotAtx.SmesherID], gotAtx)
			atx := wire.ActivationTxFromWireV1(&gotAtx)
			if gotAtx.VRFNonce == nil {
				atx.VRFNonce, err = atxs.NonceByID(db, gotAtx.PrevATXID)
				require.NoError(t, err)
			}
			logger.Debug("persisting ATX", zap.Inline(atx))
			require.NoError(t, atxs.Add(db, atx))
			data.AddFromAtx(atx, false)

			if atxsPublished.Add(1) == totalAtxs {
				close(endChan)
			}
			return nil
		},
	).Times(totalAtxs)

	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })
	v := activation.NewValidator(db, poetDb, cfg, opts.Scrypt, verifier)
	tab := activation.NewBuilder(
		conf,
		db,
		data,
		localDB,
		mpub,
		nb,
		clock,
		syncer,
		logger,
		activation.WithPoetConfig(poetCfg),
		activation.WithValidator(v),
		activation.WithPostStates(postStates),
		activation.WithPoets(client),
	)
	for _, sig := range signers {
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
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	<-endChan
	require.NoError(t, tab.StopSmeshing(false))

	for _, sig := range signers {
		var commitment types.ATXID
		var previous types.ATXID

		for seq, atx := range gotAtxs[sig.NodeID()] {
			logger.Debug("checking ATX", zap.Inline(&atx), zap.Uint64("seq", uint64(seq)))
			if seq == 0 {
				commitment = *atx.CommitmentATXID
				require.Equal(t, sig.NodeID(), *atx.NodeID)
				require.Equal(t, goldenATX, atx.PositioningATXID)
				require.NotNil(t, atx.VRFNonce)
				err := v.VRFNonce(
					sig.NodeID(),
					commitment,
					uint64(*atx.VRFNonce),
					atx.NIPost.PostMetadata.LabelsPerUnit,
					atx.NumUnits,
				)
				require.NoError(t, err)
			} else {
				require.Nil(t, atx.VRFNonce)
				require.Equal(t, previous, atx.PositioningATXID)
			}
			_, err = v.NIPost(
				context.Background(),
				sig.NodeID(),
				commitment,
				wire.NiPostFromWireV1(atx.NIPost),
				atx.NIPostChallengeV1.Hash(),
				atx.NumUnits,
			)
			require.NoError(t, err)

			require.Equal(t, previous, atx.PrevATXID)
			require.Equal(t, postGenesisEpoch.Add(uint32(seq)), atx.PublishEpoch+1)
			require.Equal(t, uint64(seq), atx.Sequence)
			require.Equal(t, types.Address{}, atx.Coinbase)

			previous = atx.ID()
		}
	}
}
