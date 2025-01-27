package tests

import (
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func createInitialAtxV1(t testing.TB,
	ctx *testcontext.Context,
	logger *zap.Logger,
	cl *cluster.Cluster,
	cfg *config.Config,
	signer *signing.EdSigner,
	db sql.StateDatabase,
	localDb sql.LocalDatabase,
	clock *timesync.NodeClock,
	verifier activation.PostVerifier,
) wire.ActivationTxV1 {
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
	t.Cleanup(func() { assert.NoError(t, grpcPrivateServer.Close()) })

	// 2.1. Create initial POST
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
			PositioningATXID: cl.GoldenATX(),
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
	err = nipost.AddChallenge(localDb, signer.NodeID(), nipostChallenge)
	require.NoError(t, err)

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
	return atx
}
