package sync

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"math"
	"testing"
	"time"
)

var proof []byte

func TestCreateBaseline(t *testing.T) {
	t.Skip()
	id := Path
	layerSize := 200
	lg := log.New(id, "", "")
	mshdb, _ := mesh.NewPersistentMeshDB(id, lg.WithOptions(log.Nop))
	nipstStore, _ := database.NewLDBDatabase(id+"nipst", 0, 0, lg.WithName("nipstDbStore").WithOptions(log.Nop))
	defer nipstStore.Close()
	atxdbStore, _ := database.NewLDBDatabase(id+"atx", 0, 0, lg.WithOptions(log.Nop))
	defer atxdbStore.Close()
	atxdb := activation.NewActivationDb(atxdbStore, &MockIStore{}, mshdb, uint16(1000), &ValidatorMock{}, lg.WithName("atxDB").WithOptions(log.Nop))
	trtl := tortoise.NewAlgorithm(int(layerSize), mshdb, 1, lg.WithName("trtl"))
	msh := mesh.NewMesh(mshdb, atxdb, rewardConf, trtl, &MockTxMemPool{}, &MockAtxMemPool{}, &stateMock{}, lg.WithOptions(log.Nop))
	defer msh.Close()
	poetDbStore, err := database.NewLDBDatabase(id+"poet", 0, 0, lg.WithName("poetDbStore").WithOptions(log.Nop))
	if err != nil {
		lg.Error("error: ", err)
		return
	}

	poetDb := activation.NewPoetDb(poetDbStore, lg.WithName("poetDb").WithOptions(log.Nop))
	proofMessage := makePoetProofMessage(t)
	proof, _ = proofMessage.Ref()
	if err := poetDb.ValidateAndStore(&proofMessage); err != nil {
		t.Error(err)
	}

	poetDb.ValidateAndStore(&proofMessage)

	lg.Info("start creating baseline")
	createBaseline(msh, 201, layerSize, layerSize, 50, 5)

	i := 1
	for ; ; i++ {
		if lyr, err2 := msh.GetLayer(types.LayerID(i)); err2 != nil || lyr == nil {
			lg.Info("loaded %v layers from disk %v", i-1, err2)
			break
		} else {
			lg.Info("loaded layer %v from disk ", i)
			msh.ValidateLayer(lyr)
		}
	}
}

func txs(num int) ([]*types.Transaction, []types.TransactionId) {
	txs := make([]*types.Transaction, 0, num)
	ids := make([]types.TransactionId, 0, num)
	for i := 0; i < num; i++ {
		tx := tx()
		txs = append(txs, tx)
		ids = append(ids, tx.Id())
	}
	return txs, ids
}

func atxs(num int) ([]*types.ActivationTx, []types.AtxId) {
	atxs := make([]*types.ActivationTx, 0, num)
	ids := make([]types.AtxId, 0, num)
	for i := 0; i < num; i++ {
		atx := atxWithProof(rand.RandString(8), proof)
		atxs = append(atxs, atx)
		ids = append(ids, atx.Id())
	}
	return atxs, ids
}

func createBaseline(msh *mesh.Mesh, layers int, layerSize int, patternSize int, txPerBlock int, atxPerBlock int) {
	lg := log.New("tortoise_test", "", "")
	l1 := mesh.GenesisLayer()
	msh.AddBlockWithTxs(l1.Blocks()[0], nil, nil)
	var lyrs []*types.Layer
	lyrs = append(lyrs, l1)
	l := createLayerWithRandVoting(msh, 1, []*types.Layer{l1}, layerSize, 1, txPerBlock, atxPerBlock)

	lyrs = append(lyrs, l)
	for i := 0; i < layers-1; i++ {
		lg.Info("!!-------------------------new layer %v-------------------------!!!!", i)
		start := time.Now()
		lyr := createLayerWithRandVoting(msh, l.Index()+1, []*types.Layer{l}, layerSize, patternSize, txPerBlock, atxPerBlock)
		lyrs = append(lyrs, lyr)
		lg.Debug("Time inserting layer into db: %v ", time.Since(start))
		l = lyr
	}

}

func createLayerWithRandVoting(msh *mesh.Mesh, index types.LayerID, prev []*types.Layer, blocksInLayer int, patternSize int, txPerBlock int, atxPerBlock int) *types.Layer {
	l := types.NewLayer(index)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize)))))
	}
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := types.NewExistingBlock(index, []byte(rand.RandString(8)))
		signer := signing.NewEdSigner()
		bl.Signature = signer.Sign(bl.Bytes())
		layerBlocks = append(layerBlocks, bl.Id())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.Id()))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(types.BlockID(prevBloc.Id()))
		}

		//add txs
		txs, txids := txs(txPerBlock)
		//add atxs
		atxs, atxids := atxs(atxPerBlock)

		bl.TxIds = txids
		bl.AtxIds = atxids

		start := time.Now()
		msh.AddBlockWithTxs(bl, txs, atxs)
		log.Info("added block %v", time.Since(start))
		l.AddBlock(bl)

	}
	log.Debug("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func atxWithProof(pubkey string, poetref []byte) *types.ActivationTx {
	coinbase := types.HexToAddress("aaaa")
	chlng := types.HexToHash32("0x3333")
	npst := activation.NewNIPSTWithChallenge(&chlng, poetref)

	atx := types.NewActivationTx(types.NodeId{Key: pubkey, VRFPublicKey: []byte(rand.RandString(8))}, coinbase, 0, *types.EmptyAtxId, 5, 1, *types.EmptyAtxId, 0, []types.BlockID{}, npst)
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = commitment.MerkleRoot
	atx.CalcAndSetId()
	return atx
}

func chooseRandomPattern(blocksInLayer int, patternSize int) []int {
	rand.Seed(time.Now().UnixNano())
	p := rand.Perm(blocksInLayer)
	indexes := make([]int, 0, patternSize)
	for _, r := range p[:patternSize] {
		indexes = append(indexes, r)
	}
	return indexes
}
