package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	grpc_logsettable "github.com/grpc-ecosystem/go-grpc-middleware/logging/settable"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

var grpclog grpc_logsettable.SettableLoggerV2

func init() {
	grpclog = grpc_logsettable.ReplaceGrpcLoggerV2()
}

func TestPostMalfeasanceProof(t *testing.T) {
	t.Parallel()
	testDir := t.TempDir()

	ctx := testcontext.New(t, testcontext.Labels("sanity"))
	logger := ctx.Log.Desugar().WithOptions(zap.IncreaseLevel(zapcore.InfoLevel), zap.WithCaller(false))

	// Prepare cluster
	ctx.PoetSize = 1 // one poet guarantees everybody gets the same proof
	ctx.ClusterSize = 3
	cl := cluster.New(ctx, cluster.WithKeys(10))
	require.NoError(t, cl.AddBootnodes(ctx, 1))
	require.NoError(t, cl.AddBootstrappers(ctx))
	require.NoError(t, cl.AddPoets(ctx))
	require.NoError(t, cl.AddSmeshers(ctx, ctx.ClusterSize-cl.Total(), cluster.WithFlags(cluster.PostK3(1))))

	// Prepare config
	cfg, err := cl.NodeConfig(ctx)
	require.NoError(t, err)

	types.SetLayersPerEpoch(cfg.LayersPerEpoch)
	cfg.DataDirParent = testDir
	cfg.SMESHING.Opts.DataDir = filepath.Join(testDir, "post-data")
	cfg.P2P.DataDir = filepath.Join(testDir, "p2p-dir")
	require.NoError(t, os.Mkdir(cfg.P2P.DataDir, os.ModePerm))

	cfg.POET.RequestTimeout = time.Minute
	cfg.POET.MaxRequestRetries = 10
	cfg.PoetServers = []types.PoetServer{
		{Address: cluster.MakePoetGlobalEndpoint(ctx.Namespace, 0)},
	}

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
	ctx.Log.Debugw("Prepared config", "cfg", cfg)

	goldenATXID := cl.GoldenATX()
	signer, err := signing.NewEdSigner(signing.WithPrefix(cl.GenesisID().Bytes()))
	require.NoError(t, err)

	prologue := fmt.Sprintf("%x-%v", cl.GenesisID(), cfg.LayersPerEpoch*2-1)
	host, err := p2p.New(
		ctx,
		log.NewFromLog(logger.Named("p2p")),
		cfg.P2P,
		[]byte(prologue),
		handshake.NetworkCookie(prologue),
	)
	require.NoError(t, err)
	logger.Info("p2p host created", zap.Stringer("id", host.ID()))
	host.Register(pubsub.AtxProtocol, func(context.Context, peer.ID, []byte) error { return nil })

	require.NoError(t, host.Start())
	t.Cleanup(func() { assert.NoError(t, host.Stop()) })

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
		datastore.NewCachedDB(sql.InMemory(), log.NewNop()),
		cl.GoldenATX(),
		syncer,
		activation.NewMocknipostValidator(ctrl),
	)
	require.NoError(t, err)

	builder := activation.NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(signer)
	postSupervisor := activation.NewPostSupervisor(
		logger.Named("post-supervisor"),
		cfg.POST,
		cfg.SMESHING.ProvingOpts,
		postSetupMgr,
		builder,
	)
	require.NoError(t, postSupervisor.Start(cfg.POSTService, cfg.SMESHING.Opts, signer))
	t.Cleanup(func() { assert.NoError(t, postSupervisor.Stop(false)) })

	// 2. create ATX with invalid POST labels
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(cl.Genesis()),
		timesync.WithLogger(logger.Named("clock")),
	)
	require.NoError(t, err)
	t.Cleanup(clock.Close)

	grpcPostService := grpcserver.NewPostService(logger.Named("grpc-post-service"))
	grpcPostService.AllowConnections(true)
	grpczap.SetGrpcLoggerV2(grpclog, logger.Named("grpc"))
	grpcPrivateServer, err := grpcserver.NewWithServices(
		cfg.API.PostListener,
		logger.Named("grpc-server"),
		cfg.API,
		[]grpcserver.ServiceAPI{grpcPostService},
	)
	require.NoError(t, err)
	require.NoError(t, grpcPrivateServer.Start())
	t.Cleanup(func() { assert.NoError(t, grpcPrivateServer.Close()) })

	nipostBuilder, err := activation.NewNIPostBuilder(
		localsql.InMemory(),
		activation.NewPoetDb(sql.InMemory(), log.NewNop()),
		grpcPostService,
		cfg.PoetServers,
		logger.Named("nipostBuilder"),
		cfg.POET,
		clock,
	)
	require.NoError(t, err)

	// 2.1. Create initial POST
	var challenge *types.NIPostChallenge
	for {
		client, err := grpcPostService.Client(signer.NodeID())
		if err != nil {
			ctx.Log.Info("waiting for poet service to connect")
			time.Sleep(time.Second)
			continue
		}
		ctx.Log.Info("poet service to connected")
		post, postInfo, err := client.Proof(ctx, shared.ZeroChallenge)
		require.NoError(t, err)

		challenge = &types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   2,
			PositioningATX: goldenATXID,
			CommitmentATX:  &postInfo.CommitmentATX,
			InitialPost:    post,
		}
		break
	}
	challengeHash := wire.NIPostChallengeToWireV1(challenge).Hash()
	nipost, err := nipostBuilder.BuildNIPost(ctx, signer, challenge.PublishEpoch, challengeHash)
	require.NoError(t, err)

	// 2.2 Create ATX with invalid POST
	for i := range nipost.Post.Indices {
		nipost.Post.Indices[i] += 1
	}
	// Sanity check that the POST is invalid
	verifyingOpts := activation.DefaultPostVerifyingOpts()
	verifyingOpts.Workers = 1
	verifier, err := activation.NewPostVerifier(cfg.POST, logger, activation.WithVerifyingOpts(verifyingOpts))
	require.NoError(t, err)
	err = verifier.Verify(ctx, (*shared.Proof)(nipost.Post), &shared.ProofMetadata{
		NodeId:          signer.NodeID().Bytes(),
		CommitmentAtxId: challenge.CommitmentATX.Bytes(),
		NumUnits:        nipost.NumUnits,
		Challenge:       nipost.PostMetadata.Challenge,
		LabelsPerUnit:   nipost.PostMetadata.LabelsPerUnit,
	})
	var invalidIdxError *verifying.ErrInvalidIndex
	require.ErrorAs(t, err, &invalidIdxError)

	atx := types.NewActivationTx(
		*challenge,
		types.Address{1, 2, 3, 4},
		nipost.NIPost,
		nipost.NumUnits,
		&nipost.VRFNonce,
	)
	nodeID := signer.NodeID()
	atx.InnerActivationTx.NodeID = &nodeID
	require.NoError(t, activation.SignAndFinalizeAtx(signer, atx))

	// 3. Wait for publish epoch
	epoch := atx.PublishEpoch
	logger.Sugar().Infow("waiting for publish epoch", "epoch", epoch, "layer", epoch.FirstLayer())
	err = layersStream(ctx, cl.Client(0), logger, func(resp *pb.LayerStreamResponse) (bool, error) {
		logger.Info("new layer", zap.Uint32("layer", resp.Layer.Number.Number))
		return resp.Layer.Number.Number < epoch.FirstLayer().Uint32(), nil
	})
	require.NoError(t, err)

	// 4. Publish ATX
	publishCtx, stopPublishing := context.WithCancel(ctx.Context)
	defer stopPublishing()
	var eg errgroup.Group
	t.Cleanup(func() { assert.NoError(t, eg.Wait()) })
	eg.Go(func() error {
		for {
			logger.Sugar().Infow("publishing ATX", "atx", atx)
			buf, err := codec.Encode(wire.ActivationTxToWireV1(atx))
			require.NoError(t, err)
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
	err = malfeasanceStream(awaitCtx, cl.Client(0), logger, func(malf *pb.MalfeasanceStreamResponse) (bool, error) {
		stopPublishing()
		logger.Info("malfeasance proof received")
		require.Equal(t, malf.GetProof().GetSmesherId().Id, signer.NodeID().Bytes())
		require.Equal(t, pb.MalfeasanceProof_MALFEASANCE_POST_INDEX, malf.GetProof().GetKind())

		var proof mwire.MalfeasanceProof
		require.NoError(t, codec.Decode(malf.Proof.Proof, &proof))
		require.Equal(t, mwire.InvalidPostIndex, proof.Proof.Type)
		invalidPostProof := proof.Proof.Data.(*mwire.InvalidPostIndexProof)
		logger.Sugar().Infow("malfeasance post proof", "proof", invalidPostProof)
		invalidAtx := invalidPostProof.Atx
		require.Equal(t, atx.PublishEpoch, invalidAtx.Publish)
		require.Equal(t, atx.SmesherID, invalidAtx.SmesherID)
		require.Equal(t, atx.ID().Hash32(), invalidAtx.HashInnerBytes())

		meta := &shared.ProofMetadata{
			NodeId:          invalidAtx.NodeID.Bytes(),
			CommitmentAtxId: invalidAtx.CommitmentATX.Bytes(),
			NumUnits:        invalidAtx.NumUnits,
			Challenge:       invalidAtx.NIPost.PostMetadata.Challenge,
			LabelsPerUnit:   invalidAtx.NIPost.PostMetadata.LabelsPerUnit,
		}
		err = verifier.Verify(awaitCtx, (*shared.Proof)(invalidAtx.NIPost.Post), meta)
		var invalidIdxError *verifying.ErrInvalidIndex
		require.ErrorAs(t, err, &invalidIdxError)
		receivedProof = true
		return false, nil
	})
	require.NoError(t, err)
	require.True(t, receivedProof, "malfeasance proof not received")
}
