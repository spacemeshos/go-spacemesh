package proposals

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmock "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	layersPerEpoch = uint32(3)
	layerAvgSize   = uint32(10)
)

func genGoldenATXID() types.ATXID {
	return types.ATXID(types.HexToHash32("0x6666666"))
}

type mockSet struct {
	mpub   *pubsubmock.MockPublisher
	mf     *mocks.MockFetcher
	mbc    *mocks.MockBeaconCollector
	mclock *MocklayerClock
	mm     *MockmeshProvider
	mv     *MockeligibilityValidator
	md     *MocktortoiseProvider
	mvrf   *MockvrfVerifier
}

func (ms *mockSet) decodeAnyBallots() *mockSet {
	ms.md.EXPECT().DecodeBallot(gomock.Any()).AnyTimes()
	ms.md.EXPECT().StoreBallot(gomock.Any()).AnyTimes()
	return ms
}

func (ms *mockSet) setCurrentLayer(layer types.LayerID) *mockSet {
	ms.mclock.EXPECT().CurrentLayer().Return(layer).AnyTimes()
	ms.mm.EXPECT().ProcessedLayer().Return(layer - 1).AnyTimes()
	ms.mclock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now().Add(-5 * time.Second)).AnyTimes()
	return ms
}

type testHandler struct {
	*Handler
	*mockSet
}

func fullMockSet(tb testing.TB) *mockSet {
	ctrl := gomock.NewController(tb)
	return &mockSet{
		mpub:   pubsubmock.NewMockPublisher(ctrl),
		mf:     mocks.NewMockFetcher(ctrl),
		mbc:    mocks.NewMockBeaconCollector(ctrl),
		mclock: NewMocklayerClock(ctrl),
		mm:     NewMockmeshProvider(ctrl),
		mv:     NewMockeligibilityValidator(ctrl),
		md:     NewMocktortoiseProvider(ctrl),
		mvrf:   NewMockvrfVerifier(ctrl),
	}
}

func createTestHandler(t *testing.T) *testHandler {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(t)
	db := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	ms.md.EXPECT().GetBallot(gomock.Any()).AnyTimes().DoAndReturn(func(id types.BallotID) *tortoise.BallotData {
		ballot, err := ballots.Get(db, id)
		if err != nil {
			return nil
		}

		data := &tortoise.BallotData{
			ID:      ballot.ID(),
			Layer:   ballot.Layer,
			ATXID:   ballot.AtxID,
			Smesher: ballot.SmesherID,
		}
		if ballot.EpochData != nil {
			data.Beacon = ballot.EpochData.Beacon
			data.Eligiblities = ballot.EpochData.EligibilityCount
		}
		return data
	})
	return &testHandler{
		Handler: NewHandler(db, signing.NewEdVerifier(), ms.mpub, ms.mf, ms.mbc, ms.mm, ms.md, ms.mvrf, ms.mclock,
			WithLogger(logtest.New(t)),
			WithConfig(Config{
				LayerSize:      layerAvgSize,
				LayersPerEpoch: layersPerEpoch,
				GoldenATXID:    genGoldenATXID(),
				MaxExceptions:  3,
				Hdist:          5,
			}),
			withValidator(ms.mv),
		),
		mockSet: ms,
	}
}

func createTestHandlerNoopDecoder(t *testing.T) *testHandler {
	th := createTestHandler(t)
	th.mockSet.decodeAnyBallots()
	th.mockSet.setCurrentLayer(100*types.LayerID(types.GetLayersPerEpoch()) + 1)
	return th
}

type createBallotOpt func(b *types.Ballot)

func withSupportBlocks(blocks ...*types.Block) createBallotOpt {
	return func(b *types.Ballot) {
		b.Votes.Support = nil
		for _, block := range blocks {
			b.Votes.Support = append(b.Votes.Support, block.ToVote())
		}
	}
}

func withAgainstBlocks(blocks ...*types.Block) createBallotOpt {
	return func(b *types.Ballot) {
		b.Votes.Against = nil
		for _, block := range blocks {
			b.Votes.Against = append(b.Votes.Against, block.ToVote())
		}
	}
}

func withAbstain(layers ...types.LayerID) createBallotOpt {
	return func(b *types.Ballot) {
		b.Votes.Abstain = layers
	}
}

func withLayer(lid types.LayerID) createBallotOpt {
	return func(b *types.Ballot) {
		b.Layer = lid
	}
}

func withRefData(activeSet types.ATXIDList) createBallotOpt {
	return func(b *types.Ballot) {
		b.RefBallot = types.EmptyBallotID
		sort.Slice(activeSet, func(i, j int) bool {
			return bytes.Compare(activeSet[i].Bytes(), activeSet[j].Bytes()) < 0
		})
		b.EpochData = &types.EpochData{
			ActiveSetHash: activeSet.Hash(),
			Beacon:        types.RandomBeacon(),
		}
	}
}

type createProposalOpt func(p *types.Proposal)

func withTransactions(ids ...types.TransactionID) createProposalOpt {
	return func(p *types.Proposal) {
		p.TxIDs = ids
	}
}

func withProposalLayer(layer types.LayerID) createProposalOpt {
	return func(p *types.Proposal) {
		p.Layer = layer
		p.Ballot.Layer = layer
	}
}

func createProposal(t *testing.T, opts ...any) *types.Proposal {
	t.Helper()
	b := types.RandomBallot()
	b.Layer = 10000
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *b,
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	for _, opt := range opts {
		switch unwrap := opt.(type) {
		case createBallotOpt:
			unwrap(&p.Ballot)
		case createProposalOpt:
			unwrap(p)
		}
	}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Ballot.SmesherID = signer.NodeID()
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	p.SmesherID = signer.NodeID()
	require.NoError(t, p.Initialize())
	return p
}

func encodeProposal(t *testing.T, p *types.Proposal) []byte {
	t.Helper()
	data, err := codec.Encode(p)
	require.NoError(t, err)
	return data
}

func createAtx(t *testing.T, db *sql.Database, epoch types.EpochID, atxID types.ATXID, nodeID types.NodeID) {
	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PublishEpoch: epoch,
		},
		NumUnits: 1,
	}}
	atx.SetID(atxID)
	atx.SetEffectiveNumUnits(1)
	atx.SetReceived(time.Now())
	atx.SmesherID = nodeID
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, vAtx))
}

func createBallot(t *testing.T, opts ...createBallotOpt) *types.Ballot {
	t.Helper()
	b := types.RandomBallot()
	for _, opt := range opts {
		opt(b)
	}
	return signAndInit(t, b)
}

func signAndInit(tb testing.TB, b *types.Ballot) *types.Ballot {
	tb.Helper()
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	b.SmesherID = sig.NodeID()
	b.Signature = sig.Sign(signing.BALLOT, b.SignedBytes())
	require.NoError(tb, b.Initialize())
	return b
}

func createRefBallot(t *testing.T, actives types.ATXIDList) *types.Ballot {
	t.Helper()
	b := types.RandomBallot()
	b.RefBallot = types.EmptyBallotID
	b.EpochData = &types.EpochData{
		ActiveSetHash: actives.Hash(),
		Beacon:        types.RandomBeacon(),
	}
	return b
}

func checkProposal(t *testing.T, cdb *datastore.CachedDB, p *types.Proposal, exist bool) {
	t.Helper()
	got, err := proposals.Get(cdb, p.ID())
	if exist {
		require.NoError(t, err)
		require.Equal(t, p, got)
	} else {
		require.ErrorIs(t, err, sql.ErrNotFound)
	}
}

func TestBallot_MalformedData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	data, err := codec.Encode(&b.InnerBallot)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), p2p.NoPeer, data), errMalformedData)
}

func TestBallot_BadSignature(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	b.Signature[types.EdSignatureSize-1] = 0xff
	data := codec.MustEncode(b)
	got := th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), p2p.NoPeer, data)
	require.ErrorContains(t, got, "failed to verify ballot signature")
}

func TestBallot_WrongHash(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	err := th.HandleSyncedBallot(context.Background(), types.RandomHash(), peer, data)
	require.ErrorIs(t, err, errWrongHash)
	require.ErrorIs(t, err, pubsub.ErrValidationReject)
}

func TestBallot_KnownBallot(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	require.NoError(t, ballots.Add(th.cdb, b))
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	require.NoError(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data))
}

func TestBallot_BeforeEffectiveGenesis(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := types.RandomBallot()
	b.Layer = types.GetEffectiveGenesis()
	b = signAndInit(t, b)
	data := codec.MustEncode(b)
	require.ErrorContains(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), "", data), "ballot before effective genesis")
}

func TestBallot_EmptyATXID(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := types.RandomBallot()
	b.AtxID = types.EmptyATXID
	b = signAndInit(t, b)
	data := codec.MustEncode(b)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), "", data), errInvalidATXID)
}

func TestBallot_GoldenATXID(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := types.RandomBallot()
	b.AtxID = genGoldenATXID()
	b = signAndInit(t, b)
	data := codec.MustEncode(b)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), "", data), errInvalidATXID)
}

func TestBallot_RefBallotMissingEpochData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createRefBallot(t, types.ATXIDList{{1}, {2}})
	b.EpochData = nil
	signAndInit(t, b)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errMissingEpochData)
}

func TestBallot_RefBallotMissingBeacon(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	activeSet := types.ATXIDList{{1}, {2}}
	b := createRefBallot(t, activeSet)
	require.NoError(t, activesets.Add(th.cdb, activeSet.Hash(), &types.EpochActiveSet{Set: activeSet}))
	b.EpochData.Beacon = types.EmptyBeacon
	signAndInit(t, b)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errMissingBeacon)
}

func TestBallot_RefBallotEmptyActiveSet(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	activeSet := types.ATXIDList{{1}, {2}}
	b := createRefBallot(t, activeSet)
	signAndInit(t, b)
	data := codec.MustEncode(b)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetActiveSet(gomock.Any(), b.EpochData.ActiveSetHash)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), sql.ErrNotFound)
}

// TODO: remove after the network no longer populate ActiveSet in ballot.
func TestBallot_RefBallotDuplicateInActiveSet(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	activeSet := types.ATXIDList{{1}, {2}, {1}}
	b := createRefBallot(t, activeSet)
	b.ActiveSet = activeSet
	signAndInit(t, b)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	require.ErrorContains(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), "not sorted")
}

// TODO: remove after the network no longer populate ActiveSet in ballot.
func TestBallot_RefBallotActiveSetNotSorted(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	activeSet := types.ATXIDList(types.RandomActiveSet(11))
	b := createRefBallot(t, activeSet)
	b.ActiveSet = activeSet
	signAndInit(t, b)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	require.ErrorContains(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), "not sorted")
}

func TestBallot_NotRefBallotButHasEpochData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := types.RandomBallot()
	b.EpochData = &types.EpochData{}
	b = signAndInit(t, b)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errUnexpectedEpochData)
}

func TestBallot_BallotDoubleVotedWithinHdist(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	cutoff := lid.Sub(th.cfg.Hdist)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: cutoff.Add(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: cutoff.Add(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errDoubleVoting)
}

func TestBallot_BallotDoubleVotedWithinHdist_LyrBfrHdist(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	th.cfg.Hdist = lid.Add(1).Uint32()
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)

	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errDoubleVoting)
}

func TestBallot_BallotDoubleVotedOutsideHdist(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	cutoff := lid.Sub(th.cfg.Hdist)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: cutoff.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: cutoff.Sub(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)

	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), b).Return(nil, nil)
	require.NoError(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data))
}

func TestBallot_ConflictingForAndAgainst(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withAgainstBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errConflictingExceptions)
}

func TestBallot_ConflictingForAndAbstain(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withAbstain(lid.Sub(1)),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errConflictingExceptions)
}

func TestBallot_ConflictingAgainstAndAbstain(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	against := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(),
		withAgainstBlocks(against...),
		withAbstain(lid.Sub(1)),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range against {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errConflictingExceptions)
}

func TestBallot_ExceedMaxExceptions(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
		types.NewExistingBlock(types.BlockID{3}, types.InnerBlock{LayerIndex: lid.Sub(3)}),
		types.NewExistingBlock(types.BlockID{4}, types.InnerBlock{LayerIndex: lid.Sub(4)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errExceptionsOverflow)
}

func TestBallot_BallotsNotAvailable(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	data := codec.MustEncode(b)

	errUnknown := errors.New("unknown")
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errUnknown)
}

func TestBallot_ATXsNotAvailable(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errUnknown)
}

func TestBallot_ErrorCheckingEligible(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, errors.New("unknown")
		})
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errNotEligible)
}

func TestBallot_NotEligible(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, nil
		})
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), errNotEligible)
}

func TestBallot_Success(t *testing.T) {
	th := createTestHandler(t)
	lid := types.LayerID(100)
	th.mockSet.setCurrentLayer(lid)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), b).Return(nil, nil)
	decoded := &tortoise.DecodedBallot{BallotTortoiseData: b.ToTortoiseData()}
	th.md.EXPECT().DecodeBallot(decoded.BallotTortoiseData).Return(decoded, nil)
	th.md.EXPECT().StoreBallot(decoded).Return(nil)
	require.NoError(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data))
}

func TestBallot_MaliciousProofIgnoredInSyncFlow(t *testing.T) {
	th := createTestHandler(t)
	lid := types.LayerID(100)
	th.mockSet.setCurrentLayer(lid)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), b).Return(&types.MalfeasanceProof{Layer: lid}, nil)
	decoded := &tortoise.DecodedBallot{BallotTortoiseData: b.ToTortoiseData()}
	th.md.EXPECT().DecodeBallot(decoded.BallotTortoiseData).Return(decoded, nil)
	th.md.EXPECT().StoreBallot(decoded).Return(nil)
	require.NoError(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data))
}

func TestBallot_RefBallot(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	activeSet := types.ATXIDList{{1}, {2}, {3}}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withRefData(activeSet),
	)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(b.Layer.GetEpoch(), []types.ATXID{b.AtxID}).Return([]types.ATXID{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{b.AtxID})
	th.mf.EXPECT().GetActiveSet(gomock.Any(), activeSet.Hash()).DoAndReturn(func(_ context.Context, hash types.Hash32) error {
		return activesets.Add(th.cdb, hash, &types.EpochActiveSet{
			Epoch: b.Layer.GetEpoch(),
			Set:   activeSet,
		})
	})
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), activeSet).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), b).Return(nil, nil)
	require.NoError(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, codec.MustEncode(b)))
}

func TestBallot_DecodeBeforeVotesConsistency(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	th.mockSet.setCurrentLayer(b.Layer)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	b.Votes.Against = b.Votes.Support
	for _, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid.ID, types.InnerBlock{LayerIndex: b.Layer.Sub(1)})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	expected := errors.New("test")

	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)

	decoded := &tortoise.DecodedBallot{BallotTortoiseData: b.ToTortoiseData()}
	th.md.EXPECT().DecodeBallot(decoded.BallotTortoiseData).Return(decoded, expected)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), expected)
}

func TestBallot_DecodedStoreFailure(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	th.mockSet.setCurrentLayer(b.Layer)
	createAtx(t, th.cdb.Database, b.Layer.GetEpoch()-1, b.AtxID, b.SmesherID)
	b.Votes.Support = nil // just to avoid creating blocks
	expected := errors.New("test")

	data := codec.MustEncode(b)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{b.AtxID}).Return(types.ATXIDList{b.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).Return(true, nil).Times(1)

	decoded := &tortoise.DecodedBallot{BallotTortoiseData: b.ToTortoiseData()}
	th.md.EXPECT().DecodeBallot(decoded.BallotTortoiseData).Return(decoded, nil)
	th.mm.EXPECT().AddBallot(context.Background(), b).Return(nil, nil)
	th.md.EXPECT().StoreBallot(decoded).Return(expected)
	require.ErrorIs(t, th.HandleSyncedBallot(context.Background(), b.ID().AsHash32(), peer, data), expected)
}

func TestProposal_MalformedData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t, withProposalLayer(th.clock.CurrentLayer()))
	data, err := codec.Encode(&p.InnerProposal)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), p2p.NoPeer, data), errMalformedData)
	require.ErrorIs(t, th.HandleProposal(context.Background(), "", data), pubsub.ErrValidationReject)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_BeforeEffectiveGenesis(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t, withProposalLayer(th.clock.CurrentLayer()))
	p.Layer = types.GetEffectiveGenesis()
	data := encodeProposal(t, p)
	got := th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), p2p.NoPeer, data)
	require.ErrorContains(t, got, "proposal before effective genesis")

	require.Error(t, th.HandleProposal(context.Background(), "", data))
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_TooOld(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.LayerID(11)
	th.mockSet.setCurrentLayer(lid)
	p := createProposal(t, withProposalLayer(th.clock.CurrentLayer()))
	p.Layer = lid - 1
	data := encodeProposal(t, p)
	got := th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), p2p.NoPeer, data)
	require.ErrorContains(t, got, "proposal too late")

	require.Error(t, th.HandleProposal(context.Background(), "", data))
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_TooFuture(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t, withProposalLayer(th.clock.CurrentLayer()+10))
	data := encodeProposal(t, p)
	got := th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), p2p.NoPeer, data)
	require.ErrorContains(t, got, "proposal from future")
}

func TestProposal_BadSignature(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t, withProposalLayer(th.clock.CurrentLayer()))
	p.Signature = types.EmptyEdSignature
	data := encodeProposal(t, p)
	got := th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), p2p.NoPeer, data)
	require.ErrorContains(t, got, "failed to verify proposal signature")

	require.Error(t, th.HandleProposal(context.Background(), "", data))
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_InconsistentSmeshers(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *types.RandomBallot(),
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	p.Layer = th.clock.CurrentLayer()
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	p.Ballot.Signature = signer1.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Ballot.SmesherID = signer1.NodeID()
	p.Signature = signer2.Sign(signing.PROPOSAL, p.SignedBytes())
	p.SmesherID = signer2.NodeID()

	data := encodeProposal(t, p)
	got := th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), p2p.NoPeer, data)
	require.ErrorContains(t, got, "failed to verify proposal signature")

	require.Error(t, th.HandleProposal(context.Background(), "", data))
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_WrongHash(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t, withProposalLayer(th.clock.CurrentLayer()))
	data := encodeProposal(t, p)
	err := th.HandleSyncedProposal(context.Background(), types.RandomHash(), p2p.NoPeer, data)
	require.ErrorIs(t, err, errWrongHash)
	require.ErrorIs(t, err, pubsub.ErrValidationReject)
}

func TestProposal_KnownProposal(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t, withProposalLayer(th.clock.CurrentLayer()))
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
	require.NoError(t, proposals.Add(th.cdb, p))
	data := encodeProposal(t, p)
	require.NoError(t, th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), p2p.NoPeer, data))
	require.Error(t, th.HandleProposal(context.Background(), "", data))
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_DuplicateTXs(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := th.clock.CurrentLayer()
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	tid := types.RandomTransactionID()
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withTransactions(tid, tid),
	)
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{p.AtxID}).Return(types.ATXIDList{p.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), &p.Ballot).DoAndReturn(
		func(_ context.Context, got *types.Ballot) (*types.MalfeasanceProof, error) {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil, nil
		})
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*p))
	require.ErrorIs(t, th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), peer, data), errDuplicateTX)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_TXsNotAvailable(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := th.clock.CurrentLayer()
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{p.AtxID}).Return(types.ATXIDList{p.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), &p.Ballot).DoAndReturn(
		func(_ context.Context, got *types.Ballot) (*types.MalfeasanceProof, error) {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil, nil
		})

	errUnknown := errors.New("unknown")
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*p))
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), peer, data), errUnknown)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_FailedToAddProposalTXs(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := th.clock.CurrentLayer()
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{p.AtxID}).Return(types.ATXIDList{p.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), &p.Ballot).DoAndReturn(
		func(_ context.Context, got *types.Ballot) (*types.MalfeasanceProof, error) {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil, nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*p))
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.Layer, p.ID(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), peer, data), errUnknown)
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_ProposalGossip_Concurrent(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := th.clock.CurrentLayer()
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)

	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*p)).MinTimes(1).MaxTimes(2)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).MinTimes(1).MaxTimes(2)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{p.AtxID}).Return(types.ATXIDList{p.AtxID}).MinTimes(1).MaxTimes(2)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).MinTimes(1).MaxTimes(2)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		}).MinTimes(1).MaxTimes(2)
	th.mm.EXPECT().AddBallot(context.Background(), &p.Ballot).DoAndReturn(
		func(_ context.Context, got *types.Ballot) (*types.MalfeasanceProof, error) {
			_ = ballots.Add(th.cdb, got)
			return nil, nil
		}).MinTimes(1).MaxTimes(2)
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).MinTimes(1).MaxTimes(2)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.Layer, p.ID(), p.TxIDs).Return(nil).Times(1)

	var wg sync.WaitGroup
	wg.Add(2)
	var res1, res2 error
	go func() {
		defer wg.Done()
		res1 = th.HandleProposal(context.Background(), peer, data)
	}()
	go func() {
		defer wg.Done()
		res2 = th.HandleProposal(context.Background(), peer, data)
	}()
	wg.Wait()
	if res1 == nil {
		require.Error(t, res2)
	} else {
		require.Error(t, res1)
		require.Equal(t, nil, res2)
	}
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_BroadcastMaliciousGossip(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := th.clock.CurrentLayer()
	p := createProposal(t, withLayer(lid))
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
	require.NoError(t, proposals.Add(th.cdb, p))
	checkProposal(t, th.cdb, p, true)

	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	pMal := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, pMal.Layer.GetEpoch()-1, pMal.AtxID, pMal.SmesherID)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*pMal))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{pMal.Votes.Base, pMal.RefBallot})
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{pMal.AtxID}).Return(types.ATXIDList{pMal.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{pMal.AtxID})
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, pMal.Ballot.ID(), ballot.ID())
			return true, nil
		})
	ballotProof := types.BallotProof{
		Messages: [2]types.BallotProofMsg{
			{},
			{},
		},
	}
	proof := &types.MalfeasanceProof{
		Layer: lid,
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &ballotProof,
		},
	}
	th.mm.EXPECT().AddBallot(context.Background(), &pMal.Ballot).DoAndReturn(
		func(_ context.Context, got *types.Ballot) (*types.MalfeasanceProof, error) {
			_ = ballots.Add(th.cdb, got)
			return proof, nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), pMal.TxIDs).Return(nil)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), pMal.Layer, pMal.ID(), pMal.TxIDs)
	th.mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var gossip types.MalfeasanceGossip
			require.NoError(t, codec.Decode(data, &gossip))
			require.Equal(t, *proof, gossip.MalfeasanceProof)
			return nil
		})
	data := encodeProposal(t, pMal)
	require.Error(t, th.HandleProposal(context.Background(), peer, data))
	checkProposal(t, th.cdb, pMal, true)
}

func TestProposal_ProposalGossip_Fetched(t *testing.T) {
	tests := []struct {
		name        string
		propFetched bool
	}{
		{
			name:        "proposal fetched",
			propFetched: true,
		},
		{
			name: "ballot fetched",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			th := createTestHandlerNoopDecoder(t)
			lid := th.clock.CurrentLayer()
			supported := []*types.Block{
				types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
				types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
			}
			p := createProposal(t,
				withLayer(lid),
				withSupportBlocks(supported...),
			)
			createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
			for _, block := range supported {
				require.NoError(t, blocks.Add(th.cdb, block))
			}
			data := encodeProposal(t, p)

			peer := p2p.Peer("buddy")
			th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*p))
			th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil)
			th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{p.AtxID}).Return(types.ATXIDList{p.AtxID})
			th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil)
			th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
				func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
					require.Equal(t, p.Ballot.ID(), ballot.ID())
					if tc.propFetched {
						// a separate goroutine fetched the propFetched and saved it to database
						require.NoError(t, proposals.Add(th.cdb, p))
						require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
					} else {
						// a separate goroutine fetched the ballot and saved it to database
						require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
					}
					return true, nil
				})
			th.mm.EXPECT().AddBallot(context.Background(), &p.Ballot).Return(nil, nil)
			th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil)
			if tc.propFetched {
				require.Error(t, th.HandleProposal(context.Background(), peer, data))
			} else {
				th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.Layer, p.ID(), p.TxIDs).Return(nil).Times(1)
				require.Equal(t, nil, th.HandleProposal(context.Background(), peer, data))
			}
			checkProposal(t, th.cdb, p, true)
		})
	}
}

func TestProposal_ValidProposal(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := th.clock.CurrentLayer()
	blks := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(blks...),
	)
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	for _, block := range blks {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{p.AtxID}).Return(types.ATXIDList{p.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), &p.Ballot).DoAndReturn(
		func(_ context.Context, got *types.Ballot) (*types.MalfeasanceProof, error) {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil, nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*p))
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.Layer, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), peer, data))
	checkProposal(t, th.cdb, p, true)
}

func TestMetrics(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := th.clock.CurrentLayer()
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	createAtx(t, th.cdb.Database, p.Layer.GetEpoch()-1, p.AtxID, p.SmesherID)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), types.ATXIDList{p.AtxID}).Return(types.ATXIDList{p.AtxID})
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), nil).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot, _ []types.ATXID) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(context.Background(), &p.Ballot).DoAndReturn(
		func(_ context.Context, got *types.Ballot) (*types.MalfeasanceProof, error) {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil, nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	peer := p2p.Peer("buddy")
	th.mf.EXPECT().RegisterPeerHashes(peer, collectHashes(*p))
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.Layer, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleSyncedProposal(context.Background(), p.ID().AsHash32(), peer, data))
	checkProposal(t, th.cdb, p, true)
	counts, err := testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_proposal_size")
	require.NoError(t, err)
	require.Equal(t, 1, counts)
	counts, err = testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_num_txs_in_proposal")
	require.NoError(t, err)
	require.Equal(t, 1, counts)
	counts, err = testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_num_blocks_in_exception")
	require.NoError(t, err)
	require.Equal(t, 3, counts)
}

func TestCollectHashes(t *testing.T) {
	p := createProposal(t)
	b := p.Ballot
	expected := []types.Hash32{b.RefBallot.AsHash32()}
	expected = append(expected, b.Votes.Base.AsHash32())
	for _, header := range b.Votes.Support {
		expected = append(expected, header.ID.AsHash32())
	}
	require.ElementsMatch(t, expected, collectHashes(b))

	expected = append(expected, types.TransactionIDsToHashes(p.TxIDs)...)
	require.ElementsMatch(t, expected, collectHashes(*p))
}

func TestHandleActiveSet(t *testing.T) {
	good := []types.ATXID{{1}, {2}, {3}}
	notsorted := []types.ATXID{{3}, {1}, {2}}
	for _, tc := range []struct {
		desc                    string
		id                      types.Hash32
		data                    []byte
		tortoise, fetch, stored []types.ATXID
		fetchErr                error
		err                     string
	}{
		{
			desc:     "sanity",
			id:       types.ATXIDList(good).Hash(),
			data:     codec.MustEncode(&types.EpochActiveSet{Epoch: 2, Set: good}),
			tortoise: good,
			fetch:    good,
			stored:   good,
		},
		{
			desc: "malformed",
			data: []byte("test"),
			err:  "malformed",
		},
		{
			desc: "not sorted",
			data: codec.MustEncode(&types.EpochActiveSet{Epoch: 2, Set: notsorted}),
			err:  "not sorted",
		},
		{
			desc: "wrong hash",
			id:   types.Hash32{1, 2, 3},
			data: codec.MustEncode(&types.EpochActiveSet{Epoch: 2, Set: good}),
			err:  "wrong hash",
		},
		{
			desc:     "fetcher error",
			id:       types.ATXIDList(good).Hash(),
			data:     codec.MustEncode(&types.EpochActiveSet{Epoch: 2, Set: good}),
			tortoise: good,
			fetch:    good,
			fetchErr: errors.New("fetcher failed"),
			err:      "fetcher failed",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			th := createTestHandler(t)
			pid := p2p.Peer("any")
			var eset types.EpochActiveSet
			if err := codec.Decode(tc.data, &eset); err == nil {
				th.mf.EXPECT().RegisterPeerHashes(pid, types.ATXIDsToHashes(eset.Set))
			}
			if tc.tortoise != nil {
				th.md.EXPECT().GetMissingActiveSet(eset.Epoch, tc.tortoise).Return(tc.fetch)
			}
			if tc.fetch != nil {
				th.mf.EXPECT().GetAtxs(gomock.Any(), tc.fetch).Return(tc.fetchErr)
			}
			err := th.HandleActiveSet(context.Background(), tc.id, pid, tc.data)
			if len(tc.err) > 0 {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
			if tc.stored != nil {
				stored, err := activesets.Get(th.cdb, tc.id)
				require.NoError(t, err)
				require.Equal(t, tc.stored, stored.Set)
			}
		})
	}
}

func gproposal(signer *signing.EdSigner, atxid types.ATXID,
	layer types.LayerID, edata *types.EpochData,
) types.Proposal {
	p := types.Proposal{}
	p.Layer = layer
	p.AtxID = atxid
	p.EpochData = edata
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Ballot.SmesherID = signer.NodeID()
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	p.SmesherID = signer.NodeID()
	return p
}

func TestHandleSyncedProposalActiveSet(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	set := types.ATXIDList{{1}, {2}}
	lid := types.LayerID(20)
	good := gproposal(signer, types.ATXID{1}, lid, &types.EpochData{
		ActiveSetHash: set.Hash(),
		Beacon:        types.Beacon{1},
	})
	require.NoError(t, good.Initialize())
	th := createTestHandler(t)
	pid := p2p.Peer("any")

	th.mclock.EXPECT().CurrentLayer().Return(lid).AnyTimes()
	th.mm.EXPECT().ProcessedLayer().Return(lid - 2).AnyTimes()
	th.mclock.EXPECT().LayerToTime(gomock.Any())
	th.mf.EXPECT().RegisterPeerHashes(pid, gomock.Any()).AnyTimes()
	th.md.EXPECT().GetMissingActiveSet(gomock.Any(), gomock.Any()).AnyTimes()
	th.mf.EXPECT().GetActiveSet(gomock.Any(), set.Hash()).DoAndReturn(
		func(_ context.Context, got types.Hash32) error {
			require.NoError(t, activesets.Add(th.cdb, got, &types.EpochActiveSet{
				Set: set,
			}))
			return nil
		},
	)
	th.mf.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).AnyTimes()
	th.mf.EXPECT().GetBallots(gomock.Any(), gomock.Any()).AnyTimes()
	th.mockSet.decodeAnyBallots()
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(true, nil)
	th.mm.EXPECT().AddBallot(gomock.Any(), gomock.Any()).AnyTimes()
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	err = th.HandleSyncedProposal(context.Background(), good.ID().AsHash32(), pid, codec.MustEncode(&good))
	require.NoError(t, err)
}
