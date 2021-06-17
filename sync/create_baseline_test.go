package sync

import (
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"math"
	"testing"

	//"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	//"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
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

	goldenATXID := types.ATXID(types.HexToHash32("77777"))

	id := Path
	lg := log.NewDefault(id)
	mshdb, _ := mesh.NewPersistentMeshDB(id, 5, lg.WithOptions(log.Nop))
	nipstStore, _ := database.NewLDBDatabase(id+"nipst", 0, 0, lg.WithName("nipstDbStore").WithOptions(log.Nop))
	defer nipstStore.Close()
	atxdbStore, _ := database.NewLDBDatabase(id+"atx", 0, 0, lg.WithOptions(log.Nop))
	defer atxdbStore.Close()
	atxdb := activation.NewDB(atxdbStore, &mockIStore{}, mshdb, uint16(1000), goldenATXID, &validatorMock{}, lg.WithName("atxDB").WithOptions(log.Nop))
	trtl := tortoise.NewVerifyingTortoise(tortoise.Config{LayerSyze: blocksPerLayer, Database: mshdb, Hdist: 1, Log: lg.WithName("trtl")})
	msh := mesh.NewMesh(mshdb, atxdb, rewardConf, trtl, &mockTxMemPool{}, &mockState{}, lg.WithOptions(log.Nop))
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
	createBaseline(msh, numOfLayers, blocksPerLayer, blocksPerLayer, txPerBlock, atxPerBlock)
}

func txs(num int) ([]*types.Transaction, []types.TransactionID) {
	txs := make([]*types.Transaction, 0, num)
	ids := make([]types.TransactionID, 0, num)
	for i := 0; i < num; i++ {
		tx := tx()
		txs = append(txs, tx)
		ids = append(ids, tx.ID())
	}
	return txs, ids
}

func atxs(num int) ([]*types.ActivationTx, []types.ATXID) {
	atxs := make([]*types.ActivationTx, 0, num)
	ids := make([]types.ATXID, 0, num)
	signer := signing.NewEdSigner()
	for i := 0; i < num; i++ {
		atx := atxWithProof(signer.PublicKey().String(), proof)
		err := activation.SignAtx(signer, atx)
		if err != nil {
			panic(err)
		}
		atxs = append(atxs, atx)
		ids = append(ids, atx.ID())
	}
	return atxs, ids
}

func createBaseline(msh *mesh.Mesh, layers int, layerSize int, patternSize int, txPerBlock int, atxPerBlock int) {
	lg := log.NewDefault("create_baseline")
	l1 := mesh.GenesisLayer()
	msh.AddBlockWithTxs(l1.Blocks()[0])
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
		bl := types.NewExistingBlock(index, []byte(rand.String(8)), nil)
		signer := signing.NewEdSigner()
		bl.Signature = signer.Sign(bl.Bytes())
		layerBlocks = append(layerBlocks, bl.ID())
		votes := make(map[types.BlockID]struct{})
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.ForDiff = append(bl.ForDiff, (b.ID()))
				votes[b.ID()] = struct{}{}
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			if _, ok := votes[prevBloc.ID()]; !ok {
				bl.AgainstDiff = append(bl.AgainstDiff, prevBloc.ID())
			}
		}

		//add txs
		_, txids := txs(txPerBlock)

		//add atxs
		atxs, _ := atxs(atxPerBlock)
		msh.AtxDB.ProcessAtxs(atxs)

		bl.TxIDs = txids
		//bl.ATXIDs = atxids
		bl.Initialize()
		start := time.Now()
		msh.AddBlockWithTxs(bl)
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

	atx := newActivationTx(types.NodeID{Key: pubkey, VRFPublicKey: []byte(rand.String(8))}, 0, *types.EmptyATXID, 5, 1, goldenATXID, coinbase, 0, []types.BlockID{}, npst)
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = commitment.MerkleRoot
	atx.CalcAndSetID()
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

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, activeSetSize uint32, view []types.BlockID,
	nipst *types.NIPST) *types.ActivationTx {

	nipstChallenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipstChallenge, coinbase, nipst, 0, nil)
}
