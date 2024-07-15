package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func TestPostMalfeasanceProof(t *testing.T) {
	t.Parallel()
	testDir := t.TempDir()

	ctx := testcontext.New(t, testcontext.Labels("sanity"))
	logger := ctx.Log.Desugar().WithOptions(zap.IncreaseLevel(zap.InfoLevel), zap.WithCaller(false))

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

	cfg.POET.DefaultRequestTimeout = time.Minute
	cfg.POET.GetProofTimeout = time.Minute
	cfg.POET.SubmitChallengeTimeout = time.Minute
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
	ctx.Log.Debugw("Prepared config", "cfg", cfg)

	goldenATXID := cl.GoldenATX()
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
		datastore.NewCachedDB(sql.InMemory(), zap.NewNop()),
		atxsdata.New(),
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
	t.Cleanup(func() { assert.NoError(t, grpcPrivateServer.Close()) })

	db := sql.InMemory()
	localDb := localsql.InMemory()
	certClient := activation.NewCertifierClient(db, localDb, logger.Named("certifier"))
	certifier := activation.NewCertifier(localDb, logger, certClient)
	poetDb := activation.NewPoetDb(db, zap.NewNop())
	poetService, err := activation.NewPoetService(
		poetDb,
		types.PoetServer{
			Address: cluster.MakePoetGlobalEndpoint(ctx.Namespace, 0),
		}, cfg.POET,
		logger,
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
	var challenge *wire.NIPostChallengeV1
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

		err = nipost.AddPost(localDb, signer.NodeID(), nipost.Post{
			Nonce:         post.Nonce,
			Indices:       post.Indices,
			Pow:           post.Pow,
			Challenge:     shared.ZeroChallenge,
			NumUnits:      postInfo.NumUnits,
			CommitmentATX: postInfo.CommitmentATX,
			VRFNonce:      *postInfo.Nonce,
		})
		require.NoError(t, err)

		challenge = &wire.NIPostChallengeV1{
			PrevATXID:        types.EmptyATXID,
			PublishEpoch:     1,
			PositioningATXID: goldenATXID,
			CommitmentATXID:  &postInfo.CommitmentATX,
			InitialPost: &wire.PostV1{
				Nonce:   post.Nonce,
				Indices: post.Indices,
				Pow:     post.Pow,
			},
		}
		break
	}
	nipostChallenge := &types.NIPostChallenge{
		PublishEpoch:   challenge.PublishEpoch,
		PrevATXID:      types.EmptyATXID,
		PositioningATX: challenge.PositioningATXID,
		CommitmentATX:  challenge.CommitmentATXID,
		InitialPost: &types.Post{
			Nonce:   challenge.InitialPost.Nonce,
			Indices: challenge.InitialPost.Indices,
			Pow:     challenge.InitialPost.Pow,
		},
	}

	nipost, err := nipostBuilder.BuildNIPost(ctx, signer, challenge.Hash(), nipostChallenge)
	require.NoError(t, err)

	// 2.2 Create ATX with invalid POST
	for i := range nipost.Post.Indices {
		nipost.Post.Indices[i] += 1
	}

	// Sanity check that the POST is invalid
	err = verifier.Verify(ctx, (*shared.Proof)(nipost.Post), &shared.ProofMetadata{
		NodeId:          signer.NodeID().Bytes(),
		CommitmentAtxId: challenge.CommitmentATXID.Bytes(),
		NumUnits:        nipost.NumUnits,
		Challenge:       nipost.PostMetadata.Challenge,
		LabelsPerUnit:   nipost.PostMetadata.LabelsPerUnit,
	})
	var invalidIdxError *verifying.ErrInvalidIndex
	require.ErrorAs(t, err, &invalidIdxError)

	nodeID := signer.NodeID()
	atx := wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: *challenge,
			Coinbase:          types.Address{1, 2, 3, 4},
			NumUnits:          nipost.NumUnits,
			NIPost:            wire.NiPostToWireV1(nipost.NIPost),
			NodeID:            &nodeID,
			VRFNonce:          (*uint64)(&nipost.VRFNonce),
		},
	}
	atx.Sign(signer)

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
			buf := codec.MustEncode(&atx)
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
		require.Equal(t, atx.PublishEpoch, invalidAtx.PublishEpoch)
		require.Equal(t, atx.SmesherID, invalidAtx.SmesherID)
		require.Equal(t, atx.ID(), invalidAtx.ID())

		meta := &shared.ProofMetadata{
			NodeId:          invalidAtx.NodeID.Bytes(),
			CommitmentAtxId: invalidAtx.CommitmentATXID.Bytes(),
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
