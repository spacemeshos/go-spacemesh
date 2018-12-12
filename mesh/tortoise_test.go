package mesh

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
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

func createGenesisLayer() *Layer {
	log.Info("Creating genesis")
	ts := time.Now()
	coin := false
	data := []byte("genesis")

	bl := NewBlock(coin, data, ts, 0)
	l := NewLayer()

	l.AddBlock(bl)

	return l
}

func createFullPointingLayer(prev *Layer, blocksInLayer int) *Layer {
	ts := time.Now()
	coin := false
	// just some random Data
	data := []byte(crypto.UUIDString())
	l := NewLayer()
	for i := 0; i < blocksInLayer; i++ {
		bl := NewBlock(coin, data, ts, 1)

		for _, pervBloc := range prev.blocks {
			bl.BlockVotes[pervBloc.Id] = true
		}
		l.AddBlock(bl)
	}
	log.Info("Created layer Id %v", l.index)
	return l
}
