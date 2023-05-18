package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

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

func TestMesh_ProcessLayerPerHareOutput(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		executed bool
	}{
		{
			desc: "no optimistic filtering",
		},
		{
			desc:     "optimistic",
			executed: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			tm := createTestMesh(t)
			tm.mockTortoise.EXPECT().Results(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			gLyr := types.GetEffectiveGenesis()
			gPlus1 := gLyr.Add(1)
			gPlus2 := gLyr.Add(2)
			gPlus3 := gLyr.Add(3)
			gPlus4 := gLyr.Add(4)
			gPlus5 := gLyr.Add(5)
			blocks1 := createLayerBlocks(t, tm.db, tm.Mesh, gPlus1)
			blocks2 := createLayerBlocks(t, tm.db, tm.Mesh, gPlus2)
			blocks3 := createLayerBlocks(t, tm.db, tm.Mesh, gPlus3)
			blocks4 := createLayerBlocks(t, tm.db, tm.Mesh, gPlus4)
			blocks5 := createLayerBlocks(t, tm.db, tm.Mesh, gPlus5)
			layerBlocks := map[types.LayerID]*types.Block{
				gPlus1: blocks1[0],
				gPlus2: blocks2[0],
				gPlus3: blocks3[0],
				gPlus4: blocks4[0],
				gPlus5: blocks5[0],
			}

			for i := gPlus1; !i.After(gPlus5); i = i.Add(1) {
				toApply := layerBlocks[i]
				tm.mockTortoise.EXPECT().OnHareOutput(toApply.LayerIndex, toApply.ID())
				tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), i)
				tm.mockTortoise.EXPECT().Updates().Return(nil)
				if !tc.executed {
					var ineffective []types.Transaction
					var executed []types.TransactionWithResult
					tm.mockVM.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any()).Return(ineffective, executed, nil)
					tm.mockState.EXPECT().UpdateCache(gomock.Any(), i, toApply.ID(), executed, ineffective)
					tm.mockVM.EXPECT().GetStateRoot()
				}
				require.NoError(t, tm.ProcessLayerPerHareOutput(context.Background(), i, toApply.ID(), tc.executed))
				got, err := certificates.GetHareOutput(tm.cdb, i)
				require.NoError(t, err)
				require.Equal(t, toApply.ID(), got)
				require.Equal(t, i, tm.ProcessedLayer())
			}
			checkLastAppliedInDB(t, tm.Mesh, gPlus5)
		})
	}
}

func TestMesh_ProcessLayerPerHareOutput_certificateSynced(t *testing.T) {
	tm := createTestMesh(t)
	tm.mockTortoise.EXPECT().Results(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	lid := types.GetEffectiveGenesis().Add(1)

	require.Greater(t, numBlocks, 1)
	blks := createLayerBlocks(t, tm.db, tm.Mesh, lid)
	cert := &types.Certificate{
		BlockID: blks[0].ID(),
	}
	require.NoError(t, certificates.Add(tm.cdb, lid, cert))

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
	tm.mockTortoise.EXPECT().Updates().Return(nil)
	tm.mockVM.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any())
	tm.mockState.EXPECT().UpdateCache(gomock.Any(), lid, blks[0].ID(), gomock.Any(), gomock.Any())
	tm.mockVM.EXPECT().GetStateRoot()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.Background(), lid, blks[1].ID(), false))
	hareOutput, err := certificates.GetHareOutput(tm.cdb, lid)
	require.NoError(t, err)
	require.Equal(t, blks[0].ID(), hareOutput)
	require.Equal(t, lid, tm.ProcessedLayer())
	checkLastAppliedInDB(t, tm.Mesh, lid)

	certs, err := certificates.Get(tm.cdb, lid)
	require.NoError(t, err)
	require.Len(t, certs, 2)
	require.True(t, certs[0].Valid)
	require.Equal(t, certs[0].Block, blks[0].ID())
	require.NotNil(t, certs[0].Cert)
	require.False(t, certs[1].Valid)
	require.Equal(t, certs[1].Block, blks[1].ID())
	require.Nil(t, certs[1].Cert)
}

func TestMesh_ProcessLayerPerHareOutput_certificateSyncedButInvalid(t *testing.T) {
	tm := createTestMesh(t)
	tm.mockTortoise.EXPECT().Results(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	lid := types.GetEffectiveGenesis().Add(1)

	require.Greater(t, numBlocks, 1)
	blks := createLayerBlocks(t, tm.db, tm.Mesh, lid)
	cert := &types.Certificate{
		BlockID: blks[0].ID(),
	}
	require.NoError(t, certificates.Add(tm.cdb, lid, cert))
	require.NoError(t, certificates.SetInvalid(tm.cdb, lid, blks[0].ID()))

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
	tm.mockTortoise.EXPECT().Updates().Return(nil)
	tm.mockVM.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any())
	tm.mockState.EXPECT().UpdateCache(gomock.Any(), lid, blks[1].ID(), gomock.Any(), gomock.Any())
	tm.mockVM.EXPECT().GetStateRoot()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.Background(), lid, blks[1].ID(), false))
	hareOutput, err := certificates.GetHareOutput(tm.cdb, lid)
	require.NoError(t, err)
	require.Equal(t, blks[1].ID(), hareOutput)
	require.Equal(t, lid, tm.ProcessedLayer())
	checkLastAppliedInDB(t, tm.Mesh, lid)

	certs, err := certificates.Get(tm.cdb, lid)
	require.NoError(t, err)
	require.Len(t, certs, 2)
	require.False(t, certs[0].Valid)
	require.Equal(t, certs[0].Block, blks[0].ID())
	require.NotNil(t, certs[0].Cert)
	require.True(t, certs[1].Valid)
	require.Equal(t, certs[1].Block, blks[1].ID())
	require.Nil(t, certs[1].Cert)
}

func TestMesh_ProcessLayerPerHareOutput_emptyOutput(t *testing.T) {
	tm := createTestMesh(t)
	tm.mockTortoise.EXPECT().Results(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	blocks1 := createLayerBlocks(t, tm.db, tm.Mesh, gPlus1)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus1, blocks1[0].ID())
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus1)
	tm.mockTortoise.EXPECT().Updates().Return(nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.Background(), gPlus1, blocks1[0].ID(), true))
	hareOutput, err := certificates.GetHareOutput(tm.cdb, gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), hareOutput)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	gPlus2 := gLyr.Add(2)
	createLayerBlocks(t, tm.db, tm.Mesh, gPlus2)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus2, types.EmptyBlockID)
	tm.mockVM.EXPECT().Apply(gomock.Any(), nil, nil).Return(nil, nil, nil)
	tm.mockState.EXPECT().UpdateCache(gomock.Any(), gPlus2, types.EmptyBlockID, nil, nil)
	tm.mockVM.EXPECT().GetStateRoot()
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus2)
	tm.mockTortoise.EXPECT().Updates().Return(nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.Background(), gPlus2, types.EmptyBlockID, false))

	// hare output saved
	hareOutput, err = certificates.GetHareOutput(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, hareOutput)

	// but processed layer has advanced
	require.Equal(t, gPlus2, tm.ProcessedLayer())
	require.Equal(t, gPlus2, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus2)
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

func TestMesh_MissingTransactionsFailure(t *testing.T) {
	tm := createTestMesh(t)
	ctx := context.Background()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(1)

	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{LayerIndex: last, TxIDs: []types.TransactionID{{1, 1, 1}}})
	require.NoError(t, blocks.Add(tm.cdb, block))
	require.NoError(t, certificates.SetHareOutput(tm.cdb, last, block.ID()))

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), last)
	tm.mockTortoise.EXPECT().Updates().Return(nil)
	require.ErrorIs(t, tm.ProcessLayer(ctx, last), sql.ErrNotFound)

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, genesis, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, genesis)
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

	layer := func(lid types.LayerID, blocks ...result.Block) result.Layer {
		return result.Layer{
			Layer:  lid,
			Blocks: blocks,
		}
	}
	id := func(name string) types.BlockID {
		id := types.BlockID{}
		copy(id[:], name)
		return id
	}
	validhare := func(id string, data bool) result.Block {
		block := result.Block{}
		copy(block.Header.ID[:], id)
		block.Valid = true
		block.Hare = true
		block.Data = data
		return block
	}
	invalid := func(id string, data bool) result.Block {
		block := result.Block{}
		copy(block.Header.ID[:], id)
		block.Data = data
		block.Invalid = true
		return block
	}
	valid := func(id string, data bool) result.Block {
		block := result.Block{}
		copy(block.Header.ID[:], id)
		block.Valid = true
		block.Data = data
		return block
	}
	hare := func(id string, data bool) result.Block {
		block := result.Block{}
		copy(block.Header.ID[:], id)
		block.Hare = true
		block.Data = data
		return block
	}
	invalidhare := func(id string, data bool) result.Block {
		block := result.Block{}
		copy(block.Header.ID[:], id)
		block.Invalid = true
		block.Hare = true
		block.Data = data
		return block
	}

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
					updates: []result.Layer{
						layer(start, validhare("1", true), invalid("2", true)),
					},
					executed: []types.BlockID{id("1")},
					applied:  []types.BlockID{id("1")},
					validity: map[types.BlockID]bool{
						id("1"): true,
						id("2"): false,
					},
				},
			},
		},
		{
			"missing valid",
			[]call{
				{
					updates: []result.Layer{
						layer(start, valid("1", false)),
					},
					err: "missing",
				},
				{
					results: []result.Layer{
						layer(start, valid("1", true)),
					},
					executed: []types.BlockID{id("1")},
					applied:  []types.BlockID{id("1")},
					validity: map[types.BlockID]bool{
						id("1"): true,
					},
				},
			},
		},
		{
			"missing invalid",
			[]call{
				{
					updates: []result.Layer{
						layer(start, valid("1", false)),
					},
					err: "missing",
				},
				{
					results: []result.Layer{
						layer(start, invalid("1", false)),
					},
					executed: []types.BlockID{{}},
					applied:  []types.BlockID{{}},
				},
			},
		},
		{
			"revert from empty",
			[]call{
				{
					updates: []result.Layer{
						layer(start),
					},
					executed: []types.BlockID{{}},
					applied:  []types.BlockID{{0}},
				},
				{
					updates: []result.Layer{
						layer(start, valid("2", true)),
					},
					executed: []types.BlockID{id("2")},
					applied:  []types.BlockID{id("2")},
				},
			},
		},
		{
			"hare to valid",
			[]call{
				{
					updates: []result.Layer{
						layer(start, hare("1", true)),
					},
					executed: []types.BlockID{id("1")},
					applied:  []types.BlockID{id("1")},
				},
				{
					updates: []result.Layer{
						layer(start, validhare("1", true)),
					},
					applied: []types.BlockID{id("1")},
				},
			},
		},
		{
			"multiple layers of hare",
			[]call{
				{
					updates: []result.Layer{
						layer(start, hare("1", true)),
					},
					executed: []types.BlockID{id("1")},
					applied:  []types.BlockID{id("1")},
				},
				{
					updates: []result.Layer{
						layer(start.Add(1), hare("2", true)),
					},
					executed: []types.BlockID{id("2")},
					applied:  []types.BlockID{id("1"), id("2")},
				},
			},
		},
		{
			"hare to invalid",
			[]call{
				{
					updates: []result.Layer{
						layer(start, hare("1", true)),
					},
					executed: []types.BlockID{id("1")},
					applied:  []types.BlockID{id("1")},
				},
				{
					updates: []result.Layer{
						layer(start, invalidhare("1", true)),
					},
					executed: []types.BlockID{{0}},
					applied:  []types.BlockID{{0}},
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
