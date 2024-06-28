package activation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/spacemeshos/poet/logging"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	ae2e "github.com/spacemeshos/go-spacemesh/activation/e2e"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

const (
	layersPerEpoch                 = 5
	layerDuration                  = time.Second
	postGenesisEpoch types.EpochID = 2
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)
	res := m.Run()
	os.Exit(res)
}

func fullPost(post *types.Post, info *types.PostInfo, challenge []byte) *nipost.Post {
	return &nipost.Post{
		Nonce:         post.Nonce,
		Indices:       post.Indices,
		Pow:           post.Pow,
		Challenge:     challenge,
		NumUnits:      info.NumUnits,
		CommitmentATX: info.CommitmentATX,
		VRFNonce:      *info.Nonce,
	}
}

func spawnPoet(tb testing.TB, opts ...HTTPPoetOpt) *HTTPPoetTestHarness {
	tb.Helper()
	ctx, cancel := context.WithCancel(logging.NewContext(context.Background(), zaptest.NewLogger(tb)))

	poetProver, err := NewHTTPPoetTestHarness(ctx, tb.TempDir(), opts...)
	require.NoError(tb, err)
	require.NotNil(tb, poetProver)

	var eg errgroup.Group
	tb.Cleanup(func() {
		cancel()
		eg.Wait()
	})
	eg.Go(func() error {
		err := poetProver.Service.Start(ctx)
		return errors.Join(err, poetProver.Service.Close())
	})

	return poetProver
}

func launchPostSupervisor(
	tb testing.TB,
	log *zap.Logger,
	mgr *activation.PostSetupManager,
	sig *signing.EdSigner,
	cfg grpcserver.Config,
	postOpts activation.PostSetupOpts,
) func() {
	cmdCfg := activation.DefaultTestPostServiceConfig()
	cmdCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)
	postCfg := activation.DefaultPostConfig()
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight
	provingOpts.Nonces = 64

	builder := activation.NewMockAtxBuilder(gomock.NewController(tb))
	builder.EXPECT().Register(gomock.Any())
	ps := activation.NewPostSupervisor(log, postCfg, provingOpts, mgr, builder)
	require.NoError(tb, ps.Start(cmdCfg, postOpts, sig))
	return func() { assert.NoError(tb, ps.Stop(false)) }
}

func launchServer(tb testing.TB, services ...grpcserver.ServiceAPI) (grpcserver.Config, func()) {
	cfg := grpcserver.DefaultTestConfig()

	// run on random ports
	server := grpcserver.New("127.0.0.1:0", zaptest.NewLogger(tb).Named("grpc"), cfg)

	// attach services
	for _, svc := range services {
		svc.RegisterService(server.GrpcServer)
	}

	require.NoError(tb, server.Start())

	// update config with bound addresses
	cfg.PublicListener = server.BoundAddress

	return cfg, func() { assert.NoError(tb, server.Close()) }
}

func initPost(tb testing.TB, mgr *activation.PostSetupManager, opts activation.PostSetupOpts, id types.NodeID) {
	tb.Helper()

	// Create data.
	require.NoError(tb, mgr.PrepareInitializer(context.Background(), opts, id))
	require.NoError(tb, mgr.StartSession(context.Background(), id))
	require.Equal(tb, activation.PostSetupStateComplete, mgr.Status().State)
}

func TestNIPostBuilderWithClients(t *testing.T) {
	ctrl := gomock.NewController(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := activation.DefaultPostConfig()
	db := statesql.InMemory()
	cdb := datastore.NewCachedDB(db, logger)
	localDb := localsql.InMemory()

	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().AnyTimes().DoAndReturn(func() <-chan struct{} {
		synced := make(chan struct{})
		close(synced)
		return synced
	})

	validator := activation.NewMocknipostValidator(ctrl)
	mgr, err := activation.NewPostSetupManager(cfg, logger, cdb, atxsdata.New(), goldenATX, syncer, validator)
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

	mclock := activation.NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)

	verifier, err := activation.NewPostVerifier(cfg, logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })

	poetDb := activation.NewPoetDb(db, logger.Named("poetDb"))

	svc := grpcserver.NewPostService(logger)
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	t.Cleanup(launchPostSupervisor(t, logger, mgr, sig, grpcCfg, opts))

	var postClient activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		postClient, err = svc.Client(sig.NodeID())
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	post, info, err := postClient.Proof(context.Background(), shared.ZeroChallenge)
	require.NoError(t, err)
	err = nipost.AddPost(localDb, sig.NodeID(), *fullPost(post, info, shared.ZeroChallenge))
	require.NoError(t, err)

	client := ae2e.NewTestPoetClient(1)
	poetService := activation.NewPoetServiceWithClient(poetDb, client, poetCfg, logger)

	localDB := localsql.InMemory()
	nb, err := activation.NewNIPostBuilder(
		localDB,
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		mclock,
		nil,
		activation.WithPoetServices(poetService),
	)
	require.NoError(t, err)

	challenge := types.RandomHash()
	nipost, err := nb.BuildNIPost(context.Background(), sig, challenge, &types.NIPostChallenge{PublishEpoch: 7})
	require.NoError(t, err)

	v := activation.NewValidator(nil, poetDb, cfg, opts.Scrypt, verifier)
	_, err = v.NIPost(
		context.Background(),
		sig.NodeID(),
		goldenATX,
		nipost.NIPost,
		challenge,
		nipost.NumUnits,
	)
	require.NoError(t, err)
}

func Test_NIPostBuilderWithMultipleClients(t *testing.T) {
	ctrl := gomock.NewController(t)

	signers := make(map[types.NodeID]*signing.EdSigner, 3)
	for range 3 {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		signers[sig.NodeID()] = sig
	}

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := activation.DefaultPostConfig()
	db := statesql.InMemory()

	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().AnyTimes().DoAndReturn(func() <-chan struct{} {
		synced := make(chan struct{})
		close(synced)
		return synced
	})

	svc := grpcserver.NewPostService(logger)
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	validator := activation.NewMocknipostValidator(ctrl)
	var eg errgroup.Group
	for _, sig := range signers {
		opts := opts
		eg.Go(func() error {
			mgr, err := activation.NewPostSetupManager(cfg, logger, db, atxsdata.New(), goldenATX, syncer, validator)
			require.NoError(t, err)

			opts.DataDir = t.TempDir()
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
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:  epoch / 2,
		CycleGap:    epoch / 4,
		GracePeriod: epoch / 5,
	}

	poetDb := activation.NewPoetDb(db, logger.Named("poetDb"))
	client := ae2e.NewTestPoetClient(len(signers))
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

	localDB := localsql.InMemory()
	nb, err := activation.NewNIPostBuilder(
		localDB,
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		mclock,
		validator,
		activation.WithPoetServices(poetService),
	)
	require.NoError(t, err)

	challenge := types.RandomHash()
	for _, sig := range signers {
		eg.Go(func() error {
			post, info, err := nb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge, nil)
			require.NoError(t, err)
			err = nipost.AddPost(localDB, sig.NodeID(), *fullPost(post, info, shared.ZeroChallenge))
			require.NoError(t, err)

			nipost, err := nb.BuildNIPost(context.Background(), sig, challenge, &types.NIPostChallenge{PublishEpoch: 7})
			require.NoError(t, err)

			v := activation.NewValidator(nil, poetDb, cfg, opts.Scrypt, verifier)
			_, err = v.NIPost(
				context.Background(),
				sig.NodeID(),
				goldenATX,
				nipost.NIPost,
				challenge,
				nipost.NumUnits,
			)
			require.NoError(t, err)
			return nil
		})
	}
	require.NoError(t, eg.Wait())
}
