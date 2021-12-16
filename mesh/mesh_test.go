package mesh

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

type ContextualValidityMock struct{}

func (m *ContextualValidityMock) Has([]byte) (bool, error) {
	return true, nil
}

func (m *ContextualValidityMock) NewBatch() database.Batch {
	panic("implement me")
}

func (m *ContextualValidityMock) Find([]byte) database.Iterator {
	panic("implement me")
}

func (m *ContextualValidityMock) Put([]byte, []byte) error {
	return nil
}

func (m *ContextualValidityMock) Get([]byte) (value []byte, err error) {
	return constTrue, nil
}

func (m *ContextualValidityMock) Delete([]byte) error {
	return nil
}

func (m *ContextualValidityMock) Close() {
}

type MeshValidatorMock struct {
	mdb          *DB
	lastComplete types.LayerID
}

func (m *MeshValidatorMock) Persist(context.Context) error {
	return nil
}

func (m *MeshValidatorMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *MeshValidatorMock) HandleIncomingLayer(_ context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	gLyr := types.GetEffectiveGenesis()
	if !layerID.After(gLyr) {
		return gLyr, gLyr, false
	}
	oldVerified := layerID.Sub(2)
	if oldVerified.Before(gLyr) {
		oldVerified = gLyr
	}
	newVerified := layerID.Sub(1)
	return oldVerified, newVerified, false
}

type MockState struct{}

func (MockState) AddTxToPool(*types.Transaction) error {
	return nil
}

func (MockState) Rewind(types.LayerID) (types.Hash32, error) {
	panic("implement me")
}

func (MockState) GetStateRoot() types.Hash32 {
	return [32]byte{}
}

func (MockState) ValidateNonceAndBalance(*types.Transaction) error {
	panic("implement me")
}

func (MockState) GetLayerApplied(types.TransactionID) *types.LayerID {
	panic("implement me")
}

func (s *MockState) ApplyLayer(l types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
	return make([]*types.Transaction, 0), nil
}

func (MockState) AddressExists(types.Address) bool {
	return true
}

func (MockState) GetAllAccounts() (*types.MultipleAccountsState, error) {
	panic("implement me")
}

func (MockState) GetLayerStateRoot(types.LayerID) (types.Hash32, error) {
	panic("implement me")
}

func (MockState) GetBalance(types.Address) uint64 {
	panic("implement me")
}

func (MockState) GetNonce(types.Address) uint64 {
	panic("implement me")
}

type MockTxMemPool struct {
	db map[types.TransactionID]*types.Transaction
}

func newMockTxMemPool() *MockTxMemPool {
	return &MockTxMemPool{
		db: make(map[types.TransactionID]*types.Transaction),
	}
}

func (m *MockTxMemPool) Get(ID types.TransactionID) (*types.Transaction, error) {
	return m.db[ID], nil
}

func (m *MockTxMemPool) Put(ID types.TransactionID, t *types.Transaction) {
	m.db[ID] = t
}

func (MockTxMemPool) Invalidate(types.TransactionID) {
}

func getMesh(tb testing.TB, id string) *Mesh {
	lg := logtest.New(tb).WithName(id)
	mmdb := NewMemMeshDB(lg)
	ctrl := gomock.NewController(tb)
	mockFetch := mocks.NewMockBlockFetcher(ctrl)
	mockFetch.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).AnyTimes()
	return NewMesh(mmdb, NewAtxDbMock(), ConfigTst(), mockFetch, &MeshValidatorMock{mdb: mmdb}, newMockTxMemPool(), &MockState{}, lg)
}

func addLayer(r *require.Assertions, id types.LayerID, layerSize int, msh *Mesh) *types.Layer {
	if layerSize == 0 {
		r.NoError(msh.SetZeroBlockLayer(id))
	} else {
		for i := 0; i < layerSize; i++ {
			txIDs, _ := addManyTXsToPool(r, msh, 4)
			b := types.GenLayerBlock(id, txIDs)
			b.Initialize()
			r.NoError(msh.AddProposalWithTxs(context.TODO(), (*types.Proposal)(b)), "cannot add data to test")
			r.NoError(msh.SaveContextualValidity(b.ID(), id, true))
		}
	}
	l, err := msh.GetLayer(id)
	r.NoError(err, "cant get a layer we've just created")
	return l
}

func TestMesh_LayerBlocksSortedByIDs(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "blockIDs")
	t.Cleanup(func() {
		msh.Close()
	})
	lyr := addLayer(r, types.GetEffectiveGenesis().Add(19), 100, msh)
	blockIDs := types.ToProposalIDs(lyr.Proposals())
	outOfSort := blockIDs[1:]
	outOfSort = append(outOfSort, blockIDs[0])
	sorted := types.SortProposalIDs(blockIDs)
	assert.Equal(t, sorted, blockIDs)
	assert.NotEqual(t, sorted, outOfSort)
}

func TestMesh_LayerHash(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "layerHash")
	t.Cleanup(func() {
		msh.Close()
	})

	gLyr := types.GetEffectiveGenesis()
	latestLyr := gLyr.Add(5)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		numBlocks := 10 * i.Uint32()
		lyr := addLayer(r, i, int(numBlocks), msh)
		// make the first block of each layer invalid
		msh.SaveContextualValidity(lyr.Blocks()[0].ID(), i, false)
	}

	for i := types.NewLayerID(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := msh.GetLayer(i)
		r.NoError(err)
		if !i.After(gLyr) {
			var expHash types.Hash32
			if i == gLyr {
				expHash = types.CalcBlocksHash32(types.GenesisLayer().BlocksIDs(), nil)
			} else {
				expHash = types.EmptyLayerHash
			}
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			assert.Equal(t, expHash, msh.GetLayerHash(i))
			msh.ValidateLayer(context.TODO(), thisLyr)
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			assert.Equal(t, expHash, msh.GetLayerHash(i))
		} else {
			// when tortoise verify a layer N, it can only determine blocks' contextual validity for layer N-1
			prevLyr, err := msh.GetLayer(i.Sub(1))
			r.NoError(err)
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			assert.Equal(t, prevLyr.Hash(), msh.GetLayerHash(i.Sub(1)))
			msh.ValidateLayer(context.TODO(), thisLyr)
			// for this layer, hash is unchanged because the block contextual validity is not determined yet
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			if prevLyr.Index().After(gLyr) {
				// but for previous layer hash should already be changed to contain only valid proposals
				prevExpHash := types.CalcBlocksHash32(types.SortBlockIDs(prevLyr.BlocksIDs()[1:]), nil)
				assert.Equal(t, prevExpHash, msh.GetLayerHash(i.Sub(1)))
				assert.NotEqual(t, prevLyr.Hash(), msh.GetLayerHash(i.Sub(1)))
			}
		}
	}
}

func TestMesh_GetAggregatedLayerHash(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "aggLayerHash")
	t.Cleanup(func() {
		msh.Close()
	})

	gLyr := types.GetEffectiveGenesis()
	latestLyr := gLyr.Add(5)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		numBlocks := 10 * i.Uint32()
		lyr := addLayer(r, i, int(numBlocks), msh)
		// make the first block of each layer invalid
		bid := types.BlockID(lyr.Proposals()[0].ID())
		msh.SaveContextualValidity(bid, i, false)
	}

	prevAggHash := types.EmptyLayerHash
	var expHash types.Hash32
	for i := types.NewLayerID(1); !i.After(gLyr); i = i.Add(1) {
		thisLyr, err := msh.GetLayer(i)
		r.NoError(err)
		if i == gLyr {
			expHash = types.CalcProposalsHash32(types.SortProposalIDs(types.ToProposalIDs(thisLyr.Proposals())), prevAggHash.Bytes())
		} else {
			expHash = types.CalcProposalsHash32([]types.ProposalID{}, prevAggHash.Bytes())
		}
		assert.Equal(t, expHash, msh.GetAggregatedLayerHash(i))
		msh.ValidateLayer(context.TODO(), thisLyr)
		assert.Equal(t, expHash, msh.GetAggregatedLayerHash(i))
		prevAggHash = expHash
	}

	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := msh.GetLayer(i)
		r.NoError(err)
		prevLyr, err := msh.GetLayer(i.Sub(1))
		r.NoError(err)
		if prevLyr.Index() == gLyr {
			r.Equal(prevAggHash, msh.GetAggregatedLayerHash(i.Sub(1)))
			r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
			msh.ValidateLayer(context.TODO(), thisLyr)
			r.Equal(prevAggHash, msh.GetAggregatedLayerHash(i.Sub(1)))
			r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
			continue
		}
		r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i.Sub(1)))
		r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
		msh.ValidateLayer(context.TODO(), thisLyr)
		// contextual validity is still not determined for thisLyr, so aggregated hash is not calculated for this layer
		r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
		// but for previous layer hash should already be changed to contain only valid proposals
		expHash = types.CalcProposalsHash32(types.SortProposalIDs(types.ToProposalIDs(prevLyr.Proposals()[1:])), prevAggHash.Bytes())
		assert.Equal(t, expHash, msh.GetAggregatedLayerHash(prevLyr.Index()))
		prevAggHash = expHash
	}
}

func TestMesh_SetZeroBlockLayer(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "zero block layer")
	t.Cleanup(func() {
		msh.Close()
	})

	lyrID := types.GetEffectiveGenesis().Add(1)
	lyr, err := msh.GetLayer(lyrID)
	r.ErrorIs(err, database.ErrNotFound)
	r.Nil(lyr)
	err = msh.SetZeroBlockLayer(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, types.EmptyLayerHash, msh.GetLayerHash(lyrID))

	// it's ok to add to an empty layer
	lyr = addLayer(r, lyrID, 10, msh)
	msh.ValidateLayer(context.TODO(), lyr)
	lyr, err = msh.GetLayer(lyrID)
	r.NoError(err)
	assert.Equal(t, 10, len(lyr.Proposals()))

	// but not okay to set a non-empty layer to an empty layer
	err = msh.SetZeroBlockLayer(lyrID)
	assert.Equal(t, errLayerHasBlock, err)
}

func TestMesh_AddLayerGetLayer(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "t2")
	t.Cleanup(func() {
		msh.Close()
	})

	id := types.GetEffectiveGenesis().Add(1)
	_, err := msh.GetLayer(id)
	r.ErrorIs(err, database.ErrNotFound)

	txIDs1, _ := addManyTXsToPool(r, msh, 4)
	txIDs2, _ := addManyTXsToPool(r, msh, 3)
	txIDs3, _ := addManyTXsToPool(r, msh, 6)
	r.NoError(msh.AddProposalWithTxs(context.TODO(), types.GenLayerProposal(id, txIDs1)))
	r.NoError(msh.AddProposalWithTxs(context.TODO(), types.GenLayerProposal(id, txIDs2)))
	r.NoError(msh.AddProposalWithTxs(context.TODO(), types.GenLayerProposal(id, txIDs3)))

	lyr, err := msh.GetLayer(id)
	r.NoError(err)
	r.Equal(id, lyr.Index())
	r.Equal(3, len(lyr.Proposals()))
}

func TestMesh_ProcessedLayer(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "processed layer")
	t.Cleanup(func() {
		msh.Close()
	})

	// test genesis layers
	gLyr := types.GetEffectiveGenesis()
	var lyrs []*types.Layer
	for i := types.NewLayerID(1); i.Before(gLyr); i = i.Add(1) {
		lyr := addLayer(r, i, 0, msh)
		lyrs = append(lyrs, lyr)
	}
	lyr, err := msh.GetLayer(gLyr)
	r.NoError(err)
	lyrs = append(lyrs, lyr)
	for _, lyr := range lyrs {
		msh.ValidateLayer(context.TODO(), lyr)
		assert.Equal(t, lyr.Index(), msh.ProcessedLayer())
		// make sure processed layer is persisted
		pLyr, err := msh.GetProcessedLayer()
		assert.NoError(t, err)
		assert.Equal(t, lyr.Index(), pLyr)
	}

	gPlus1 := addLayer(r, gLyr.Add(1), 1, msh)
	gPlus2 := addLayer(r, gLyr.Add(2), 2, msh)
	gPlus3 := addLayer(r, gLyr.Add(3), 3, msh)
	gPlus4 := addLayer(r, gLyr.Add(4), 4, msh)
	gPlus5 := addLayer(r, gLyr.Add(5), 5, msh)

	msh.ValidateLayer(context.TODO(), gPlus1)
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err := msh.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	// set gPlus3 and gPlus5 out of order
	msh.ValidateLayer(context.TODO(), gPlus3)
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	pLyr, err = msh.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	msh.ValidateLayer(context.TODO(), gPlus5)
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	pLyr, err = msh.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	// setting gPlus2 will bring the processed layer to gPlus3
	msh.ValidateLayer(context.TODO(), gPlus2)
	assert.Equal(t, gPlus3.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = msh.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus3.Index(), pLyr)

	// setting gPlus4 will bring the processed layer to gPlus5
	msh.ValidateLayer(context.TODO(), gPlus4)
	assert.Equal(t, gPlus5.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = msh.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus5.Index(), pLyr)

	// setting it to an older layer should have no effect
	msh.ValidateLayer(context.TODO(), gPlus2)
	assert.Equal(t, gPlus5.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = msh.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus5.Index(), pLyr)
}

func TestMesh_PersistProcessedLayer(t *testing.T) {
	msh := getMesh(t, "persist_processed_layer")
	t.Cleanup(func() {
		msh.Close()
	})
	layerID := types.NewLayerID(3)
	assert.NoError(t, msh.persistProcessedLayer(layerID))
	rLyr, err := msh.GetProcessedLayer()
	assert.NoError(t, err)
	assert.Equal(t, layerID, rLyr)
}

func TestMesh_LatestKnownLayer(t *testing.T) {
	msh := getMesh(t, "t6")
	t.Cleanup(func() {
		msh.Close()
	})
	msh.setLatestLayer(types.NewLayerID(3))
	msh.setLatestLayer(types.NewLayerID(7))
	msh.setLatestLayer(types.NewLayerID(10))
	msh.setLatestLayer(types.NewLayerID(1))
	msh.setLatestLayer(types.NewLayerID(2))
	assert.Equal(t, types.NewLayerID(10), msh.LatestLayer(), "wrong layer")
}

func TestMesh_WakeUp(t *testing.T) {
	msh := getMesh(t, "t1")
	t.Cleanup(func() {
		msh.Close()
	})

	gLyr := types.GetEffectiveGenesis()
	r := require.New(t)
	txIDs1, _ := addManyTXsToPool(r, msh, 4)
	txIDs2, _ := addManyTXsToPool(r, msh, 3)
	block1 := types.GenLayerProposal(gLyr.Add(1), txIDs1)
	block2 := types.GenLayerProposal(gLyr.Add(2), txIDs2)

	assert.NoError(t, msh.AddProposalWithTxs(context.TODO(), block1))
	assert.NoError(t, msh.AddProposalWithTxs(context.TODO(), block2))

	rBlock2, err := msh.getProposal(block2.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs2), len(rBlock2.TxIDs), "block TX size was wrong")

	rBlock1, err := msh.getProposal(block1.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs1), len(rBlock1.TxIDs), "block TX size was wrong")

	ctrl := gomock.NewController(t)
	recoveredMesh := NewMesh(msh.DB, NewAtxDbMock(), ConfigTst(), mocks.NewMockBlockFetcher(ctrl), &MeshValidatorMock{mdb: msh.DB}, newMockTxMemPool(), &MockState{}, logtest.New(t))

	rBlock2, err = recoveredMesh.getProposal(block2.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs2), len(rBlock2.TxIDs), "block TX size was wrong")

	rBlock1, err = recoveredMesh.getProposal(block1.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs1), len(rBlock1.TxIDs), "block TX size was wrong")
}

func TestMesh_AddBlockWithTxs_PushTransactions_UpdateUnappliedTxs(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "mesh")

	state := &MockMapState{}
	msh.state = state

	layerID := types.GetEffectiveGenesis().Add(1)
	signer, origin := newSignerAndAddress(r, "origin")
	tx1 := addTxToMesh(r, msh, signer, 2468)
	tx2 := addTxToMesh(r, msh, signer, 2469)
	tx3 := addTxToMesh(r, msh, signer, 2470)
	tx4 := addTxToMesh(r, msh, signer, 2471)
	tx5 := addTxToMesh(r, msh, signer, 2472)
	addBlockAndTxsToMesh(r, msh, layerID, true, tx1, tx2)
	addBlockAndTxsToMesh(r, msh, layerID, true, tx2, tx3, tx4)
	addBlockAndTxsToMesh(r, msh, layerID, false, tx4, tx5)
	addBlockAndTxsToMesh(r, msh, layerID, false, tx5)

	txns := getTxns(r, msh.DB, origin)
	r.Len(txns, 5)
	for i := 0; i < 5; i++ {
		r.Equal(2468+i, int(txns[i].Nonce))
		r.Equal(111, int(txns[i].TotalAmount))
	}

	msh.pushLayersToState(context.TODO(), types.GetEffectiveGenesis(), types.GetEffectiveGenesis().Add(1))
	r.Equal(4, len(state.Txs))

	r.ElementsMatch(GetTransactionIds(tx5), GetTransactionIds(state.Pool...))

	txns = getTxns(r, msh.DB, origin)
	r.Empty(txns)
}

func TestMesh_ExtractUniqueOrderedTransactions(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "t2")
	t.Cleanup(func() {
		msh.Close()
	})
	layerID := types.NewLayerID(1)
	signer, _ := newSignerAndAddress(r, "origin")
	tx1 := addTxToMesh(r, msh, signer, 2468)
	tx2 := addTxToMesh(r, msh, signer, 2469)
	tx3 := addTxToMesh(r, msh, signer, 2470)
	tx4 := addTxToMesh(r, msh, signer, 2471)
	addBlockAndTxsToMesh(r, msh, layerID, true, tx1, tx2)
	addBlockAndTxsToMesh(r, msh, layerID, true, tx2, tx3, tx4)
	addBlockAndTxsToMesh(r, msh, layerID, false, tx4)
	l, err := msh.GetLayer(layerID)
	r.NoError(err)

	validBlocks := msh.extractUniqueOrderedTransactions(l)

	r.ElementsMatch(GetTransactionIds(tx1, tx2, tx3, tx4), GetTransactionIds(validBlocks...))
}

func randomHash() (hash types.Hash32) {
	hash.SetBytes([]byte(rand.String(32)))
	return
}

func TestMesh_persistLayerHash(t *testing.T) {
	msh := getMesh(t, "persistLayerHash")
	t.Cleanup(func() {
		msh.Close()
	})

	// persist once
	lyr := types.NewLayerID(3)
	hash := randomHash()
	msh.persistLayerHash(lyr, hash)
	assert.Equal(t, hash, msh.GetLayerHash(lyr))

	// persist twice
	newHash := randomHash()
	assert.NotEqual(t, newHash, hash)
	msh.persistLayerHash(lyr, newHash)
	assert.Equal(t, newHash, msh.GetLayerHash(lyr))
}

func GetTransactionIds(txs ...*types.Transaction) []types.TransactionID {
	var res []types.TransactionID
	for _, tx := range txs {
		res = append(res, tx.ID())
	}
	return res
}

func addTxToMesh(r *require.Assertions, msh *Mesh, signer *signing.EdSigner, nonce uint64) *types.Transaction {
	tx1 := newTx(r, signer, nonce, 111)
	p := &types.Proposal{}
	p.LayerIndex = types.NewLayerID(1)
	err := msh.writeTransactions(p, tx1)
	r.NoError(err)
	return tx1
}

func addBlockAndTxsToMesh(r *require.Assertions, msh *Mesh, id types.LayerID, valid bool, txs ...*types.Transaction) *types.Block {
	txIDs := make([]types.TransactionID, 0, len(txs))
	for _, tx := range txs {
		msh.txPool.Put(tx.ID(), tx)
		txIDs = append(txIDs, tx.ID())
	}
	b := types.GenLayerBlock(id, txIDs)
	b.Initialize()
	r.NoError(msh.SaveContextualValidity(b.ID(), id, valid))
	r.NoError(msh.AddProposalWithTxs(context.TODO(), (*types.Proposal)(b)))
	return b
}

func addManyTXsToPool(r *require.Assertions, msh *Mesh, numOfTxs int) ([]types.TransactionID, []*types.Transaction) {
	txs := make([]*types.Transaction, numOfTxs)
	txIDs := make([]types.TransactionID, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		tx, err := types.NewSignedTx(1, types.HexToAddress("1"), 10, 100, rand.Uint64(), signing.NewEdSigner())
		r.NoError(err)
		txs[i] = tx
		txIDs[i] = tx.ID()
		msh.txPool.Put(tx.ID(), tx)
	}
	return txIDs, txs
}

func TestMesh_AddBlockWithTxs(t *testing.T) {
	r := require.New(t)
	mesh := getMesh(t, "AddProposalWithTxs")

	numTXs := 6
	txIDs, _ := addManyTXsToPool(r, mesh, numTXs)

	block := types.GenLayerProposal(types.NewLayerID(1), txIDs)
	r.NoError(mesh.AddProposalWithTxs(context.TODO(), block))

	res, err := mesh.getProposal(block.ID())
	assert.NoError(t, err)
	assert.Equal(t, numTXs, len(res.TxIDs), "block TX size was wrong")
}

func TestMesh_HandleValidatedLayer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFetch := mocks.NewMockBlockFetcher(ctrl)
	numOfBlocks := 10
	maxTxs := 20
	s := &MockMapState{Rewards: make(map[types.Address]uint64)}
	msh, atxDB := getMeshWithMapState(t, "t1", s)
	defer msh.Close()
	msh.fetch = mockFetch

	gLyr := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); i.Before(gLyr); i = i.Add(1) {
		msh.HandleValidatedLayer(context.TODO(), i, []types.BlockID{})
		require.Equal(t, i, msh.ProcessedLayer())
	}
	msh.HandleValidatedLayer(context.TODO(), gLyr, types.GenesisLayer().BlocksIDs())
	require.Equal(t, gLyr, msh.ProcessedLayer())

	lyr := gLyr.Add(1)
	_, blocks := createLayer(t, msh, lyr, numOfBlocks, maxTxs, atxDB)
	mockFetch.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Do(
		func(ctx context.Context, blockIDs []types.BlockID) {
			assert.Equal(t, types.SortBlockIDs(blockIDs), types.SortBlockIDs(types.BlockIDs(blocks)))
		}).Times(1)
	msh.HandleValidatedLayer(context.TODO(), lyr, types.BlockIDs(blocks))
	require.Equal(t, lyr, msh.ProcessedLayer())
}

func TestMesh_HandleValidatedLayer_emptyOutputFromHare(t *testing.T) {
	msh := getMesh(t, "HandleValidatedLayer_Empty")
	layerID := types.GetEffectiveGenesis().Add(1)

	createMeshFromHareOutput(t, layerID, msh, NewAtxDbMock())
	require.Equal(t, layerID, msh.ProcessedLayer())

	var empty []types.BlockID
	layerID = layerID.Add(1)
	msh.HandleValidatedLayer(context.TODO(), layerID, empty)

	// input vector saved
	iv, err := msh.GetLayerInputVectorByID(layerID)
	require.NoError(t, err)
	assert.Nil(t, iv)

	// but processed layer has advanced
	assert.Equal(t, layerID, msh.ProcessedLayer())
}
