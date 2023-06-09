package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	numBallots = 10
	numBlocks  = 5
	numTXs     = 20
)

type testMesh struct {
	*Mesh
	db           sql.Executor
	mockClock    *mocks.MocklayerClock
	mockVM       *mocks.MockvmState
	mockState    *mocks.MockconservativeState
	mockTortoise *smocks.MockTortoise
}

func createTestMesh(t *testing.T) *testMesh {
	t.Helper()
	types.SetLayersPerEpoch(3)
	lg := logtest.New(t)
	db := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	tm := &testMesh{
		db:           db,
		mockClock:    mocks.NewMocklayerClock(ctrl),
		mockVM:       mocks.NewMockvmState(ctrl),
		mockState:    mocks.NewMockconservativeState(ctrl),
		mockTortoise: smocks.NewMockTortoise(ctrl),
	}
	exec := NewExecutor(db, tm.mockVM, tm.mockState, lg)
	msh, err := NewMesh(db, tm.mockClock, tm.mockTortoise, exec, tm.mockState, lg)
	require.NoError(t, err)
	gLid := types.GetEffectiveGenesis()
	checkLastAppliedInDB(t, msh, gLid)
	checkProcessedInDB(t, msh, gLid)
	tm.Mesh = msh
	return tm
}

func genTx(t testing.TB, signer *signing.EdSigner, dest types.Address, amount, nonce, price uint64) types.Transaction {
	t.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount, nonce)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = 100
	tx.MaxSpend = amount
	tx.GasPrice = price
	tx.Nonce = nonce
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return tx
}

func CreateAndSaveTxs(t testing.TB, db sql.Executor, numOfTxs int) []types.TransactionID {
	t.Helper()
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		addr := types.GenerateAddress([]byte("1"))
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		tx := genTx(t, signer, addr, 1, 10, 100)
		require.NoError(t, transactions.Add(db, &tx, time.Now()))
		txIDs = append(txIDs, tx.ID)
	}
	return txIDs
}

func createBlock(t testing.TB, db sql.Executor, mesh *Mesh, layerID types.LayerID, nodeID types.NodeID) *types.Block {
	t.Helper()
	txIDs := CreateAndSaveTxs(t, db, numTXs)
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			TxIDs:      txIDs,
		},
	}
	b.Initialize()
	require.NoError(t, blocks.Add(mesh.cdb, b))
	return b
}

func createLayerBlocks(t *testing.T, db sql.Executor, mesh *Mesh, lyrID types.LayerID) []*types.Block {
	t.Helper()
	blks := make([]*types.Block, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		nodeID := types.NodeID{byte(i)}
		blk := createBlock(t, db, mesh, lyrID, nodeID)
		blks = append(blks, blk)
	}
	return blks
}

func genLayerBallot(tb testing.TB, layerID types.LayerID) *types.Ballot {
	b := types.RandomBallot()
	b.Layer = layerID
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	b.Signature = sig.Sign(signing.BALLOT, b.SignedBytes())
	b.SmesherID = sig.NodeID()
	require.NoError(tb, b.Initialize())
	return b
}

func createLayerBallots(tb testing.TB, mesh *Mesh, lyrID types.LayerID) []*types.Ballot {
	tb.Helper()
	blts := make([]*types.Ballot, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballot := genLayerBallot(tb, lyrID)
		blts = append(blts, ballot)
		malProof, err := mesh.AddBallot(context.Background(), ballot)
		require.NoError(tb, err)
		require.Nil(tb, malProof)
	}
	return blts
}

func checkLastAppliedInDB(t *testing.T, mesh *Mesh, expected types.LayerID) {
	t.Helper()
	lid, err := layers.GetLastApplied(mesh.cdb)
	require.NoError(t, err)
	require.Equal(t, expected, lid)
}

func checkProcessedInDB(t *testing.T, mesh *Mesh, expected types.LayerID) {
	t.Helper()
	lid, err := layers.GetProcessed(mesh.cdb)
	require.NoError(t, err)
	require.Equal(t, expected, lid)
}

func TestMesh_FromGenesis(t *testing.T) {
	tm := createTestMesh(t)
	gotP := tm.Mesh.ProcessedLayer()
	require.Equal(t, types.GetEffectiveGenesis(), gotP)
	getLS := tm.Mesh.LatestLayerInState()
	require.Equal(t, types.GetEffectiveGenesis(), getLS)
	gotL := tm.Mesh.LatestLayer()
	require.Equal(t, types.GetEffectiveGenesis(), gotL)

	opinion, err := layers.GetAggregatedHash(tm.cdb, types.GetEffectiveGenesis())
	require.NoError(t, err)
	buf := hash.Sum(nil)
	require.Equal(t, buf[:], opinion[:])
}

func TestMesh_WakeUpWhileGenesis(t *testing.T) {
	tm := createTestMesh(t)
	msh, err := NewMesh(tm.cdb, tm.mockClock, tm.mockTortoise, tm.executor, tm.mockState, logtest.New(t))
	require.NoError(t, err)
	gLid := types.GetEffectiveGenesis()
	checkProcessedInDB(t, msh, gLid)
	checkLastAppliedInDB(t, msh, gLid)
	gotL := msh.LatestLayer()
	require.Equal(t, gLid, gotL)
	gotP := msh.ProcessedLayer()
	require.Equal(t, gLid, gotP)
	gotLS := msh.LatestLayerInState()
	require.Equal(t, gLid, gotLS)
}

func TestMesh_WakeUp(t *testing.T) {
	tm := createTestMesh(t)
	latest := types.LayerID(11)
	b := types.NewExistingBallot(types.BallotID{1, 2, 3}, types.EmptyEdSignature, types.EmptyNodeID, latest)
	require.NoError(t, ballots.Add(tm.cdb, &b))
	require.NoError(t, layers.SetProcessed(tm.cdb, latest))
	latestState := latest.Sub(1)
	require.NoError(t, layers.SetApplied(tm.cdb, latestState, types.RandomBlockID()))

	tm.mockVM.EXPECT().Revert(latestState)
	tm.mockState.EXPECT().RevertCache(latestState)
	tm.mockVM.EXPECT().GetStateRoot()
	msh, err := NewMesh(tm.cdb, tm.mockClock, tm.mockTortoise, tm.executor, tm.mockState, logtest.New(t))
	require.NoError(t, err)
	gotL := msh.LatestLayer()
	require.Equal(t, latest, gotL)
	gotP := msh.ProcessedLayer()
	require.Equal(t, latest, gotP)
	gotLS := msh.LatestLayerInState()
	require.Equal(t, latestState, gotLS)
}

func TestMesh_GetLayer(t *testing.T) {
	tm := createTestMesh(t)
	id := types.GetEffectiveGenesis().Add(1)
	lyr, err := tm.GetLayer(id)
	require.NoError(t, err)
	require.Empty(t, lyr.Blocks())
	require.Empty(t, lyr.Ballots())

	blks := createLayerBlocks(t, tm.db, tm.Mesh, id)
	blts := createLayerBallots(t, tm.Mesh, id)
	lyr, err = tm.GetLayer(id)
	require.NoError(t, err)
	require.Equal(t, id, lyr.Index())
	require.Len(t, lyr.Ballots(), len(blts))
	require.ElementsMatch(t, blts, lyr.Ballots())
	require.ElementsMatch(t, blks, lyr.Blocks())
}

func TestMesh_LatestKnownLayer(t *testing.T) {
	tm := createTestMesh(t)
	lg := logtest.New(t)
	tm.setLatestLayer(lg, types.LayerID(3))
	tm.setLatestLayer(lg, types.LayerID(7))
	tm.setLatestLayer(lg, types.LayerID(10))
	tm.setLatestLayer(lg, types.LayerID(1))
	tm.setLatestLayer(lg, types.LayerID(2))
	require.Equal(t, types.LayerID(10), tm.LatestLayer(), "wrong layer")
}

func genLayerBlock(layerID types.LayerID, txs []types.TransactionID) *types.Block {
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			TxIDs:      txs,
		},
	}
	b.Initialize()
	return b
}

func TestMesh_AddTXsFromProposal(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	layerID := types.GetEffectiveGenesis().Add(1)
	pid := types.RandomProposalID()
	txIDs := types.RandomTXSet(numTXs)
	tm.mockState.EXPECT().LinkTXsWithProposal(layerID, pid, txIDs).Return(nil)
	r.NoError(tm.AddTXsFromProposal(context.Background(), layerID, pid, txIDs))
}

func TestMesh_AddBlockWithTXs(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()

	txIDs := types.RandomTXSet(numTXs)
	layerID := types.GetEffectiveGenesis().Add(1)
	block := genLayerBlock(layerID, txIDs)
	tm.mockState.EXPECT().LinkTXsWithBlock(layerID, block.ID(), txIDs).Return(nil)
	r.NoError(tm.AddBlockWithTXs(context.Background(), block))
}

func TestMesh_CallOnBlock(t *testing.T) {
	tm := createTestMesh(t)
	block := types.Block{}
	block.LayerIndex = types.LayerID(10)
	block.Initialize()

	tm.mockTortoise.EXPECT().OnBlock(block.ToVote())
	tm.mockState.EXPECT().LinkTXsWithBlock(block.LayerIndex, block.ID(), block.TxIDs)
	require.NoError(t, tm.AddBlockWithTXs(context.Background(), &block))
}

func TestMesh_MaliciousBallots(t *testing.T) {
	tm := createTestMesh(t)
	lid := types.LayerID(1)
	nodeID := types.BytesToNodeID([]byte{1, 1, 1})

	blts := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, lid),
		types.NewExistingBallot(types.BallotID{2}, types.RandomEdSignature(), nodeID, lid),
		types.NewExistingBallot(types.BallotID{3}, types.RandomEdSignature(), nodeID, lid),
	}
	malProof, err := tm.AddBallot(context.Background(), &blts[0])
	require.NoError(t, err)
	require.Nil(t, malProof)
	require.False(t, blts[0].IsMalicious())
	mal, err := identities.IsMalicious(tm.cdb, nodeID)
	require.NoError(t, err)
	require.False(t, mal)
	saved, err := identities.GetMalfeasanceProof(tm.cdb, nodeID)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, saved)

	// second one will create a MalfeasanceProof
	malProof, err = tm.AddBallot(context.Background(), &blts[1])
	require.NoError(t, err)
	require.NotNil(t, malProof)
	require.True(t, blts[1].IsMalicious())
	require.EqualValues(t, types.MultipleBallots, malProof.Proof.Type)
	proof, ok := malProof.Proof.Data.(*types.BallotProof)
	require.True(t, ok)
	require.Equal(t, blts[0].Layer, proof.Messages[0].InnerMsg.Layer)
	require.Equal(t, blts[0].HashInnerBytes(), proof.Messages[0].InnerMsg.MsgHash.Bytes())
	require.Equal(t, blts[0].Signature, proof.Messages[0].Signature)
	require.Equal(t, blts[0].SmesherID, proof.Messages[0].SmesherID)
	require.Equal(t, blts[1].Layer, proof.Messages[0].InnerMsg.Layer)
	require.Equal(t, blts[1].HashInnerBytes(), proof.Messages[0].InnerMsg.MsgHash.Bytes())
	require.Equal(t, blts[1].Signature, proof.Messages[1].Signature)
	require.Equal(t, blts[1].SmesherID, proof.Messages[1].SmesherID)
	mal, err = identities.IsMalicious(tm.cdb, nodeID)
	require.NoError(t, err)
	require.True(t, mal)
	saved, err = identities.GetMalfeasanceProof(tm.cdb, nodeID)
	require.NoError(t, err)
	require.EqualValues(t, malProof, saved)
	expected := malProof

	// third one will NOT generate another MalfeasanceProof
	malProof, err = tm.AddBallot(context.Background(), &blts[2])
	require.NoError(t, err)
	require.Nil(t, malProof)
	// but identity is still malicious
	mal, err = identities.IsMalicious(tm.cdb, nodeID)
	require.NoError(t, err)
	require.True(t, mal)
	saved, err = identities.GetMalfeasanceProof(tm.cdb, nodeID)
	require.NoError(t, err)
	require.EqualValues(t, expected, saved)
}

func TestProcessLayer(t *testing.T) {
	t.Parallel()
	type call struct {
		// inputs
		updates []result.Layer
		results []result.Layer

		// outputs
		err      string
		executed []types.BlockID
		applied  []types.BlockID
		validity map[types.BlockID]bool
	}
	type testCase struct {
		desc  string
		calls []call
	}
	types.SetLayersPerEpoch(3)
	start := types.GetEffectiveGenesis().Add(1)
	for _, tc := range []testCase{
		{
			"sanity",
			[]call{
				{
					updates: rlayers(
						rlayer(start,
							rblock(idg("1"), fixture.Good()),
							rblock(idg("2"), fixture.Data(), fixture.Invalid())),
					),
					executed: []types.BlockID{idg("1")},
					applied:  []types.BlockID{idg("1")},
					validity: map[types.BlockID]bool{
						idg("1"): true,
						idg("2"): false,
					},
				},
			},
		},
		{
			"missing valid",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Valid())),
					),
					err: "missing",
				},
				{
					results: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Data(), fixture.Valid())),
					),
					executed: []types.BlockID{idg("1")},
					applied:  []types.BlockID{idg("1")},
					validity: map[types.BlockID]bool{
						idg("1"): true,
					},
				},
			},
		},
		{
			"missing invalid",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Valid())),
					),
					err: "missing",
				},
				{
					results: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Invalid())),
					),
					executed: []types.BlockID{{}},
					applied:  []types.BlockID{{}},
				},
			},
		},
		{
			"revert from empty",
			[]call{
				{
					updates: rlayers(
						rlayer(start),
					),
					executed: []types.BlockID{{}},
					applied:  []types.BlockID{{0}},
				},
				{
					updates: []result.Layer{
						rlayer(start, rblock(idg("2"), fixture.Valid(), fixture.Data())),
					},
					executed: []types.BlockID{idg("2")},
					applied:  []types.BlockID{idg("2")},
				},
			},
		},
		{
			"hare to valid",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Hare(), fixture.Data())),
					),
					executed: []types.BlockID{idg("1")},
					applied:  []types.BlockID{idg("1")},
				},
				{
					updates: []result.Layer{
						rlayer(start, rblock(idg("1"), fixture.Hare(), fixture.Data(), fixture.Valid())),
					},
					applied: []types.BlockID{idg("1")},
				},
			},
		},
		{
			"multiple layers of hare",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Hare(), fixture.Data())),
					),
					executed: []types.BlockID{idg("1")},
					applied:  []types.BlockID{idg("1")},
				},
				{
					updates: rlayers(
						rlayer(start.Add(1), rblock(idg("2"), fixture.Hare(), fixture.Data())),
					),
					executed: []types.BlockID{idg("2")},
					applied:  []types.BlockID{idg("1"), idg("2")},
				},
			},
		},
		{
			"hare to invalid",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Valid(), fixture.Data())),
					),
					executed: []types.BlockID{idg("1")},
					applied:  []types.BlockID{idg("1")},
				},
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Invalid(), fixture.Hare(), fixture.Data())),
					),
					executed: []types.BlockID{{0}},
					applied:  []types.BlockID{{0}},
				},
			},
		},
		{
			"reorg",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Valid(), fixture.Data())),
						rlayer(start+1, rblock(idg("2"), fixture.Valid(), fixture.Data())),
					),
					executed: []types.BlockID{idg("1"), idg("2")},
					applied:  []types.BlockID{idg("1"), idg("2")},
				},
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Invalid(), fixture.Data())),
						rlayer(start+1, rblock(idg("2"), fixture.Valid(), fixture.Data())),
					),
					executed: []types.BlockID{types.EmptyBlockID, idg("2")},
					applied:  []types.BlockID{types.EmptyBlockID, idg("2")},
				},
			},
		},
		{
			"reorg with missing",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Valid(), fixture.Data())),
						rlayer(start+1, rblock(idg("2"), fixture.Valid(), fixture.Data())),
					),
					executed: []types.BlockID{idg("1"), idg("2")},
					applied:  []types.BlockID{idg("1"), idg("2")},
				},
				{
					updates: rlayers(
						rlayer(start,
							rblock(idg("1"), fixture.Invalid(), fixture.Data()),
							rblock(idg("3"), fixture.Valid()),
						),
						rlayer(start+1,
							rblock(idg("2"), fixture.Valid(), fixture.Data())),
					),
					err: "missing",
				},
				{
					results: rlayers(
						rlayer(start,
							rblock(idg("1"), fixture.Invalid(), fixture.Data()),
							rblock(idg("3"), fixture.Valid(), fixture.Data()),
						),
						rlayer(start+1,
							rblock(idg("2"), fixture.Valid(), fixture.Data())),
					),
					executed: []types.BlockID{idg("3"), idg("2")},
					applied:  []types.BlockID{idg("3"), idg("2")},
				},
			},
		},
		{
			"more updates after failure",
			[]call{
				{
					updates: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Valid())),
					),
					err: "missing",
				},
				{
					updates: rlayers(
						rlayer(start+1, rblock(idg("2"), fixture.Valid(), fixture.Data())),
					),
					results: rlayers(
						rlayer(start, rblock(idg("1"), fixture.Valid(), fixture.Data())),
						rlayer(start+1, rblock(idg("2"), fixture.Valid(), fixture.Data())),
					),
					executed: []types.BlockID{idg("1"), idg("2")},
					applied:  []types.BlockID{idg("1"), idg("2")},
				},
			},
		},
		{
			"empty non verified",
			[]call{
				{
					updates: rlayers(
						fixture.RLayerNonFinal(start),
					),
				},
				{
					updates: rlayers(
						fixture.RLayer(start),
					),
					executed: []types.BlockID{{}},
					applied:  []types.BlockID{{}},
				},
			},
		},
		{
			"missing layer",
			[]call{
				{
					updates: rlayers(
						fixture.RLayerNonFinal(start.Add(1),
							fixture.RBlock(fixture.IDGen("2"), fixture.Valid(), fixture.Data()),
						),
					),
					results: rlayers(
						rlayer(start,
							fixture.RBlock(fixture.IDGen("1"), fixture.Valid(), fixture.Data()),
						),
						rlayer(start.Add(1),
							fixture.RBlock(fixture.IDGen("2"), fixture.Valid(), fixture.Data()),
						),
					),
					executed: []types.BlockID{idg("1"), idg("2")},
					applied:  []types.BlockID{idg("1"), idg("2")},
				},
			},
		},
		{
			"silent revert",
			[]call{
				{
					updates: rlayers(
						rlayer(start,
							rblock(fixture.IDGen("1"), fixture.Invalid(), fixture.Data()),
						),
						rlayer(start.Add(1),
							rblock(fixture.IDGen("2"), fixture.Invalid(), fixture.Data()),
						),
					),
					executed: []types.BlockID{{}, {}},
					applied:  []types.BlockID{{}, {}},
				},
				{
					updates: rlayers(
						rlayer(start,
							rblock(fixture.IDGen("1"), fixture.Invalid(), fixture.Data()),
						)),
					applied: []types.BlockID{{}, {}},
				},
				{
					updates: rlayers(
						rlayer(start.Add(1),
							rblock(fixture.IDGen("2"), fixture.Valid(), fixture.Data()),
						),
					),
					executed: []types.BlockID{idg("2")},
					applied:  []types.BlockID{{}, idg("2")},
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			tm := createTestMesh(t)
			tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gomock.Any()).AnyTimes()
			tm.mockVM.EXPECT().GetStateRoot().AnyTimes()
			tm.mockVM.EXPECT().Revert(gomock.Any()).AnyTimes()
			tm.mockState.EXPECT().RevertCache(gomock.Any()).AnyTimes()

			lid := start
			for _, c := range tc.calls {
				for _, executed := range c.executed {
					tm.mockVM.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any())
					tm.mockState.EXPECT().UpdateCache(gomock.Any(), gomock.Any(), executed, gomock.Any(), gomock.Any()).Return(nil)
				}
				tm.mockTortoise.EXPECT().Updates().Return(c.updates)
				if c.results != nil {
					tm.mockTortoise.EXPECT().Results(gomock.Any(), gomock.Any()).Return(c.results, nil)
				}
				ensuresDatabaseConsistent(t, tm.cdb, c.updates)
				ensuresDatabaseConsistent(t, tm.cdb, c.results)
				err := tm.ProcessLayer(context.TODO(), lid)
				if len(c.err) > 0 {
					require.ErrorContains(t, err, c.err)
				} else {
					require.NoError(t, err)
				}
				for i := range c.applied {
					applied, err := layers.GetApplied(tm.cdb, start.Add(uint32(i)))
					require.NoError(t, err)
					require.Equal(t, c.applied[i], applied)
				}
				for bid, valid := range c.validity {
					stored, err := blocks.IsValid(tm.cdb, bid)
					require.NoError(t, err)
					require.Equal(t, valid, stored, "%v", bid)
				}
				lid = lid.Add(1)
			}
		})
	}
}

func ensuresDatabaseConsistent(t *testing.T, db sql.Executor, results []result.Layer) {
	for _, layer := range results {
		for _, rst := range layer.Blocks {
			if !rst.Data {
				continue
			}
			_ = blocks.Add(db, types.NewExistingBlock(rst.Header.ID, types.InnerBlock{
				LayerIndex: layer.Layer,
			}))
		}
	}
}

var (
	rlayers = fixture.RLayers
	rlayer  = fixture.RLayer
	rblock  = fixture.RBlock
	idg     = fixture.IDGen
)

func validcert(id types.BlockID) certificates.CertValidity {
	return certificates.CertValidity{
		Block: id,
		Valid: true,
	}
}

func invalidcert(id types.BlockID) certificates.CertValidity {
	return certificates.CertValidity{
		Block: id,
	}
}

func fullcert(id types.BlockID) certificates.CertValidity {
	return certificates.CertValidity{
		Block: id,
		Cert:  &types.Certificate{BlockID: id},
		Valid: true,
	}
}

func TestProcessLayerPerHareOutput(t *testing.T) {
	t.Parallel()
	type cert struct {
		layer types.LayerID
		cert  certificates.CertValidity
	}
	type call struct {
		lid    types.LayerID
		bid    types.BlockID
		onHare bool
		expect []certificates.CertValidity
		err    string
	}
	types.SetLayersPerEpoch(3)
	start := types.GetEffectiveGenesis().Add(1)
	for _, tc := range []struct {
		desc  string
		calls []call
		certs []cert
	}{
		{
			desc: "sanity",
			calls: []call{
				{
					lid: start, bid: idg("1"), onHare: true,
					expect: []certificates.CertValidity{validcert(idg("1"))},
				},
			},
		},
		{
			desc: "empty",
			calls: []call{
				{
					lid: start, bid: types.BlockID{}, onHare: true,
					expect: []certificates.CertValidity{validcert(types.BlockID{})},
				},
			},
		},
		{
			desc: "exists",
			calls: []call{
				{
					lid: start, bid: idg("1"),
					expect: []certificates.CertValidity{validcert(idg("1"))},
				},
			},
			certs: []cert{
				{layer: start, cert: validcert(idg("1"))},
			},
		},
		{
			desc: "exists different",
			calls: []call{
				{
					lid: start, bid: idg("1"),
					expect: []certificates.CertValidity{invalidcert(idg("1")), validcert(idg("2"))},
				},
			},
			certs: []cert{
				{layer: start, cert: validcert(idg("2"))},
			},
		},
		{
			desc: "exists different invalid",
			calls: []call{
				{
					lid: start, bid: idg("1"),
					expect: []certificates.CertValidity{validcert(idg("1")), invalidcert(idg("2"))},
				},
			},
			certs: []cert{
				{layer: start, cert: invalidcert(idg("2"))},
			},
		},
		{
			desc: "many",
			calls: []call{
				{
					lid: start, bid: idg("1"),
					expect: []certificates.CertValidity{
						fullcert(idg("2")),
						fullcert(idg("3")),
						invalidcert(idg("1")),
					},
				},
			},
			certs: []cert{
				{layer: start, cert: fullcert(idg("2"))},
				{layer: start, cert: fullcert(idg("3"))},
			},
		},
		{
			desc: "out of order",
			calls: []call{
				{
					lid: start, bid: idg("1"), onHare: true,
					expect: []certificates.CertValidity{
						validcert(idg("1")),
					},
				},
				{
					lid: start.Add(2), bid: idg("2"), onHare: true,
					expect: []certificates.CertValidity{
						validcert(idg("2")),
					},
				},
				{
					lid: start.Add(1), bid: idg("3"), onHare: true,
					expect: []certificates.CertValidity{
						validcert(idg("3")),
					},
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			tm := createTestMesh(t)
			tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gomock.Any()).AnyTimes()
			tm.mockTortoise.EXPECT().Updates().Return(nil).AnyTimes() // this makes ProcessLayer noop
			for _, c := range tc.certs {
				if c.cert.Cert != nil {
					require.NoError(t, certificates.Add(tm.cdb, c.layer, c.cert.Cert))
				} else if c.cert.Valid {
					require.NoError(t, certificates.SetHareOutput(tm.cdb, c.layer, c.cert.Block))
				} else {
					require.NoError(t, certificates.SetHareOutputInvalid(tm.cdb, c.layer, c.cert.Block))
				}
			}
			for _, c := range tc.calls {
				if c.onHare {
					tm.mockTortoise.EXPECT().OnHareOutput(c.lid, c.bid)
				}
				err := tm.ProcessLayerPerHareOutput(context.TODO(), c.lid, c.bid, false)
				if len(c.err) > 0 {
					require.ErrorContains(t, err, c.err)
				}
				rst, err := certificates.Get(tm.cdb, c.lid)
				require.NoError(t, err)
				require.Len(t, rst, len(c.expect))
				for i := range c.expect {
					require.Equal(t, c.expect[i], rst[i])
				}
			}
		})
	}
}
