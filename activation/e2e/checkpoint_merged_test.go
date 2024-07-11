package activation_test

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	ae2e "github.com/spacemeshos/go-spacemesh/activation/e2e"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func Test_CheckpointAfterMerge(t *testing.T) {
	ctrl := gomock.NewController(t)
	signers := signers(t, singerKeys[:])

	var nonces [2]uint64

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := testPostConfig()
	db := statesql.InMemory()
	cdb := datastore.NewCachedDB(db, logger)
	localDB := localsql.InMemory()

	svc := grpcserver.NewPostService(logger, grpcserver.PostServiceQueryInterval(100*time.Millisecond))
	svc.AllowConnections(true)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := testPostSetupOpts(t)
	verifyingOpts := activation.DefaultTestPostVerifyingOpts()
	verifier, err := activation.NewPostVerifier(cfg, logger, activation.WithVerifyingOpts(verifyingOpts))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, verifier.Close()) })
	poetDb := activation.NewPoetDb(db, logger.Named("poetDb"))
	validator := activation.NewValidator(db, poetDb, cfg, opts.Scrypt, verifier)

	eg, ctx := errgroup.WithContext(context.Background())
	for _, sig := range signers {
		opts := opts
		opts.DataDir = t.TempDir()

		eg.Go(func() error {
			initPost(t, cfg, opts, sig, goldenATX, grpcCfg, svc)
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Round(layerDuration)
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:  epoch,
		CycleGap:    3 * epoch / 4,
		GracePeriod: epoch / 4,
	}

	client := ae2e.NewTestPoetClient(2)
	poetSvc := activation.NewPoetServiceWithClient(poetDb, client, poetCfg, logger)

	clock, err := timesync.NewClock(
		timesync.WithGenesisTime(genesis),
		timesync.WithLayerDuration(layerDuration),
		timesync.WithTickInterval(100*time.Millisecond),
		timesync.WithLogger(zap.NewNop()),
	)
	require.NoError(t, err)
	t.Cleanup(clock.Close)

	nb, err := activation.NewNIPostBuilder(
		localDB,
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		clock,
		validator,
		activation.WithPoetServices(poetSvc),
	)
	require.NoError(t, err)

	mpub := mocks.NewMockPublisher(ctrl)
	mFetch := smocks.NewMockFetcher(ctrl)
	mBeacon := activation.NewMockAtxReceiver(ctrl)
	mTortoise := smocks.NewMockTortoise(ctrl)

	atxHdlr := activation.NewHandler(
		"local",
		cdb,
		atxsdata.New(),
		signing.NewEdVerifier(),
		clock,
		mpub,
		mFetch,
		goldenATX,
		validator,
		mBeacon,
		mTortoise,
		logger,
		activation.WithAtxVersions(activation.AtxVersions{0: types.AtxV2}),
	)

	// Step 1. Marry
	publish := types.EpochID(1)
	var niposts [2]nipostData
	var initialPosts [2]*types.Post
	eg, ctx = errgroup.WithContext(context.Background())
	for i, signer := range signers {
		eg.Go(func() error {
			post, postInfo, err := nb.Proof(context.Background(), signer.NodeID(), types.EmptyHash32[:], nil)
			if err != nil {
				return err
			}

			postChallenge := &types.NIPostChallenge{
				PublishEpoch:   publish,
				PositioningATX: goldenATX,
				InitialPost:    post,
			}
			challenge := wire.NIPostChallengeToWireV2(postChallenge).Hash()
			nipost, err := nb.BuildNIPost(context.Background(), signer, challenge, postChallenge)
			if err != nil {
				return err
			}
			nb.ResetState(signer.NodeID())

			initialPosts[i] = post
			nonces[i] = uint64(*postInfo.Nonce)
			niposts[i] = nipostData{types.EmptyATXID, nipost}
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	// mainID will create marriage ATX
	mainID, mergedID := signers[0], signers[1]

	mergedIdAtx := createInitialAtx(publish, goldenATX, goldenATX, niposts[1].NIPostState, initialPosts[1])
	mergedIdAtx.Sign(mergedID)

	marriageATX := createInitialAtx(publish, goldenATX, goldenATX, niposts[0].NIPostState, initialPosts[0])
	marriageATX.Marriages = []wire.MarriageCertificate{
		{
			Signature: mainID.Sign(signing.MARRIAGE, mainID.NodeID().Bytes()),
		},
		{
			ReferenceAtx: mergedIdAtx.ID(),
			Signature:    mergedID.Sign(signing.MARRIAGE, mainID.NodeID().Bytes()),
		},
	}
	marriageATX.Sign(mainID)
	logger.Info("publishing marriage ATX", zap.Inline(marriageATX))

	mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any()).Times(2)
	mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any()).Times(2)
	mFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{mergedIdAtx.ID()}, gomock.Any())
	mBeacon.EXPECT().OnAtx(gomock.Any()).Times(2)
	mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any()).Times(2)
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(mergedIdAtx))
	require.NoError(t, err)
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(marriageATX))
	require.NoError(t, err)

	// Step 2. Publish merged ATX together
	publish = marriageATX.PublishEpoch + 2
	eg, ctx = errgroup.WithContext(context.Background())
	// 2.1. NiPOST for main ID (the publisher)
	eg.Go(func() error {
		n, err := buildNipost(ctx, nb, mainID, publish, marriageATX.ID(), marriageATX.ID())
		logger.Info("built NiPoST", zap.Any("post", n))
		niposts[0] = n
		return err
	})

	// 2.2. NiPOST for merged ID
	prevATXID, err := atxs.GetLastIDByNodeID(db, mergedID.NodeID())
	require.NoError(t, err)
	eg.Go(func() error {
		n, err := buildNipost(ctx, nb, mergedID, publish, prevATXID, marriageATX.ID())
		logger.Info("built NiPoST", zap.Any("post", n))
		niposts[1] = n
		return err
	})
	require.NoError(t, eg.Wait())

	// 2.3 Construct a multi-ID poet membership merkle proof for both IDs
	_, members, err := poetSvc.Proof(context.Background(), "1")
	require.NoError(t, err)
	membershipProof := constructMerkleProof(t, members, map[uint64]bool{0: true, 1: true})

	mergedATX := createMerged(
		t,
		niposts[:],
		publish,
		marriageATX.ID(),
		marriageATX.ID(),
		[]types.ATXID{marriageATX.ID(), prevATXID},
		membershipProof,
	)
	mergedATX.VRFNonce = nonces[0]
	mergedATX.Sign(mainID)

	// 2.4 Publish
	<-clock.AwaitLayer(mergedATX.PublishEpoch.FirstLayer())
	logger.Info("publishing merged ATX", zap.Inline(mergedATX))

	mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
	mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
	mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	mBeacon.EXPECT().OnAtx(gomock.Any())
	mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(mergedATX))
	require.NoError(t, err)

	// Step 3. Checkpoint
	// 3.1. Generate checkpoint
	require.NoError(t, accounts.Update(db, &types.Account{}))
	snapshot := clock.CurrentLayer()
	fs := afero.NewMemMapFs()
	dir, err := afero.TempDir(fs, "", "Generate")
	require.NoError(t, err)
	err = checkpoint.Generate(context.Background(), fs, db, dir, snapshot, 1)
	require.NoError(t, err)

	// 3.2. Recover from checkpoint
	recoveryCfg := checkpoint.RecoverConfig{
		GoldenAtx: goldenATX,
		DataDir:   t.TempDir(),
		NodeIDs:   []types.NodeID{mainID.NodeID(), mergedID.NodeID()},
		DbFile:    "db.sql",
		Restore:   snapshot,
	}
	filename := checkpoint.SelfCheckpointFilename(dir, snapshot)

	data, err := checkpoint.RecoverFromLocalFile(context.Background(), logger, db, localDB, fs, &recoveryCfg, filename)
	require.NoError(t, err)
	require.Nil(t, data)

	newDB, err := statesql.Open("file:" + recoveryCfg.DbPath())
	require.NoError(t, err)
	defer newDB.Close()

	// 3.3 Verify IDs are still married
	for i, signer := range signers {
		marriage, err := identities.Marriage(newDB, signer.NodeID())
		require.NoError(t, err)
		require.Equal(t, marriageATX.ID(), marriage.ATX)
		require.Equal(t, i, marriage.Index)
	}

	// 4. Spawn new ATX handler and builder using the new DB
	poetDb = activation.NewPoetDb(newDB, logger.Named("poetDb"))
	cdb = datastore.NewCachedDB(newDB, logger)

	poetSvc = activation.NewPoetServiceWithClient(poetDb, client, poetCfg, logger)
	validator = activation.NewValidator(newDB, poetDb, cfg, opts.Scrypt, verifier)
	require.NoError(t, err)
	atxHdlr = activation.NewHandler(
		"local",
		cdb,
		atxsdata.New(),
		signing.NewEdVerifier(),
		clock,
		mpub,
		mFetch,
		goldenATX,
		validator,
		mBeacon,
		mTortoise,
		logger,
		activation.WithAtxVersions(activation.AtxVersions{0: types.AtxV2}),
	)

	nb, err = activation.NewNIPostBuilder(
		localDB,
		svc,
		logger.Named("nipostBuilder"),
		poetCfg,
		clock,
		validator,
		activation.WithPoetServices(poetSvc),
	)
	require.NoError(t, err)

	// Step 4. Publish merged using the same previous now
	// Publish by the other signer this time.
	publish = mergedATX.PublishEpoch + 1
	eg, ctx = errgroup.WithContext(context.Background())
	for i, sig := range signers {
		eg.Go(func() error {
			n, err := buildNipost(ctx, nb, sig, publish, mergedATX.ID(), mergedATX.ID())
			logger.Info("built NiPoST", zap.Any("post", n))
			niposts[i] = n
			return err
		})
	}
	require.NoError(t, eg.Wait())
	_, members, err = poetSvc.Proof(context.Background(), "2")
	require.NoError(t, err)
	membershipProof = constructMerkleProof(t, members, map[uint64]bool{0: true})

	mergedATX2 := createMerged(
		t,
		niposts[:],
		publish,
		marriageATX.ID(),
		mergedATX.ID(),
		[]types.ATXID{mergedATX.ID()},
		membershipProof,
	)
	mergedATX2.VRFNonce = nonces[1]
	mergedATX2.Sign(signers[1])

	logger.Info("publishing second merged ATX", zap.Inline(mergedATX2))
	mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
	mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
	mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	mBeacon.EXPECT().OnAtx(gomock.Any())
	mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(mergedATX2))
	require.NoError(t, err)
}
