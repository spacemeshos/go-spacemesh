package core

import (
	"testing"
	"time"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
)

func TestAlgorithm_Sanity(t *testing.T) {
	layerSize := 50
	cachedLayers := 100

	alg := NewAlgorithm(uint32(layerSize),uint32(cachedLayers))
	l := createGenesisLayer()
	alg.HandleIncomingLayer(l)
	for i:=0; i<cachedLayers-1; i++ {
		lyr := createFullPointingLayer(&l,layerSize)
		alg.HandleIncomingLayer(lyr)
		l = lyr
	}
}

func createGenesisLayer() Layer{
	log.Info("Creating genesis")
	ts := time.Now()
	coin := false
	data := []byte("genesis")

	bl := NewBlock(coin,data,ts)

	l := NewLayer()

	l.AddBlock(bl)

	return l
}


func createFullPointingLayer(prev *Layer, blocksInLayer int) Layer{
	ts := time.Now()
	coin := false
	// just some random data
	data := []byte(crypto.UUIDString())
	l := NewLayer()
	for i := 0; i< blocksInLayer; i++ {
		bl := NewBlock(coin,data,ts)

		for _, pervBloc := range prev.blocks{
			bl.blockVotes[pervBloc.id] = true
		}
		l.AddBlock(bl)
	}
	log.Info("Created layer id %v", l.layerNum)
	return l
}
