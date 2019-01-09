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

	alg := NewAlgorithm(uint32(layerSize), uint32(cachedLayers))
	l := createGenesisLayer()
	alg.HandleIncomingLayer(l)
	for i := 0; i < 11-1; i++ {
		lyr := createFullPointingLayer(l, layerSize)
		start := time.Now()
		alg.HandleIncomingLayer(lyr)
		log.Info("Time to process layer: %v ", time.Since(start))
		l = lyr
	}
}

func createGenesisLayer() *mesh.Layer {
	log.Info("Creating genesis")
	ts := time.Now()
	coin := false
	data := []byte("genesis")

	bl := mesh.NewBlock(coin, data, ts, 0)
	l := mesh.NewLayer(0)

	l.AddBlock(bl)

	return l
}

func createFullPointingLayer(prev *mesh.Layer, blocksInLayer int) *mesh.Layer {
	ts := time.Now()
	coin := false
	// just some random Data
	data := []byte(crypto.UUIDString())
	l := mesh.NewLayer(prev.Index() +1 )
	for i := 0; i < blocksInLayer; i++ {
		bl := mesh.NewBlock(coin, data, ts, 1)

		for _, prevBloc := range prev.Blocks() {
			bl.AddVote(mesh.BlockID(prevBloc.Id))
		}
		l.AddBlock(bl)
	}
	log.Info("Created layer Id %v", l.Index())
	return l
}
