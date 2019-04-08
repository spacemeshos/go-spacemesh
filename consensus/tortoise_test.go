package consensus

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"testing"
	"time"
)

func TestAlgorithm_Sanity(t *testing.T) {
	layerSize := 50
	cachedLayers := 100

	alg := NewTortoise(uint32(layerSize), uint32(cachedLayers))
	l := mesh.GenesisLayer()
	alg.HandleIncomingLayer(l)
	alg.RegisterLayerCallback(func(id block.LayerID) {})
	for i := 0; i < 11-1; i++ {
		lyr := createFullPointingLayer(l, layerSize)
		start := time.Now()
		alg.HandleIncomingLayer(lyr)
		log.Info("Time to process layer: %v ", time.Since(start))
		l = lyr
	}
}

func createFullPointingLayer(prev *block.Layer, blocksInLayer int) *block.Layer {
	l := block.NewLayer(prev.Index() + 1)
	for i := 0; i < blocksInLayer; i++ {
		bl := block.NewExistingBlock(block.BlockID(uuid.New().ID()), l.Index(), []byte("data1"))
		for _, prevBloc := range prev.Blocks() {
			bl.AddVote(block.BlockID(prevBloc.Id))
		}
		l.AddBlock(bl)
	}
	log.Info("Created block.LayerID %v", l.Index())
	return l
}
