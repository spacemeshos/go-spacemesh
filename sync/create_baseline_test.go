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

type blockBuilderMock struct{}

func (blockBuilderMock) ValidateAndAddTxToPool(tx *types.Transaction) error {
	return nil
}

func TestCreateBaseline(t *testing.T) {
	t.Skip()
	const (
		numOfLayers    = 51  // 201
		blocksPerLayer = 200 // 200
		txPerBlock     = 5   // 50
		atxPerBlock    = 5   // 5
	)

	id := Path
	lg := log.New(id, "", "")
	mshdb, _ := mesh.NewPersistentMeshDB(id, 5, lg.WithOptions(log.Nop))
	nipstStore, _ := database.NewLDBDatabase(id+"nipst", 0, 0, lg.WithName("nipstDbStore").WithOptions(log.Nop))
	defer nipstStore.Close()
	atxdbStore, _ := database.NewLDBDatabase(id+"atx", 0, 0, lg.WithOptions(log.Nop))
	defer atxdbStore.Close()
	atxdb := activation.NewActivationDb(atxdbStore, &MockIStore{}, mshdb, uint16(1000), &ValidatorMock{}, lg.WithName("atxDB").WithOptions(log.Nop))
	trtl := tortoise.NewAlgorithm(blocksPerLayer, mshdb, 1, lg.WithName("trtl"))
	msh := mesh.NewMesh(mshdb, atxdb, rewardConf, trtl, &mockTxMemPool{}, &mockAtxMemPool{}, &MockState{}, lg.WithOptions(log.Nop))
	defer msh.Close()
	msh.SetBlockBuilder(&blockBuilderMock{})
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
	createBaseline(msh, numOfLayers, blocksPerLayer, blocksPerLayer, txPerBlock, atxPerBlock)
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
	signer := signing.NewEdSigner()
	for i := 0; i < num; i++ {
		atx := atxWithProof(signer.PublicKey().String(), proof)
		err := activation.SignAtx(signer, atx)
		if err != nil {
			panic(err)
		}
		atxs = append(atxs, atx)
		ids = append(ids, atx.Id())
	}
	return atxs, ids
}

func createBaseline(msh *mesh.Mesh, layers int, layerSize int, patternSize int, txPerBlock int, atxPerBlock int) {
	lg := log.New("create_baseline", "", "")
	l1 := mesh.GenesisLayer()
	msh.AddBlockWithTxs(l1.Blocks()[0], nil, nil)
	var lyrs []*types.Layer
	lyrs = append(lyrs, l1)
	l := createLayerWithRandVoting(msh, 1, []*types.Layer{l1}, layerSize, 1, txPerBlock, atxPerBlock)

	lyrs = append(lyrs, l)
	for i := 0; i < layers-1; i++ {
		lg.Debug("!!-------------------------new layer %v-------------------------!!!!", i)
		start := time.Now()
		lyr := createLayerWithRandVoting(msh, l.Index()+1, []*types.Layer{l}, layerSize, patternSize, txPerBlock, atxPerBlock)
		lyrs = append(lyrs, lyr)
		lg.Info("Time inserting layer into db: %v ", time.Since(start))
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
				bl.AddVote(b.Id())
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(prevBloc.Id())
		}

		//add txs
		txs, txids := txs(txPerBlock)
		//add atxs
		atxs, atxids := atxs(atxPerBlock)

		bl.TxIds = txids
		bl.AtxIds = atxids
		bl.Initialize()
		start := time.Now()
		msh.AddBlockWithTxs(bl, txs, atxs)
		log.Debug("added block %v", time.Since(start))
		l.AddBlock(bl)

	}
	log.Debug("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func atxWithProof(pubkey string, poetref []byte) *types.ActivationTx {
	coinbase := types.HexToAddress("aaaa")
	chlng := types.HexToHash32("0x3333")
	npst := activation.NewNIPSTWithChallenge(&chlng, poetref)

	atx := types.NewActivationTxForTests(types.NodeId{Key: pubkey, VRFPublicKey: []byte(rand.RandString(8))}, 0, *types.EmptyAtxId, 5, 1, *types.EmptyAtxId, coinbase, 0, []types.BlockID{}, npst)
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
