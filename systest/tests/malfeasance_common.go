package tests

import (
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"

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

type builtAtx interface {
	ID() types.ATXID

	scale.Encodable
	zapcore.ObjectMarshaler
}

func createInitialAtx(t testing.TB,
	ctx *testcontext.Context,
	logger *zap.Logger,
	cl *cluster.Cluster,
	cfg *config.Config,
	signer *signing.EdSigner,
	db sql.StateDatabase,
	localDb sql.LocalDatabase,
	clock *timesync.NodeClock,
	verifier activation.PostVerifier,
	publishEpoch types.EpochID,
) (builtAtx, types.EpochID) {
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

	var client activation.PostClient
	for {
		client, err = grpcPostService.Client(signer.NodeID())
		if err == nil {
			break
		}
		ctx.Log.Info("waiting for poet service to connect")
		time.Sleep(time.Second)
	}
	ctx.Log.Info("poet service to connected")
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
	ctx.Log.Desugar().Info("waiting for epoch to register at poet",
		zap.Uint32("register_epoch", uint32(registerEpoch)),
		zap.Uint32("publish_epoch", uint32(publishEpoch)),
	)
	select {
	case <-ctx.Done():
		ctx.Log.Info("context canceled")
		return nil, 0
	case <-clock.AwaitLayer(registerEpoch.FirstLayer()):
	}
	ctx.Log.Desugar().Info("reached register epoch", zap.Uint32("register_epoch", uint32(registerEpoch)))

	registerEpoch = clock.CurrentLayer().GetEpoch()
	publishEpoch = registerEpoch + 1
	nipostChallenge := &types.NIPostChallenge{
		PublishEpoch:   publishEpoch,
		PrevATXID:      types.EmptyATXID,
		PositioningATX: cl.GoldenATX(),
		CommitmentATX:  &initialPostInfo.CommitmentATX,
		InitialPost: &types.Post{
			Nonce:   initialPost.Nonce,
			Indices: initialPost.Indices,
			Pow:     initialPost.Pow,
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
	for i := range nipost.Post.Indices {
		nipost.Post.Indices[i] += 1
	}

	// Sanity check that the POST is invalid
	err = verifier.Verify(ctx, (*shared.Proof)(nipost.Post), &shared.ProofMetadata{
		NodeId:          signer.NodeID().Bytes(),
		CommitmentAtxId: nipostChallenge.CommitmentATX.Bytes(),
		NumUnits:        nipost.NumUnits,
		Challenge:       nipost.PostMetadata.Challenge,
		LabelsPerUnit:   nipost.PostMetadata.LabelsPerUnit,
	})
	var invalidIdxError *verifying.ErrInvalidIndex
	require.ErrorAs(t, err, &invalidIdxError)

	switch version {
	case types.AtxV1:
		atx := &wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: *wire.NIPostChallengeToWireV1(nipostChallenge),
				Coinbase:          types.Address{1, 2, 3, 4},
				NumUnits:          nipost.NumUnits,
				NIPost:            wire.NiPostToWireV1(nipost.NIPost),
				VRFNonce:          (*uint64)(&nipost.VRFNonce),
			},
		}
		atx.Sign(signer)
		return atx, atx.PublishEpoch
	case types.AtxV2:
		atx := &wire.ActivationTxV2{
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
		atx.Sign(signer)
		return atx, atx.PublishEpoch
	default:
		require.Fail(t, fmt.Sprintf("unsupported ATX version: %v", version))
		return nil, 0
	}
}

func version(cfg *config.Config, publish types.EpochID) types.AtxVersion {
	epochs := append([]types.EpochID{0}, maps.Keys(cfg.AtxVersions)...)
	version := types.AtxV1
	for _, epoch := range epochs {
		if publish >= epoch {
			version = cfg.AtxVersions[epoch]
		}
	}
	return version
}
