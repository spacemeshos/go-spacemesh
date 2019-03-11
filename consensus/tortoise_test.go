package consensus

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"testing"
	"time"
)

func TestAlgorithm_Sanity(t *testing.T) {
	layerSize := 50
	cachedLayers := 100

	alg := NewTortoise(uint32(layerSize), uint32(cachedLayers))
	l := GenesisLayer()
	alg.HandleIncomingLayer(l)
	alg.RegisterLayerCallback(func(id mesh.LayerID) {})
	for i := 0; i < 11-1; i++ {
		lyr := createFullPointingLayer(l, layerSize)
		start := time.Now()
		alg.HandleIncomingLayer(lyr)
		log.Info("Time to process layer: %v ", time.Since(start))
		l = lyr
	}
}

func createFullPointingLayer(prev *mesh.Layer, blocksInLayer int) *mesh.Layer {
	ts := time.Now()
	coin := false
	// just some random Data
	data := []byte(crypto.UUIDString())
	l := mesh.NewLayer(prev.Index() + 1)
	for i := 0; i < blocksInLayer; i++ {
		bl := mesh.NewBlock(coin, data, ts, 1)

		for _, prevBloc := range prev.Blocks() {
			bl.AddVote(mesh.BlockID(prevBloc.Id))
		}
		l.AddBlock(bl)
	}
	log.Info("Created mesh.LayerID %v", l.Index())
	return l
}
