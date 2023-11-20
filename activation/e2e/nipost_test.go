package activation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/spacemeshos/poet/logging"
	"github.com/spacemeshos/poet/registration"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

const (
	layersPerEpoch                 = 10
	layerDuration                  = time.Second
	postGenesisEpoch types.EpochID = 2
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)
	res := m.Run()
	os.Exit(res)
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
	cfg grpcserver.Config,
	postOpts activation.PostSetupOpts,
) func() {
	cmdCfg := activation.DefaultTestPostServiceConfig()
	cmdCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)
	postCfg := activation.DefaultPostConfig()
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

	syncer := activation.NewMocksyncer(gomock.NewController(tb))
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).AnyTimes()

	ps, err := activation.NewPostSupervisor(log, cmdCfg, postCfg, provingOpts, mgr, syncer)
	require.NoError(tb, err)
	require.NotNil(tb, ps)
	require.NoError(tb, ps.Start(postOpts))
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

func initPost(tb testing.TB, logger *zap.Logger, mgr *activation.PostSetupManager, opts activation.PostSetupOpts) {
	tb.Helper()

	// Create data.
	require.NoError(tb, mgr.PrepareInitializer(opts))
	require.NoError(tb, mgr.StartSession(context.Background()))
	require.Equal(tb, activation.PostSetupStateComplete, mgr.Status().State)
}

func TestNIPostBuilderWithClients(t *testing.T) {
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

	pubKey, addr := spawnTestCertifier(t, cfg, verifying.WithLabelScryptParams(opts.Scrypt))
	certifierCfg := &registration.CertifierConfig{
		URL:    "http://" + addr.String(),
		PubKey: registration.Base64Enc(pubKey),
	}

	poetProver := spawnPoet(
		t,
		WithGenesis(time.Now()),
		WithEpochDuration(epoch),
		WithPhaseShift(poetCfg.PhaseShift),
		WithCycleGap(poetCfg.CycleGap),
		WithCertifier(certifierCfg),
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

	var postClient activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		postClient, err = svc.Client(sig.NodeID())
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	post, info, err := postClient.Proof(context.Background(), shared.ZeroChallenge)
	require.NoError(t, err)

	client, err := activation.NewHTTPPoetClient(
		poetProver.RestURL().String(),
		poetCfg,
		activation.WithLogger(logger),
	)
	require.NoError(t, err)

	certifierClient := activation.NewCertifierClient(zaptest.NewLogger(t), post, info, shared.ZeroChallenge)
	certifier := activation.NewCertifier(localsql.InMemory(), logger, certifierClient)
	certifier.CertifyAll(context.Background(), []activation.PoetClient{client})

	nb, err := activation.NewNIPostBuilder(
		poetDb,
		svc,
		t.TempDir(),
		logger.Named("nipostBuilder"),
		sig,
		poetCfg,
		mclock,
		localsql.InMemory(),
		activation.WithPoetClients(client),
	)
	require.NoError(t, err)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	nipost, err := nb.BuildNIPost(context.Background(), &challenge, certifier)
	require.NoError(t, err)

	v := activation.NewValidator(poetDb, cfg, opts.Scrypt, verifier)
	_, err = v.NIPost(
		context.Background(),
		sig.NodeID(),
		goldenATX,
		nipost,
		challenge.Hash(),
		opts.NumUnits,
	)
	require.NoError(t, err)
}
