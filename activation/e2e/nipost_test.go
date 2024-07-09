package activation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/spacemeshos/poet/logging"
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

func testPostConfig() activation.PostConfig {
	cfg := activation.DefaultPostConfig()
	// simplify PoST parameters for faster proving
	cfg.K1 = 12
	cfg.K2 = 8
	cfg.K3 = 8
	cfg.LabelsPerUnit = 64
	return cfg
}

func launchPostSupervisor(
	tb testing.TB,
	log *zap.Logger,
	mgr *activation.PostSetupManager,
	sig *signing.EdSigner,
	cfg grpcserver.Config,
	postCfg activation.PostConfig,
	postOpts activation.PostSetupOpts,
) func() {
	cmdCfg := activation.DefaultTestPostServiceConfig()
	cmdCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

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

func initPost(
	tb testing.TB,
	cfg activation.PostConfig,
	opts activation.PostSetupOpts,
	sig *signing.EdSigner,
	golden types.ATXID,
	grpcCfg grpcserver.Config,
	svc *grpcserver.PostService,
) {
	tb.Helper()

	logger := zaptest.NewLogger(tb)
	syncer := syncedSyncer(tb)
	db := statesql.InMemory()
	mgr, err := activation.NewPostSetupManager(cfg, logger, db, atxsdata.New(), golden, syncer, nil)
	require.NoError(tb, err)

	stop := launchPostSupervisor(tb, logger, mgr, sig, grpcCfg, cfg, opts)
	tb.Cleanup(stop)
	require.Eventually(tb, func() bool {
		_, err := svc.Client(sig.NodeID())
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")
}

func TestNIPostBuilderWithClients(t *testing.T) {
	ctrl := gomock.NewController(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := testPostConfig()
	db := statesql.InMemory()
	localDb := localsql.InMemory()

	opts := testPostSetupOpts(t)
	svc := grpcserver.NewPostService(logger)
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

	postClient, err := svc.Client(sig.NodeID())
	require.NoError(t, err)

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

	signers := make([]*signing.EdSigner, 3)
	for i := range 3 {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		signers[i] = sig
	}

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := testPostConfig()
	db := statesql.InMemory()

	opts := testPostSetupOpts(t)
	svc := grpcserver.NewPostService(logger)
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)
	var eg errgroup.Group
	for i, sig := range signers {
		opts := testPostSetupOpts(t)
		opts.NumUnits = min(opts.NumUnits+2*uint32(i)*opts.NumUnits, cfg.MaxNumUnits)
		eg.Go(func() error {
			initPost(t, cfg, opts, sig, goldenATX, grpcCfg, svc)
			return nil
		})
	}
	require.NoError(t, eg.Wait())
	validator := activation.NewMocknipostValidator(ctrl)

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:  epoch / 2,
		CycleGap:    3 * epoch / 4,
		GracePeriod: epoch / 4,
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
