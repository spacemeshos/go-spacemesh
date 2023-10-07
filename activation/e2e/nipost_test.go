package activation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacemeshos/poet/logging"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
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
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
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

func launchPostSupervisor(tb testing.TB, log *zap.Logger, cfg grpcserver.Config, postDir string) func() {
	path, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(tb, err)

	opts := activation.PostSupervisorConfig{
		PostServiceCmd:  filepath.Join(filepath.Dir(string(path)), "build", "service"),
		DataDir:         postDir,
		NodeAddress:     fmt.Sprintf("http://%s", cfg.PublicListener),
		PowDifficulty:   activation.DefaultPostConfig().PowDifficulty,
		PostServiceMode: "light",
		N:               2,
	}

	ps, err := activation.NewPostSupervisor(log, opts)
	require.NoError(tb, err)
	require.NotNil(tb, ps)
	return func() { assert.NoError(tb, ps.Close()) }
}

func launchServer(tb testing.TB, services ...grpcserver.ServiceAPI) (grpcserver.Config, func()) {
	cfg := grpcserver.DefaultTestConfig()

	// run on random ports
	grpcService := grpcserver.New("127.0.0.1:0", logtest.New(tb).Named("grpc"))

	// attach services
	for _, svc := range services {
		svc.RegisterService(grpcService)
	}

	require.NoError(tb, grpcService.Start())

	// update config with bound addresses
	cfg.PublicListener = grpcService.BoundAddress

	return cfg, func() { assert.NoError(tb, grpcService.Close()) }
}

func initPost(tb testing.TB, logger *zap.Logger, mgr *activation.PostSetupManager, opts activation.PostSetupOpts) {
	tb.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var eg errgroup.Group
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

		lastStatus := &activation.PostSetupStatus{}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				status := mgr.Status()
				require.GreaterOrEqual(tb, status.NumLabelsWritten, lastStatus.NumLabelsWritten)

				if status.NumLabelsWritten == uint64(mgr.LastOpts().NumUnits)*mgr.Config().LabelsPerUnit {
					return nil
				}
				require.Contains(tb, []activation.PostSetupState{activation.PostSetupStatePrepared, activation.PostSetupStateInProgress}, status.State)
				lastStatus = status
			}
		}
	})

	// Create data.
	require.NoError(tb, mgr.PrepareInitializer(context.Background(), opts))
	require.NoError(tb, mgr.StartSession(context.Background()))
	require.NoError(tb, eg.Wait())
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

	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.Flags = config.RecommendedPowFlags()

	mgr, err := activation.NewPostSetupManager(sig.NodeID(), cfg, logger, cdb, goldenATX, provingOpts)
	require.NoError(t, err)

	postDir := t.TempDir()
	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = postDir
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
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
	poetProver := spawnPoet(t, WithGenesis(time.Now()), WithEpochDuration(epoch), WithPhaseShift(poetCfg.PhaseShift), WithCycleGap(poetCfg.CycleGap))

	mclock := activation.NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(postGenesisEpoch.FirstLayer()) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)

	verifier, err := activation.NewPostVerifier(mgr.Config(), logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })

	poetDb := activation.NewPoetDb(sql.InMemory(), log.NewFromLog(logger).Named("poetDb"))
	v := activation.NewValidator(poetDb, mgr.Config(), mgr.LastOpts().Scrypt, verifier)

	nb, err := activation.NewNIPostBuilder(
		sig.NodeID(),
		mgr,
		poetDb,
		[]string{poetProver.RestURL().String()},
		t.TempDir(),
		log.NewFromLog(logger),
		sig,
		poetCfg,
		mclock,
		activation.WithNipostValidator(v),
	)
	require.NoError(t, err)

	connected := make(chan struct{})
	con := grpcserver.NewMockpostConnectionListener(ctrl)
	con.EXPECT().Connected(gomock.Any()).DoAndReturn(func(c activation.PostClient) {
		close(connected)
	}).Times(1)
	con.EXPECT().Disconnected(gomock.Any()).Times(1)

	svc := grpcserver.NewPostService(logger, nb, con)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	t.Cleanup(launchPostSupervisor(t, logger, grpcCfg, postDir))

	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for connection")
	}

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	require.NoError(t, err)
	_, err = v.NIPost(
		context.Background(),
		sig.NodeID(),
		goldenATX,
		nipost,
		challenge.Hash(),
		mgr.LastOpts().NumUnits,
	)
	require.NoError(t, err)
}

func TestNIPostBuilder_Close(t *testing.T) {
	ctrl := gomock.NewController(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)

	nipostClient := activation.NewMocknipostClient(ctrl)
	nipostClient.EXPECT().Status().Return(&activation.PostSetupStatus{State: activation.PostSetupStateComplete})
	poetProver := spawnPoet(t, WithGenesis(time.Now()), WithEpochDuration(time.Second))

	poetDb := activation.NewMockpoetDbAPI(ctrl)

	mclock := activation.NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(postGenesisEpoch.FirstLayer()) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)

	nb, err := activation.NewNIPostBuilder(
		sig.NodeID(),
		nipostClient,
		poetDb,
		[]string{poetProver.RestURL().String()},
		t.TempDir(),
		log.NewFromLog(logger),
		sig,
		activation.PoetConfig{},
		mclock,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	nipost, err := nb.BuildNIPost(ctx, &challenge)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, nipost)
}

func TestNewNIPostBuilderNotInitialized(t *testing.T) {
	ctrl := gomock.NewController(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := activation.DefaultPostConfig()
	cdb := datastore.NewCachedDB(sql.InMemory(), log.NewFromLog(logger))

	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.Flags = config.RecommendedPowFlags()

	mgr, err := activation.NewPostSetupManager(sig.NodeID(), cfg, logger, cdb, goldenATX, provingOpts)
	require.NoError(t, err)

	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:        epoch / 5,
		CycleGap:          epoch / 10,
		GracePeriod:       epoch / 10,
		RequestTimeout:    epoch / 10,
		RequestRetryDelay: epoch / 100,
		MaxRequestRetries: 10,
	}
	poetProver := spawnPoet(t, WithGenesis(time.Now()), WithEpochDuration(epoch), WithPhaseShift(poetCfg.PhaseShift), WithCycleGap(poetCfg.CycleGap))

	mclock := activation.NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(postGenesisEpoch.FirstLayer()) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)

	poetDb := activation.NewPoetDb(sql.InMemory(), log.NewFromLog(logger).Named("poetDb"))
	nipostValidator := activation.NewMocknipostValidator(ctrl)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)

	nb, err := activation.NewNIPostBuilder(
		sig.NodeID(),
		mgr,
		poetDb,
		[]string{poetProver.RestURL().String()},
		t.TempDir(),
		logtest.New(t),
		sig,
		poetCfg,
		mclock,
		activation.WithNipostValidator(nipostValidator),
	)
	require.NoError(t, err)

	connected := make(chan struct{})
	con := grpcserver.NewMockpostConnectionListener(ctrl)
	con.EXPECT().Connected(gomock.Any()).DoAndReturn(func(c activation.PostClient) {
		close(connected)
	}).Times(1)
	con.EXPECT().Disconnected(gomock.Any()).Times(1)

	svc := grpcserver.NewPostService(logger, nb, con)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	postDir := t.TempDir()
	t.Cleanup(launchPostSupervisor(t, logger, grpcCfg, postDir))

	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for connection")
	}

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	require.EqualError(t, err, "post setup not complete")
	require.Nil(t, nipost)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = postDir
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	initPost(t, logger.Named("manager"), mgr, opts)

	nipost, err = nb.BuildNIPost(context.Background(), &challenge)
	require.NoError(t, err)
	require.NotNil(t, nipost)

	verifier, err := activation.NewPostVerifier(mgr.Config(), logger.Named("verifier"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })

	v := activation.NewValidator(poetDb, mgr.Config(), mgr.LastOpts().Scrypt, verifier)
	_, err = v.NIPost(
		context.Background(),
		sig.NodeID(),
		goldenATX,
		nipost,
		challenge.Hash(),
		mgr.LastOpts().NumUnits,
	)
	require.NoError(t, err)
}
