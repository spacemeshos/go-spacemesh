package tests

import (
	"context"
	"fmt"
	grpc_logsettable "github.com/grpc-ecosystem/go-grpc-middleware/logging/settable"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var (
	grpclog grpc_logsettable.SettableLoggerV2
)

func init() {
	grpclog = grpc_logsettable.ReplaceGrpcLoggerV2()
}

func TestCreatingPostMalfeasanceProof(t *testing.T) {
	t.Parallel()

	testDir := t.TempDir()

	ctx := testcontext.New(t, testcontext.Labels("sanity"))
	cl, err := cluster.Reuse(ctx, cluster.WithKeys(10))
	require.NoError(t, err)

	cfg, err := presets.Get("fastnet")
	require.NoError(t, err)
	cfg.Genesis = &config.GenesisConfig{
		GenesisTime: cl.Genesis().Format(time.RFC3339),
		ExtraData:   cl.GenesisExtraData(),
	}
	cfg.LayersPerEpoch = uint32(testcontext.LayersPerEpoch.Get(ctx.Parameters))
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)
	cfg.LayerDuration = testcontext.LayerDuration.Get(ctx.Parameters)

	cfg.DataDirParent = testDir
	cfg.SMESHING.Opts.DataDir = filepath.Join(testDir, "post-data")
	cfg.P2P.DataDir = filepath.Join(testDir, "post-data")
	require.NoError(t, os.Mkdir(cfg.P2P.DataDir, os.ModePerm))

	cfg.PoetServers = []types.PoetServer{
		{Address: cluster.MakePoetGlobalEndpoint(ctx.Namespace, 0)},
	}
	cfg.POET.MaxRequestRetries = 10
	cfg.POET.RequestTimeout = time.Minute
	cfg.POET.RequestRetryDelay = 5 * time.Second

	cfg.API.PrivateListener = "0.0.0.0:9093"

	ctx.Log.Desugar().Info("Prepared config", zap.Any("cfg", cfg))
	goldenATXID := cl.GoldenATX()

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	var bootnodes []*cluster.NodeClient
	for i := 0; i < cl.Bootnodes(); i++ {
		bootnodes = append(bootnodes, cl.Client(i))
	}

	endpoints, err := cluster.ExtractP2PEndpoints(ctx, bootnodes)
	require.NoError(t, err)
	cfg.P2P.Bootnodes = endpoints
	prologue := fmt.Sprintf("%x-%v", cl.GenesisID(), cfg.LayersPerEpoch*2-1)

	host, err := p2p.New(
		ctx,
		log.NewFromLog(ctx.Log.Desugar().Named("p2p")),
		cfg.P2P,
		[]byte(prologue),
		handshake.NetworkCookie(prologue),
	)
	require.NoError(t, err)
	host.Register(pubsub.AtxProtocol, func(context.Context, peer.ID, []byte) error { return nil })
	ptimesync := peersync.New(
		host,
		host,
		peersync.WithLog(log.NewFromLog(ctx.Log.Named("peersync").Desugar())),
		peersync.WithConfig(cfg.TIME.Peersync),
	)
	ptimesync.Start()
	t.Cleanup(ptimesync.Stop)

	require.NoError(t, host.Start())
	t.Cleanup(func() { host.Stop() })

	mValidator := activation.NewMocknipostValidator(gomock.NewController(t))
	// 1. Initialize
	postSetupMgr, err := activation.NewPostSetupManager(
		signer.NodeID(),
		cfg.POST,
		ctx.Log.Named("post").Desugar(),
		sql.InMemory(),
		cl.GoldenATX(),
		mValidator,
	)
	require.NoError(t, err)

	syncer := activation.NewMocksyncer(gomock.NewController(t))
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).AnyTimes()

	postSupervisor, err := activation.NewPostSupervisor(
		ctx.Log.Named("post-supervisor").Desugar(),
		cfg.POSTService,
		cfg.POST,
		cfg.SMESHING.ProvingOpts,
		postSetupMgr,
		syncer,
	)
	require.NoError(t, err)
	require.NoError(t, postSupervisor.Start(cfg.SMESHING.Opts))

	// 2. create ATX with invalid POST labels
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(cl.Genesis()),
		timesync.WithLogger(log.NewFromLog(ctx.Log.Desugar().Named("clock"))),
	)
	require.NoError(t, err)

	grpcPostService := grpcserver.NewPostService(ctx.Log.Desugar().Named("grpc-post-service"))
	grpczap.SetGrpcLoggerV2(grpclog, ctx.Log.Desugar().Named("grpc"))
	grpcPrivateServer, err := grpcserver.NewPrivate(
		ctx.Log.Desugar().Named("grpc-server"),
		cfg.API,
		[]grpcserver.ServiceAPI{grpcPostService},
	)
	require.NoError(t, err)
	require.NoError(t, grpcPrivateServer.Start())

	nipostBuilder, err := activation.NewNIPostBuilder(
		localsql.InMemory(),
		activation.NewPoetDb(sql.InMemory(), log.NewFromLog(ctx.Log.Desugar().Named("poet-db"))),
		grpcPostService,
		cfg.PoetServers,
		ctx.Log.Desugar().Named("nipostBuilder"),
		signer,
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

	nipost, err := nipostBuilder.BuildNIPost(ctx, challenge)
	require.NoError(t, err)

	// 2.2 Create ATX with invalid POST
	for i := range nipost.Post.Indices {
		nipost.Post.Indices[i] += 1
	}
	atx := types.NewActivationTx(
		*challenge,
		types.Address{1, 2, 3, 4},
		nipost.NIPost,
		nipost.NumUnits,
		&nipost.VRFNonce,
	)
	nodeID := signer.NodeID()
	atx.InnerActivationTx.NodeID = &nodeID
	err = activation.SignAndFinalizeAtx(signer, atx)
	require.NoError(t, err)

	require.NoError(t, cl.WaitAll(ctx))

	// 3. Wait for publish epoch
	err = layersStream(ctx, cl.Client(0), ctx.Log.Desugar(), func(layer *pb.LayerStreamResponse) (bool, error) {
		return layer.Layer.Number.Number == cfg.LayersPerEpoch*2, nil
	})
	require.NoError(t, err)

	// 4. Publish ATX
	buf, err := codec.Encode(atx)
	require.NoError(t, err)
	err = host.Publish(ctx, pubsub.AtxProtocol, buf)
	require.NoError(t, err)

	// 5. Wait for POST malfeasance proof
	err = malfeasanceStream(ctx, cl.Client(0), ctx.Log.Desugar(), func(malfeasance *pb.MalfeasanceStreamResponse) (bool, error) {
		ctx.Log.Desugar().Info("malfeasance proof received", zap.Any("malfeasance", malfeasance))
		require.Equal(t, malfeasance.Proof.SmesherId, signer.NodeID().String())
		return false, nil
	})
	require.NoError(t, err)
}
