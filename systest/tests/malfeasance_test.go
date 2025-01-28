package tests

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	pb2 "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/malfeasance2"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

// TestPostMalfeasanceProof tests that nodes can detect an invalid PoST and create a malfeasance proof against it.
func TestPostMalfeasanceProof(t *testing.T) {
	t.Parallel()

	ctx := testcontext.New(t)
	logger := ctx.Log.Desugar().WithOptions(zap.IncreaseLevel(zap.InfoLevel), zap.WithCaller(false))

	// Prepare cluster
	ctx.PoetSize = 1 // one poet guarantees everybody gets the same proof
	ctx.ClusterSize = 5
	cl := cluster.New(ctx, cluster.WithKeys(10))
	require.NoError(t, cl.AddBootnodes(ctx, 1))
	require.NoError(t, cl.AddBootstrappers(ctx))
	require.NoError(t, cl.AddPoets(ctx))
	require.NoError(t, cl.AddSmeshers(ctx, ctx.ClusterSize-cl.Total(), cluster.WithFlags(cluster.PostK3(1))))

	var eg errgroup.Group
	eg.Go(func() error {
		// Prepare config
		cfg := getConfig(t, cl, ctx)
		testDir := t.TempDir()

		cfg.DataDirParent = testDir
		cfg.SMESHING.Opts.DataDir = filepath.Join(testDir, "post-data")
		cfg.P2P.DataDir = filepath.Join(testDir, "p2p-dir")
		require.NoError(t, os.Mkdir(cfg.P2P.DataDir, os.ModePerm))

		signer, err := signing.NewEdSigner(signing.WithPrefix(cl.GenesisID().Bytes()))
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		db := statesql.InMemoryTest(t)
		cdb := datastore.NewCachedDB(db, zap.NewNop())
		t.Cleanup(func() { assert.NoError(t, cdb.Close()) })

		host := setupHost(t, logger, cl, cfg)
		clock := setupClock(t, logger, cl, cfg)
		setupFetcher(t, cl, ctx, logger, cfg, db, clock, host)

		syncer := activation.NewMocksyncer(ctrl)
		syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
			ch := make(chan struct{})
			close(ch)
			return ch
		}).AnyTimes()

		initPost(t, cl, ctx, logger, cfg, signer, cdb, syncer)

		verifyingOpts := activation.DefaultPostVerifyingOpts()
		verifyingOpts.Workers = 1
		verifier, err := activation.NewPostVerifier(cfg.POST, logger, activation.WithVerifyingOpts(verifyingOpts))
		require.NoError(t, err)

		localDb := localsql.InMemoryTest(t)
		atx, publishEpoch := createInitialAtx(
			t,
			ctx,
			logger,
			cl,
			cfg,
			signer,
			db,
			localDb,
			clock,
			verifier,
			types.EpochID(1),
		)
		ctx.Log.Desugar().Info("created initial ATX",
			zap.Object("atx", atx),
			zap.Uint32("pub_epoch", publishEpoch.Uint32()),
		)

		publishCtx, stopPublishing := context.WithCancel(ctx.Context)
		defer stopPublishing()
		publishATX(t, cl, ctx, logger, host, publishCtx, publishEpoch, atx)
		ctx.Log.Desugar().Info("published initial ATX",
			zap.Object("atx", atx),
			zap.Uint32("pub_epoch", publishEpoch.Uint32()),
		)

		receivedProof := false
		timeout := time.Minute * 2
		logger.Info("waiting for malfeasance proof", zap.Duration("timeout", timeout))
		awaitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		err = malfeasanceStream(awaitCtx, cl.Client(0), logger, func(proof *pb2.MalfeasanceProof) (bool, error) {
			if !bytes.Equal(proof.GetSmesher(), signer.NodeID().Bytes()) {
				return true, nil
			}
			stopPublishing()
			logger.Info("malfeasance proof received")
			require.Equal(t, 0, proof.Domain)
			require.Equal(t, mwire.InvalidPostIndex, proof.Type)
			require.Equal(t, atx.ID(), proof.Properties["atx"])
			receivedProof = true
			return false, nil
		})
		require.NoError(t, err)
		require.True(t, receivedProof, "malfeasance proof not received")
		return nil
	})

	eg.Go(func() error {
		// Prepare config
		cfg := getConfig(t, cl, ctx)
		testDir := t.TempDir()

		cfg.DataDirParent = testDir
		cfg.SMESHING.Opts.DataDir = filepath.Join(testDir, "post-data")
		cfg.P2P.DataDir = filepath.Join(testDir, "p2p-dir")
		require.NoError(t, os.Mkdir(cfg.P2P.DataDir, os.ModePerm))

		signer, err := signing.NewEdSigner(signing.WithPrefix(cl.GenesisID().Bytes()))
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		db := statesql.InMemoryTest(t)
		cdb := datastore.NewCachedDB(db, zap.NewNop())
		t.Cleanup(func() { assert.NoError(t, cdb.Close()) })

		host := setupHost(t, logger, cl, cfg)
		clock := setupClock(t, logger, cl, cfg)
		setupFetcher(t, cl, ctx, logger, cfg, db, clock, host)

		syncer := activation.NewMocksyncer(ctrl)
		syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
			ch := make(chan struct{})
			close(ch)
			return ch
		}).AnyTimes()

		initPost(t, cl, ctx, logger, cfg, signer, cdb, syncer)

		verifyingOpts := activation.DefaultPostVerifyingOpts()
		verifyingOpts.Workers = 1
		verifier, err := activation.NewPostVerifier(cfg.POST, logger, activation.WithVerifyingOpts(verifyingOpts))
		require.NoError(t, err)

		localDb := localsql.InMemoryTest(t)
		var publishEpoch types.EpochID
		for k, v := range cfg.AtxVersions {
			if v == 2 {
				publishEpoch = types.EpochID(k)
			}
		}
		atx, publishEpoch := createInitialAtx(
			t,
			ctx,
			logger,
			cl,
			cfg,
			signer,
			db,
			localDb,
			clock,
			verifier,
			publishEpoch,
		)

		publishCtx, stopPublishing := context.WithCancel(ctx.Context)
		defer stopPublishing()
		publishATX(t, cl, ctx, logger, host, publishCtx, publishEpoch, atx)

		receivedProof := false
		timeout := time.Minute * 2
		logger.Info("waiting for malfeasance proof", zap.Duration("timeout", timeout))
		awaitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		err = malfeasanceStream(awaitCtx, cl.Client(0), logger, func(proof *pb2.MalfeasanceProof) (bool, error) {
			if !bytes.Equal(proof.GetSmesher(), signer.NodeID().Bytes()) {
				return true, nil
			}
			stopPublishing()
			logger.Info("malfeasance proof received")
			require.Equal(t, malfeasance2.InvalidActivation, proof.Domain)
			require.Equal(t, "InvalidPoSTProof", proof.Type)
			require.Equal(t, atx.ID(), proof.Properties["atx"])
			receivedProof = true
			return false, nil
		})
		require.NoError(t, err)
		require.True(t, receivedProof, "malfeasance proof not received")
		return nil
	})
	eg.Wait()
}

func getConfig(t testing.TB, cl *cluster.Cluster, ctx *testcontext.Context) *config.Config {
	cfg, err := cl.NodeConfig(ctx)
	require.NoError(t, err)

	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	cfg.POET.RequestTimeout = time.Minute
	cfg.POET.MaxRequestRetries = 10

	var bootnodes []*cluster.NodeClient
	for i := 0; i < cl.Bootnodes(); i++ {
		bootnodes = append(bootnodes, cl.Client(i))
	}

	endpoints, err := cluster.ExtractP2PEndpoints(ctx, bootnodes)
	require.NoError(t, err)
	cfg.P2P.Bootnodes = endpoints
	cfg.P2P.PrivateNetwork = true

	cfg.Bootstrap.URL = cluster.BootstrapperGlobalEndpoint(ctx.Namespace, 0)
	cfg.P2P.MinPeers = 2
	ctx.Log.Desugar().Debug("Prepared config", zap.Any("cfg", cfg))
	return cfg
}

func setupHost(tb testing.TB, logger *zap.Logger, cl *cluster.Cluster, cfg *config.Config) *p2p.Host {
	prologue := fmt.Sprintf("%x-%v", cl.GenesisID(), cfg.LayersPerEpoch*2-1)
	host, err := p2p.New(
		logger.Named("p2p"),
		cfg.P2P,
		[]byte(prologue),
		handshake.NetworkCookie(prologue),
	)
	require.NoError(tb, err)
	logger.Info("p2p host created", zap.Stringer("id", host.ID()))
	host.Register(pubsub.AtxProtocol, func(context.Context, peer.ID, []byte) error { return nil })
	require.NoError(tb, host.Start())
	tb.Cleanup(func() { assert.NoError(tb, host.Stop()) })
	return host
}

func setupClock(tb testing.TB, logger *zap.Logger, cl *cluster.Cluster, cfg *config.Config) *timesync.NodeClock {
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(cl.Genesis()),
		timesync.WithLogger(logger.Named("clock")),
	)
	require.NoError(tb, err)
	tb.Cleanup(clock.Close)
	return clock
}

func setupFetcher(
	tb testing.TB,
	cl *cluster.Cluster,
	ctx *testcontext.Context,
	logger *zap.Logger,
	cfg *config.Config,
	db sql.StateDatabase,
	clock *timesync.NodeClock,
	host *p2p.Host,
) *fetch.Fetch {
	proposalsStore := store.New(
		store.WithEvictedLayer(clock.CurrentLayer()),
		store.WithLogger(logger.Named("proposals-store")),
		store.WithCapacity(cfg.Tortoise.Zdist+1),
	)

	fetcher, err := fetch.NewFetch(db, proposalsStore, host,
		peers.New(),
		fetch.WithContext(ctx),
		fetch.WithConfig(cfg.FETCH),
		fetch.WithLogger(logger.Named("fetcher")),
	)
	require.NoError(tb, err)

	fetcher.SetValidators(
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
		fetch.ValidatorFunc(func(context.Context, types.Hash32, peer.ID, []byte) error { return nil }),
	)

	require.NoError(tb, fetcher.Start())
	tb.Cleanup(fetcher.Stop)
	return fetcher
}

func initPost(
	tb testing.TB,
	cl *cluster.Cluster,
	ctx *testcontext.Context,
	logger *zap.Logger,
	cfg *config.Config,
	signer *signing.EdSigner,
	cdb *datastore.CachedDB,
	syncer *activation.Mocksyncer,
) {
	ctrl := gomock.NewController(tb)
	postSetupMgr, err := activation.NewPostSetupManager(
		cfg.POST,
		logger.Named("post"),
		cdb,
		atxsdata.New(),
		cl.GoldenATX(),
		syncer,
		activation.NewMocknipostValidator(ctrl),
	)
	require.NoError(tb, err)

	builder := activation.NewMockatxBuilder(ctrl)
	builder.EXPECT().Register(signer)
	postSupervisor := activation.NewPostSupervisor(
		logger.Named("post-supervisor"),
		cfg.POST,
		cfg.SMESHING.ProvingOpts,
		postSetupMgr,
		builder,
	)
	require.NoError(tb, postSupervisor.Start(cfg.POSTService, cfg.SMESHING.Opts, signer))
	tb.Cleanup(func() { assert.NoError(tb, postSupervisor.Stop(false)) })
}

func publishATX(
	tb testing.TB,
	cl *cluster.Cluster,
	ctx *testcontext.Context,
	logger *zap.Logger,
	host *p2p.Host,
	publishCtx context.Context,
	publishEpoch types.EpochID,
	atx builtAtx,
) {
	// 3. Wait for publish epoch
	require.NoError(tb, cl.WaitAll(ctx))
	logger.Sugar().Infow("waiting for publish epoch", "epoch", publishEpoch, "layer", publishEpoch.FirstLayer())
	err := layersStream(ctx, cl.Client(0), logger, func(resp *pb.LayerStreamResponse) (bool, error) {
		logger.Info("new layer", zap.Uint32("layer", resp.Layer.Number.Number))
		return resp.Layer.Number.Number < publishEpoch.FirstLayer().Uint32(), nil
	})
	require.NoError(tb, err)

	// 4. Publish ATX
	var eg errgroup.Group
	tb.Cleanup(func() { assert.NoError(tb, eg.Wait()) })
	eg.Go(func() error {
		for {
			logger.Info("publishing ATX", zap.Object("atx", atx))
			buf := codec.MustEncode(atx)
			err = host.Publish(ctx, pubsub.AtxProtocol, buf)
			require.NoError(tb, err)

			select {
			case <-publishCtx.Done():
				return nil
			case <-time.After(10 * time.Second):
			}
		}
	})
}
