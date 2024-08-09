package activation_test

import (
	"context"
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/atxwriter"
	ae2e "github.com/spacemeshos/go-spacemesh/activation/e2e"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/system"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func constructMerkleProof(t testing.TB, members []types.Hash32, ids map[uint64]bool) wire.MerkleProofV2 {
	t.Helper()

	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(ids).
		WithHashFunc(shared.HashMembershipTreeNode).
		Build()
	require.NoError(t, err)
	for _, member := range members {
		require.NoError(t, tree.AddLeaf(member[:]))
	}
	nodes := tree.Proof()
	nodesH32 := make([]types.Hash32, 0, len(nodes))
	for _, n := range nodes {
		nodesH32 = append(nodesH32, types.BytesToHash(n))
	}
	return wire.MerkleProofV2{Nodes: nodesH32}
}

type nipostData struct {
	previous types.ATXID
	*nipost.NIPostState
}

func buildNipost(
	ctx context.Context,
	nb *activation.NIPostBuilder,
	signer *signing.EdSigner,
	publish types.EpochID,
	previous, positioning types.ATXID,
) (nipostData, error) {
	postChallenge := &types.NIPostChallenge{
		PublishEpoch:   publish,
		PrevATXID:      previous,
		PositioningATX: positioning,
	}
	challenge := wire.NIPostChallengeToWireV2(postChallenge).Hash()
	nipost, err := nb.BuildNIPost(ctx, signer, challenge, postChallenge)
	nb.ResetState(signer.NodeID())
	return nipostData{previous, nipost}, err
}

func createInitialAtx(
	publish types.EpochID,
	commitment, pos types.ATXID,
	nipost *nipost.NIPostState,
	initial *types.Post,
) *wire.ActivationTxV2 {
	return &wire.ActivationTxV2{
		PublishEpoch:   publish,
		PositioningATX: pos,
		Initial: &wire.InitialAtxPartsV2{
			CommitmentATX: commitment,
			Post:          *wire.PostToWireV1(initial),
		},
		VRFNonce: uint64(nipost.VRFNonce),
		NiPosts: []wire.NiPostsV2{
			{
				Membership: wire.MerkleProofV2{
					Nodes: nipost.Membership.Nodes,
				},
				Challenge: types.Hash32(nipost.PostMetadata.Challenge),
				Posts: []wire.SubPostV2{
					{
						Post:                *wire.PostToWireV1(nipost.Post),
						NumUnits:            nipost.NumUnits,
						MembershipLeafIndex: nipost.Membership.LeafIndex,
					},
				},
			},
		},
	}
}

func createSoloAtx(publish types.EpochID, prev, pos types.ATXID, nipost *nipost.NIPostState) *wire.ActivationTxV2 {
	return &wire.ActivationTxV2{
		PublishEpoch:   publish,
		PreviousATXs:   []types.ATXID{prev},
		PositioningATX: pos,
		VRFNonce:       uint64(nipost.VRFNonce),
		NiPosts: []wire.NiPostsV2{
			{
				Membership: wire.MerkleProofV2{
					Nodes: nipost.Membership.Nodes,
				},
				Challenge: types.Hash32(nipost.PostMetadata.Challenge),
				Posts: []wire.SubPostV2{
					{
						Post:                *wire.PostToWireV1(nipost.Post),
						NumUnits:            nipost.NumUnits,
						MembershipLeafIndex: nipost.Membership.LeafIndex,
					},
				},
			},
		},
	}
}

func createMerged(
	t testing.TB,
	niposts []nipostData,
	publish types.EpochID,
	marriage, positioning types.ATXID,
	previous []types.ATXID,
	membership wire.MerkleProofV2,
) *wire.ActivationTxV2 {
	atx := &wire.ActivationTxV2{
		PublishEpoch:   publish,
		PreviousATXs:   previous,
		MarriageATX:    &marriage,
		PositioningATX: positioning,
		NiPosts: []wire.NiPostsV2{
			{
				Membership: membership,
				Challenge:  types.Hash32(niposts[0].PostMetadata.Challenge),
			},
		},
	}
	// Append PoSTs for all IDs
	for i, nipost := range niposts {
		idx := slices.IndexFunc(previous, func(a types.ATXID) bool { return a == nipost.previous })
		require.NotEqual(t, -1, idx)
		atx.NiPosts[0].Posts = append(atx.NiPosts[0].Posts, wire.SubPostV2{
			MarriageIndex:       uint32(i),
			PrevATXIndex:        uint32(idx),
			MembershipLeafIndex: nipost.Membership.LeafIndex,
			Post:                *wire.PostToWireV1(nipost.Post),
			NumUnits:            nipost.NumUnits,
		})
	}
	return atx
}

func signers(t testing.TB, keysHex []string) []*signing.EdSigner {
	t.Helper()

	signers := make([]*signing.EdSigner, 0, len(keysHex))
	for _, k := range keysHex {
		key, err := hex.DecodeString(k)
		require.NoError(t, err)

		sig, err := signing.NewEdSigner(signing.WithPrivateKey(key))
		require.NoError(t, err)
		signers = append(signers, sig)
	}
	return signers
}

var units = [2]uint32{2, 3}

// Keys were preselected to give IDs whose VRF nonces satisfy the combined storage requirement for the above `units`.
//
//nolint:lll
var singerKeys = [2]string{
	"1f2b77052ecc193038156d5c32f08d449742e7dda81fa172f8ac90839d34c76935a5d9365d1317c3002838126409e138321c57a5651d758485336c1e7e5af101",
	"6f385445a53d8af57874acd2dd98023858df7aa62f0b6e91ffdd51198036e2c331d2a7c55ba1e29312ac71dd419b4edc019b6406960cfc8ffb3d7550dde2ca1b",
}

func Test_MarryAndMerge(t *testing.T) {
	ctrl := gomock.NewController(t)
	signers := signers(t, singerKeys[:])

	var totalNumUnits uint32
	var nonces [2]uint64

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := testPostConfig()
	db := statesql.InMemoryTest(t)
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
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eg, ctx := errgroup.WithContext(ctx)
	for i, sig := range signers {
		opts := opts
		opts.DataDir = t.TempDir()
		opts.NumUnits = units[i]
		totalNumUnits += units[i]

		eg.Go(func() error {
			initPost(t, cfg, opts, sig, goldenATX, grpcCfg, svc)
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:  epoch,
		CycleGap:    3 * epoch / 4,
		GracePeriod: epoch / 4,
	}

	client := ae2e.NewTestPoetClient(2, poetCfg)
	poetSvc := activation.NewPoetServiceWithClient(poetDb, client, poetCfg, logger)

	clock, err := timesync.NewClock(
		timesync.WithGenesisTime(genesis),
		timesync.WithLayerDuration(layerDuration),
		timesync.WithTickInterval(10*time.Millisecond),
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

	writer := atxwriter.New(db, logger)
	go writer.Start(ctx)
	tickSize := uint64(3)
	atxHdlr := activation.NewHandler(
		"local",
		cdb,
		atxsdata.New(),
		writer,
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
		activation.WithTickSize(tickSize),
	)

	// Step 1. Marry
	publish := types.EpochID(1)
	var niposts [2]nipostData
	var initialPosts [2]*types.Post
	eg, _ = errgroup.WithContext(ctx)
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

	mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
	mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
	mFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{mergedIdAtx.ID()}, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ []types.ATXID, _ ...system.GetAtxOpt) error {
			// Provide the referenced ATX for the married ID
			mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
			mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
			mBeacon.EXPECT().OnAtx(gomock.Any())
			mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
			return atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(mergedIdAtx))
		})
	mBeacon.EXPECT().OnAtx(gomock.Any())
	mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(marriageATX))
	require.NoError(t, err)

	// Verify marriage
	for i, signer := range signers {
		marriage, err := identities.Marriage(db, signer.NodeID())
		require.NoError(t, err)
		require.NotNil(t, marriage)
		require.Equal(t, marriageATX.ID(), marriage.ATX)
		require.Equal(t, i, marriage.Index)
	}

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
	poetProof, members, err := poetSvc.Proof(context.Background(), "1")
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

	// Step 3. verify the merged ATX
	atx, err := atxs.Get(db, mergedATX.ID())
	require.NoError(t, err)
	require.Equal(t, totalNumUnits, atx.NumUnits)
	require.Equal(t, mainID.NodeID(), atx.SmesherID)
	require.Equal(t, poetProof.LeafCount/tickSize, atx.TickCount)
	require.Equal(t, uint64(totalNumUnits)*atx.TickCount, atx.Weight)

	posATX, err := atxs.Get(db, marriageATX.ID())
	require.NoError(t, err)
	require.Equal(t, posATX.TickHeight(), atx.BaseTickHeight)

	// Step 4. Publish merged using the same previous now
	// Publish by the other signer this time.
	eg, ctx = errgroup.WithContext(context.Background())
	publish = mergedATX.PublishEpoch + 1
	for i, sig := range signers {
		eg.Go(func() error {
			n, err := buildNipost(ctx, nb, sig, publish, mergedATX.ID(), mergedATX.ID())
			logger.Info("built NiPoST", zap.Any("post", n))
			niposts[i] = n
			return err
		})
	}
	require.NoError(t, eg.Wait())
	poetProof, members, err = poetSvc.Proof(context.Background(), "2")
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

	<-clock.AwaitLayer(mergedATX2.PublishEpoch.FirstLayer())
	logger.Info("publishing second merged ATX", zap.Inline(mergedATX2))
	mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
	mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
	mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	mBeacon.EXPECT().OnAtx(gomock.Any())
	mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(mergedATX2))
	require.NoError(t, err)

	atx, err = atxs.Get(db, mergedATX2.ID())
	require.NoError(t, err)
	require.Equal(t, totalNumUnits, atx.NumUnits)
	require.Equal(t, signers[1].NodeID(), atx.SmesherID)
	require.Equal(t, poetProof.LeafCount/tickSize, atx.TickCount)
	require.Equal(t, uint64(totalNumUnits)*atx.TickCount, atx.Weight)

	posATX, err = atxs.Get(db, mergedATX.ID())
	require.NoError(t, err)
	require.Equal(t, posATX.TickHeight(), atx.BaseTickHeight)

	// Step 5. Make an emergency split and publish separately
	publish = mergedATX2.PublishEpoch + 1
	eg, ctx = errgroup.WithContext(context.Background())
	for i, sig := range signers {
		eg.Go(func() error {
			n, err := buildNipost(ctx, nb, sig, publish, mergedATX2.ID(), mergedATX2.ID())
			logger.Info("built NiPoST", zap.Any("post", n))
			niposts[i] = n
			return err
		})
	}
	require.NoError(t, eg.Wait())

	<-clock.AwaitLayer(publish.FirstLayer())
	for i, signer := range signers {
		atx := createSoloAtx(publish, mergedATX2.ID(), mergedATX2.ID(), niposts[i].NIPostState)
		atx.Sign(signer)
		logger.Info("publishing split ATX", zap.Inline(atx))

		mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
		mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
		mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
		mBeacon.EXPECT().OnAtx(gomock.Any())
		mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
		err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx))
		require.NoError(t, err)

		atxFromDb, err := atxs.Get(db, atx.ID())
		require.NoError(t, err)
		require.Equal(t, units[i], atxFromDb.NumUnits)
		require.Equal(t, signer.NodeID(), atxFromDb.SmesherID)
		require.Equal(t, publish, atxFromDb.PublishEpoch)
		prev, err := atxs.Previous(db, atxFromDb.ID())
		require.NoError(t, err)
		require.Len(t, prev, 1)
		require.Equal(t, mergedATX2.ID(), prev[0])
	}
}
