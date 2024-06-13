package activation_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/registration"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func constructMerkleProof(t *testing.T, members []types.Hash32, ids map[uint64]bool) wire.MerkleProofV2 {
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
	nb *activation.NIPostBuilder,
	sig *signing.EdSigner,
	publish types.EpochID,
	previous, positioning types.ATXID,
) (nipostData, error) {
	challenge := wire.NIPostChallengeV2{
		PublishEpoch:     publish,
		PrevATXID:        previous,
		PositioningATXID: positioning,
	}
	nipost, err := nb.BuildNIPost(context.Background(), sig, challenge.PublishEpoch, challenge.Hash())
	nb.ResetState(sig.NodeID())
	return nipostData{previous, nipost}, err
}

func createMerged(
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
		if idx == -1 {
			panic(fmt.Sprintf("previous ATX %s not found in %s", nipost.previous, previous))
		}
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

func signers(t *testing.T, keysHex []string) []*signing.EdSigner {
	t.Helper()

	var keys [][]byte
	for _, k := range keysHex {
		key, err := hex.DecodeString(k)
		require.NoError(t, err)
		keys = append(keys, key)
	}

	signers := []*signing.EdSigner{}
	for _, key := range keys {
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
	t.Parallel()
	ctrl := gomock.NewController(t)
	signers := signers(t, singerKeys[:])

	var totalNumUnits uint32
	var nonces [2]uint64

	logger := zaptest.NewLogger(t)
	goldenATX := types.ATXID{2, 3, 4}
	cfg := activation.DefaultPostConfig()
	db := sql.InMemory()
	cdb := datastore.NewCachedDB(db, logger)
	localDB := localsql.InMemory()

	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		synced := make(chan struct{})
		close(synced)
		return synced
	}).AnyTimes()

	svc := grpcserver.NewPostService(logger)
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

	var eg errgroup.Group
	for i, sig := range signers {
		opts := opts
		opts.DataDir = t.TempDir()
		opts.NumUnits = units[i]
		totalNumUnits += units[i]

		eg.Go(func() error {
			mgr, err := activation.NewPostSetupManager(cfg, logger, db, atxsdata.New(), goldenATX, syncer, validator)
			require.NoError(t, err)

			t.Cleanup(launchPostSupervisor(t, zap.NewNop(), mgr, sig, grpcCfg, opts))

			require.Eventually(t, func() bool {
				_, err := svc.Client(sig.NodeID())
				return err == nil
			}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	// ensure that genesis aligns with layer timings
	genesis := time.Now().Add(layerDuration).Round(layerDuration)
	layerDuration := 2 * time.Second
	epoch := layersPerEpoch * layerDuration
	poetCfg := activation.PoetConfig{
		PhaseShift:        epoch,
		CycleGap:          epoch / 2,
		GracePeriod:       epoch / 5,
		RequestTimeout:    epoch / 5,
		RequestRetryDelay: epoch / 50,
		MaxRequestRetries: 10,
	}

	pubkey, address := spawnTestCertifier(t, cfg, nil, verifying.WithLabelScryptParams(opts.Scrypt))
	certClient := activation.NewCertifierClient(db, localDB, logger.Named("certifier"))
	certifier := activation.NewCertifier(localDB, logger, certClient)
	poet := spawnPoet(
		t,
		WithGenesis(genesis),
		WithEpochDuration(epoch),
		WithPhaseShift(poetCfg.PhaseShift),
		WithCycleGap(poetCfg.CycleGap),
		WithCertifier(&registration.CertifierConfig{
			URL:    (&url.URL{Scheme: "http", Host: address.String()}).String(),
			PubKey: registration.Base64Enc(pubkey),
		}),
	)
	poetClient, err := poet.Client(poetDb, poetCfg, logger, activation.WithCertifier(certifier))
	require.NoError(t, err)

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
		activation.WithPoetClients(poetClient),
	)
	require.NoError(t, err)

	conf := activation.Config{
		GoldenATXID:      goldenATX,
		RegossipInterval: 0,
	}

	data := atxsdata.New()
	atxVersions := activation.AtxVersions{postGenesisEpoch: types.AtxV2}
	edVerifier := signing.NewEdVerifier()
	mpub := mocks.NewMockPublisher(ctrl)
	mFetch := smocks.NewMockFetcher(ctrl)
	mBeacon := activation.NewMockAtxReceiver(ctrl)
	mTortoise := smocks.NewMockTortoise(ctrl)

	tickSize := uint64(3)
	atxHdlr := activation.NewHandler(
		"local",
		cdb,
		data,
		edVerifier,
		clock,
		mpub,
		mFetch,
		goldenATX,
		validator,
		mBeacon,
		mTortoise,
		logger,
		activation.WithAtxVersions(atxVersions),
		activation.WithTickSize(tickSize),
	)

	mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(ctx context.Context, p string, got []byte) error {
			mFetch.EXPECT().RegisterPeerHashes(peer.ID(p), gomock.Any())
			mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
			mBeacon.EXPECT().OnAtx(gomock.Any())
			mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
			return atxHdlr.HandleGossipAtx(ctx, peer.ID(p), got)
		},
	).Times(2)

	v := activation.NewValidator(db, poetDb, cfg, opts.Scrypt, verifier)
	builder := activation.NewBuilder(
		conf,
		db,
		data,
		localDB,
		mpub,
		nb,
		clock,
		syncer,
		logger,
		activation.WithPoetConfig(poetCfg),
		activation.WithValidator(v),
	)

	// Step 1. Publish initial ATXs for each signer
	eg = errgroup.Group{}
	for i, signer := range signers {
		eg.Go(func() error {
			post, postInfo, err := nb.Proof(context.Background(), signer.NodeID(), types.EmptyHash32[:])
			if err != nil {
				return err
			}
			initialPost := nipost.Post{
				Nonce:         post.Nonce,
				Indices:       post.Indices,
				Pow:           post.Pow,
				Challenge:     types.EmptyHash32[:],
				NumUnits:      postInfo.NumUnits,
				CommitmentATX: postInfo.CommitmentATX,
				VRFNonce:      *postInfo.Nonce,
			}
			// make sure that the vrf nonce is good enough for the combined storage
			err = v.VRFNonceV2(signer.NodeID(), postInfo.CommitmentATX, uint64(*postInfo.Nonce), totalNumUnits)
			if err != nil {
				return err
			}
			nonces[i] = uint64(*postInfo.Nonce)
			if err := nipost.AddPost(localDB, signer.NodeID(), initialPost); err != nil {
				return err
			}
			return builder.PublishActivationTx(context.Background(), signer)
		})
	}
	require.NoError(t, eg.Wait())

	// Step 2. Marry
	mainID, mergedID := signers[0], signers[1]

	prevMergedIDATX, err := atxs.GetLastIDByNodeID(db, mergedID.NodeID())
	require.NoError(t, err)
	prevATXID, err := atxs.GetLastIDByNodeID(db, mainID.NodeID())
	require.NoError(t, err)
	prev, err := atxs.Get(db, prevATXID)
	require.NoError(t, err)

	challenge := wire.NIPostChallengeV2{
		PublishEpoch:     prev.PublishEpoch + 1,
		PrevATXID:        prevATXID,
		PositioningATXID: prevATXID,
	}

	nipostState, err := nb.BuildNIPost(context.Background(), mainID, challenge.PublishEpoch, challenge.Hash())
	require.NoError(t, err)
	require.NoError(t, nb.ResetState(mainID.NodeID()))

	marriageATX := &wire.ActivationTxV2{
		PublishEpoch:   challenge.PublishEpoch,
		PositioningATX: challenge.PositioningATXID,
		PreviousATXs:   []types.ATXID{challenge.PrevATXID},
		Coinbase:       builder.Coinbase(),
		VRFNonce:       (uint64)(nipostState.VRFNonce),
		NiPosts: []wire.NiPostsV2{
			{
				Membership: wire.MerkleProofV2{
					Nodes: nipostState.Membership.Nodes,
				},
				Challenge: types.Hash32(nipostState.NIPost.PostMetadata.Challenge),
				Posts: []wire.SubPostV2{
					{
						Post:                *wire.PostToWireV1(nipostState.Post),
						NumUnits:            nipostState.NumUnits,
						MembershipLeafIndex: nipostState.Membership.LeafIndex,
					},
				},
			},
		},
		Marriages: []wire.MarriageCertificate{
			{
				ReferenceAtx: prevMergedIDATX,
				Signature:    mergedID.Sign(signing.MARRIAGE, mainID.NodeID().Bytes()),
			},
		},
	}
	marriageATX.Sign(mainID)
	logger.Info("publishing marriage ATX", zap.Inline(marriageATX))

	mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
	mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
	mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	mBeacon.EXPECT().OnAtx(gomock.Any())
	mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(marriageATX))
	require.NoError(t, err)

	// Verify marriage
	for i, signer := range signers {
		marriage, idx, err := identities.MarriageInfo(db, signer.NodeID())
		require.NoError(t, err)
		require.NotNil(t, marriage)
		require.Equal(t, marriageATX.ID(), *marriage)
		require.Equal(t, i, idx)
	}

	// Step 3. Publish merged ATX together
	publish := marriageATX.PublishEpoch + 2
	eg = errgroup.Group{}

	var niposts [2]nipostData
	// 3.1. NiPOST for main ID (the publisher)
	eg.Go(func() error {
		n, err := buildNipost(nb, mainID, publish, marriageATX.ID(), marriageATX.ID())
		logger.Info("built NiPoST", zap.Any("post", niposts[0]))
		niposts[0] = n
		return err
	})

	// 3.2. NiPOST for merged ID
	prevATXID, err = atxs.GetLastIDByNodeID(db, mergedID.NodeID())
	require.NoError(t, err)
	eg.Go(func() error {
		n, err := buildNipost(nb, mergedID, publish, prevATXID, marriageATX.ID())
		logger.Info("built NiPoST", zap.Any("post", n))
		niposts[1] = n
		return err
	})
	require.NoError(t, eg.Wait())

	// 3.3 Construct a multi-ID poet membership merkle proof for both IDs
	poetProof, members, err := poetClient.Proof(context.Background(), "2")
	require.NoError(t, err)
	membershipProof := constructMerkleProof(t, members, map[uint64]bool{0: true, 1: true})

	mergedATX := createMerged(
		niposts[:],
		publish,
		marriageATX.ID(),
		marriageATX.ID(),
		[]types.ATXID{marriageATX.ID(), prevATXID},
		membershipProof,
	)
	mergedATX.Coinbase = builder.Coinbase()
	mergedATX.VRFNonce = nonces[0]
	mergedATX.Sign(mainID)

	// 3.5 Publish
	logger.Info("publishing merged ATX", zap.Inline(mergedATX))

	mFetch.EXPECT().RegisterPeerHashes(peer.ID(""), gomock.Any())
	mFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
	mFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	mBeacon.EXPECT().OnAtx(gomock.Any())
	mTortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(mergedATX))
	require.NoError(t, err)

	// Step 4. verify the merged ATX
	atx, err := atxs.Get(db, mergedATX.ID())
	require.NoError(t, err)
	require.Equal(t, totalNumUnits, atx.NumUnits)
	require.Equal(t, mainID.NodeID(), atx.SmesherID)
	require.Equal(t, poetProof.LeafCount/tickSize, atx.TickCount)

	posATX, err := atxs.Get(db, marriageATX.ID())
	require.NoError(t, err)
	require.Equal(t, posATX.TickHeight(), atx.BaseTickHeight)

	// Step 5. Publish merged using the same previous now
	// Publish by the other signer this time.
	publish = mergedATX.PublishEpoch + 1
	eg = errgroup.Group{}
	for i, sig := range signers {
		eg.Go(func() error {
			n, err := buildNipost(nb, sig, publish, mergedATX.ID(), mergedATX.ID())
			logger.Info("built NiPoST", zap.Any("post", n))
			niposts[i] = n
			return err
		})
	}
	require.NoError(t, eg.Wait())
	poetProof, members, err = poetClient.Proof(context.Background(), "3")
	require.NoError(t, err)
	membershipProof = constructMerkleProof(t, members, map[uint64]bool{0: true})

	mergedATX2 := createMerged(
		niposts[:],
		publish,
		marriageATX.ID(),
		mergedATX.ID(),
		[]types.ATXID{mergedATX.ID()},
		membershipProof,
	)
	mergedATX2.Coinbase = builder.Coinbase()
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

	atx, err = atxs.Get(db, mergedATX2.ID())
	require.NoError(t, err)
	require.Equal(t, totalNumUnits, atx.NumUnits)
	require.Equal(t, signers[1].NodeID(), atx.SmesherID)
	require.Equal(t, poetProof.LeafCount/tickSize, atx.TickCount)

	posATX, err = atxs.Get(db, mergedATX.ID())
	require.NoError(t, err)
	require.Equal(t, posATX.TickHeight(), atx.BaseTickHeight)
}
