package sync

import (
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLayers_AddLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	idx := 0
	layers.AddLayer(mesh.NewExistingLayer(uint32(idx), make([]*mesh.Block, 10)))
	assert.True(t, layers.LocalLayerCount() == 1, "wrong layer count")
	_, err := layers.GetLayer(idx)
	assert.True(t, err == nil, "error: ", err)
}

func TestLayers_AddWrongLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 10)))
	layers.AddLayer(mesh.NewExistingLayer(uint32(3), make([]*mesh.Block, 10)))
	layers.AddLayer(mesh.NewExistingLayer(uint32(2), make([]*mesh.Block, 10)))
	assert.True(t, layers.LocalLayerCount() == 2, "wrong layer count")
	_, err := layers.GetLayer(1)
	assert.True(t, err == nil, "error: ", err)
	_, err1 := layers.GetLayer(1)
	assert.True(t, err1 == nil, "error: ", err1)
	_, err2 := layers.GetLayer(3)
	assert.True(t, err2 != nil, "added wrong layer ", err2)
}

func TestLayers_AddInvalidLayer(t *testing.T) {
	//todo complete layer validation and write test
}

func TestLayers_GetLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 10)))
	layers.AddLayer(mesh.NewExistingLayer(uint32(3), make([]*mesh.Block, 10)))
	layers.AddLayer(mesh.NewExistingLayer(uint32(2), make([]*mesh.Block, 10)))
	l, err := layers.GetLayer(2)
	assert.True(t, err == nil, "error: ", err)
	assert.True(t, l.Index() == 2, "wrong layer")

}

func TestLayers_LocalLayerCount(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 10)))
	layers.AddLayer(mesh.NewExistingLayer(uint32(3), make([]*mesh.Block, 10)))
	layers.AddLayer(mesh.NewExistingLayer(uint32(2), make([]*mesh.Block, 10)))
	assert.True(t, layers.LocalLayerCount() == 4, "wrong layer count")

}

func TestLayers_LatestKnownLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	layers.SetLatestKnownLayer(10)
	assert.True(t, layers.LatestKnownLayer() == 10, "wrong layer")

}
