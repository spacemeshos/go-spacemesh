package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	pb2 "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type builtAtx interface {
	ID() types.ATXID

	scale.Encodable
	zapcore.ObjectMarshaler
}

func version(cfg *config.Config, publish types.EpochID) types.AtxVersion {
	cfg.AtxVersions[0] = types.AtxV1
	epochs := maps.Keys(cfg.AtxVersions)
	slices.Sort(epochs)
	version := types.AtxV1
	for _, epoch := range epochs {
		if publish >= epoch {
			version = cfg.AtxVersions[epoch]
		}
	}
	return version
}

// TestPostMalfeasanceProof tests that nodes can detect an invalid PoST and create a malfeasance proof against it.
func TestPostMalfeasanceProof(t *testing.T) {
	t.Parallel()

	ctx := testcontext.New(t)

	// Prepare cluster
	ctx.PoetSize = 1 // one poet guarantees everybody gets the same proof
	ctx.ClusterSize = 5
	cl := cluster.New(ctx, cluster.WithKeys(10))
	require.NoError(t, cl.AddBootnodes(ctx, 1))
	require.NoError(t, cl.AddBootstrappers(ctx))
	require.NoError(t, cl.AddPoets(ctx))
	require.NoError(t, cl.AddSmeshers(ctx, ctx.ClusterSize-cl.Total(), cluster.WithFlags(cluster.PostK3(1))))

	logger := ctx.Log.Desugar().WithOptions(zap.IncreaseLevel(zap.InfoLevel), zap.WithCaller(false))
	cfg := getConfig(t, logger, cl, ctx)

	// Test malfeasance for each ATX version, malfeasance1 in first epoch
	publishEpoch := types.EpochID(1)
	testPostMalfeasance(t, cfg, cl, logger, ctx, publishEpoch)

	for k, v := range cfg.AtxVersions {
		if v == 2 {
			publishEpoch = types.EpochID(k)
		}
	}

	// malfeasance2 in first epoch with ATXv2
	testPostMalfeasance(t, cfg, cl, logger, ctx, publishEpoch)
}

func testPostMalfeasance(
	t *testing.T,
	cfg *config.Config,
	cl *cluster.Cluster,
	logger *zap.Logger,
	ctx *testcontext.Context,
	publishEpoch types.EpochID,
) {
	// Prepare config
	testDir := t.TempDir()

	cfg.DataDirParent = testDir
	cfg.SMESHING.Opts.DataDir = filepath.Join(testDir, "post-data")
	cfg.P2P.DataDir = filepath.Join(testDir, "p2p-dir")
	require.NoError(t, os.Mkdir(cfg.P2P.DataDir, os.ModePerm))

	signer, err := signing.NewEdSigner(signing.WithPrefix(cl.GenesisID().Bytes()))
	require.NoError(t, err)

	prologue := fmt.Sprintf("%x-%v", cl.GenesisID(), cfg.LayersPerEpoch*2-1)
	host, err := p2p.New(
		logger.Named("p2p"),
		cfg.P2P,
		[]byte(prologue),
		handshake.NetworkCookie(prologue),
	)
	require.NoError(t, err)
	logger.Info("p2p host created", zap.Stringer("id", host.ID()))
	host.Register(pubsub.AtxProtocol, func(context.Context, peer.ID, []byte) error { return nil })
	require.NoError(t, host.Start())
	defer host.Stop()

	db := statesql.InMemoryTest(t)
	cdb := datastore.NewCachedDB(db, zap.NewNop())
	defer cdb.Close()

	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(cl.Genesis()),
		timesync.WithLogger(logger.Named("clock")),
	)
	require.NoError(t, err)
	defer clock.Close()

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
	require.NoError(t, err)

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

	require.NoError(t, fetcher.Start())
	defer fetcher.Stop()

	ctrl := gomock.NewController(t)
	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).AnyTimes()

	// 1. Initialize
	postSetupMgr, err := activation.NewPostSetupManager(
		cfg.POST,
		logger.Named("post"),
		cdb,
		atxsdata.New(),
		cl.GoldenATX(),
		syncer,
		activation.NewMocknipostValidator(ctrl),
	)
	require.NoError(t, err)

	builder := activation.NewMockatxBuilder(ctrl)
	builder.EXPECT().Register(signer)
	postSupervisor := activation.NewPostSupervisor(
		logger.Named("post-supervisor"),
		cfg.POST,
		cfg.SMESHING.ProvingOpts,
		postSetupMgr,
		builder,
	)
	require.NoError(t, postSupervisor.Start(cfg.POSTService, cfg.SMESHING.Opts, signer))
	defer postSupervisor.Stop(false)

	// 2. create ATX with invalid POST labels
	grpcPostService := grpcserver.NewPostService(
		logger.Named("grpc-post-service"),
		grpcserver.PostServiceQueryInterval(500*time.Millisecond),
	)
	grpcPostService.AllowConnections(true)

	grpcPrivateServer, err := grpcserver.NewWithServices(
		cfg.API.PostListener,
		logger.Named("grpc-server"),
		cfg.API,
		[]grpcserver.ServiceAPI{grpcPostService},
	)
	require.NoError(t, err)
	require.NoError(t, grpcPrivateServer.Start())
	defer grpcPrivateServer.Close()

	localDb := localsql.InMemoryTest(t)
	certClient := activation.NewCertifierClient(db, localDb, logger.Named("certifier"))
	certifier := activation.NewCertifier(localDb, logger, certClient)
	poetDb, err := activation.NewPoetDb(db, zap.NewNop())
	require.NoError(t, err)
	poetService, err := activation.NewPoetService(
		poetDb,
		types.PoetServer{Address: cluster.MakePoetGlobalEndpoint(ctx.Namespace, 0)},
		cfg.POET,
		logger,
		1,
		activation.WithCertifier(certifier),
	)
	require.NoError(t, err)

	verifyingOpts := activation.DefaultPostVerifyingOpts()
	verifyingOpts.Workers = 1
	verifier, err := activation.NewPostVerifier(cfg.POST, logger, activation.WithVerifyingOpts(verifyingOpts))
	require.NoError(t, err)

	validator := activation.NewValidator(
		db,
		poetDb,
		cfg.POST,
		cfg.SMESHING.Opts.Scrypt,
		verifier,
	)

	nipostBuilder, err := activation.NewNIPostBuilder(
		localDb,
		grpcPostService,
		logger.Named("nipostBuilder"),
		cfg.POET,
		clock,
		validator,
		activation.WithPoetServices(poetService),
	)
	require.NoError(t, err)

	// 2.1. Create initial POST
	var client activation.PostClient
	for {
		client, err = grpcPostService.Client(signer.NodeID())
		if err != nil {
			logger.Info("waiting for post service to connect")
			time.Sleep(time.Second)
			continue
		}
		break
	}
	logger.Info("post service connected")
	initialPost, initialPostInfo, err := client.Proof(ctx, shared.ZeroChallenge)
	require.NoError(t, err)

	err = nipost.AddPost(localDb, signer.NodeID(), nipost.Post{
		Nonce:         initialPost.Nonce,
		Indices:       initialPost.Indices,
		Pow:           initialPost.Pow,
		Challenge:     shared.ZeroChallenge,
		NumUnits:      initialPostInfo.NumUnits,
		CommitmentATX: initialPostInfo.CommitmentATX,
		VRFNonce:      *initialPostInfo.Nonce,
	})
	require.NoError(t, err)

	registerEpoch := publishEpoch - 1
	logger.Info("waiting for epoch to register at poet",
		zap.Uint32("register_epoch", uint32(registerEpoch)),
		zap.Uint32("publish_epoch", uint32(publishEpoch)),
	)
	select {
	case <-ctx.Done():
		require.Fail(t, "context canceled")
		return
	case <-clock.AwaitLayer(registerEpoch.FirstLayer()):
	}
	logger.Info("reached register epoch", zap.Uint32("register_epoch", uint32(registerEpoch)))

	registerEpoch = clock.CurrentLayer().GetEpoch()
	publishEpoch = registerEpoch + 1
	nipostChallenge := &types.NIPostChallenge{
		PublishEpoch:   publishEpoch,
		PrevATXID:      types.EmptyATXID,
		PositioningATX: cl.GoldenATX(),
		CommitmentATX:  &initialPostInfo.CommitmentATX,
		InitialPost: &types.Post{
			Nonce:   initialPost.Nonce,
			Indices: initialPost.Indices, Pow: initialPost.Pow,
		},
	}
	err = nipost.AddChallenge(localDb, signer.NodeID(), nipostChallenge)
	require.NoError(t, err)

	version := version(cfg, nipostChallenge.PublishEpoch)
	var challengeHash types.Hash32
	switch version {
	case types.AtxV1:
		challengeHash = wire.NIPostChallengeToWireV1(nipostChallenge).Hash()
	case types.AtxV2:
		challengeHash = wire.NIPostChallengeToWireV2(nipostChallenge).Hash()
	default:
		require.Fail(t, fmt.Sprintf("unsupported ATX version: %v", version))
	}
	nipost, err := nipostBuilder.BuildNIPost(ctx, signer, challengeHash, nipostChallenge)
	require.NoError(t, err)

	// 2.2 Create ATX with invalid POST
	logger.Info("invalidating PoST")
	invalidPost := false
	for i := range nipost.Post.Indices {
		for range 256 {
			nipost.Post.Indices[i] += 1
			err = verifier.Verify(ctx, (*shared.Proof)(nipost.Post), &shared.ProofMetadata{
				NodeId:          signer.NodeID().Bytes(),
				CommitmentAtxId: nipostChallenge.CommitmentATX.Bytes(),
				NumUnits:        nipost.NumUnits,
				Challenge:       nipost.PostMetadata.Challenge,
				LabelsPerUnit:   nipost.PostMetadata.LabelsPerUnit,
			})
			var invalidIdxError *verifying.ErrInvalidIndex
			if errors.As(err, &invalidIdxError) {
				invalidPost = true
				break
			}
		}
		if invalidPost {
			break
		}
	}
	require.True(t, invalidPost, "expected invalid POST")
	logger.Info("PoST invalidated")

	var (
		atx            builtAtx
		expectedDomain pb2.MalfeasanceProof_MalfeasanceDomain
		expectedType   uint32
	)
	expectedProperties := make(map[string]string)
	switch version {
	case types.AtxV1:
		watx := &wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: *wire.NIPostChallengeToWireV1(nipostChallenge),
				Coinbase:          types.Address{1, 2, 3, 4},
				NumUnits:          nipost.NumUnits,
				NIPost:            wire.NiPostToWireV1(nipost.NIPost),
				VRFNonce:          (*uint64)(&nipost.VRFNonce),
			},
		}
		watx.Sign(signer)
		atx = watx
		expectedDomain = pb2.MalfeasanceProof_DOMAIN_UNSPECIFIED
		expectedType = 4
		expectedProperties["atx"] = atx.ID().String()
	case types.AtxV2:
		watx := &wire.ActivationTxV2{
			PublishEpoch:   nipostChallenge.PublishEpoch,
			PositioningATX: nipostChallenge.PositioningATX,
			Coinbase:       types.Address{1, 2, 3, 4},
			VRFNonce:       (uint64)(nipost.VRFNonce),
			NIPosts: []wire.NIPostV2{
				{
					Membership: wire.MerkleProofV2{
						Nodes: nipost.NIPost.Membership.Nodes,
					},
					Challenge: types.Hash32(nipost.PostMetadata.Challenge),
					Posts: []wire.SubPostV2{
						{
							Post:                *wire.PostToWireV1(nipost.Post),
							NumUnits:            nipost.NumUnits,
							MembershipLeafIndex: nipost.NIPost.Membership.LeafIndex,
						},
					},
				},
			},
			Initial: &wire.InitialAtxPartsV2{
				Post:          *wire.PostToWireV1(nipostChallenge.InitialPost),
				CommitmentATX: *nipostChallenge.CommitmentATX,
			},
		}
		watx.Sign(signer)
		atx = watx
		expectedDomain = pb2.MalfeasanceProof_DOMAIN_ACTIVATION
		expectedType = 0
		expectedProperties["type"] = "InvalidPoSTProof"
		expectedProperties["atx"] = atx.ID().String()
	default:
		require.Fail(t, fmt.Sprintf("unsupported ATX version: %v", version))
		return
	}

	// 3. Wait for publish epoch
	require.NoError(t, cl.WaitAll(ctx))
	logger.Info("waiting for publish epoch",
		zap.Uint32("epoch", publishEpoch.Uint32()),
		zap.Uint32("layer", publishEpoch.FirstLayer().Uint32()),
	)
	err = layersStream(ctx, cl.Client(0), logger, func(resp *pb.LayerStreamResponse) (bool, error) {
		logger.Info("new layer", zap.Uint32("layer", resp.Layer.Number.Number))
		return resp.Layer.Number.Number < publishEpoch.FirstLayer().Uint32(), nil
	})
	require.NoError(t, err)

	// 4. Publish ATX
	publishCtx, stopPublishing := context.WithCancel(ctx.Context)
	defer stopPublishing()
	var eg errgroup.Group
	defer eg.Wait()
	eg.Go(func() error {
		for {
			logger.Info("publishing ATX", zap.Object("atx", atx))
			buf := codec.MustEncode(atx)
			err = host.Publish(ctx, pubsub.AtxProtocol, buf)
			require.NoError(t, err)

			select {
			case <-publishCtx.Done():
				return nil
			case <-time.After(10 * time.Second):
			}
		}
	})

	// 5. Wait for POST malfeasance proof
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
		require.Equal(t, expectedDomain, proof.Domain)
		require.Equal(t, expectedType, proof.Type)
		require.Subset(t, proof.Properties, expectedProperties)
		require.Equal(t, atx.ID().ShortString(), proof.Properties["atx"])
		receivedProof = true
		return false, nil
	})
	require.NoError(t, err)
	require.True(t, receivedProof, "malfeasance proof not received")
}

func getConfig(t testing.TB, logger *zap.Logger, cl *cluster.Cluster, ctx *testcontext.Context) *config.Config {
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
	logger.Debug("Prepared config", zap.Any("cfg", cfg))
	return cfg
}
