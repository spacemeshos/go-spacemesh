package mesh

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLayers_AddLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	id := 1
	block1 := NewExistingBlock(uuid.New().ID(), uint32(id), nil)
	block2 := NewExistingBlock(uuid.New().ID(), uint32(id), nil)
	block3 := NewExistingBlock(uuid.New().ID(), uint32(id), nil)

	layers.AddLayer(NewExistingLayer(uint32(1), []*Block{block1, block2, block3}))
	assert.True(t, layers.LocalLayerCount() == 1, "wrong layer count")
	_, err := layers.GetLayer(id)
	assert.True(t, err == nil, "error: ", err)
}

func TestLayers_AddWrongLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	block1 := NewExistingBlock(uuid.New().ID(), uint32(1), nil)
	block2 := NewExistingBlock(uuid.New().ID(), uint32(2), nil)
	block3 := NewExistingBlock(uuid.New().ID(), uint32(3), nil)
	layers.AddLayer(NewExistingLayer(uint32(1), []*Block{block1}))
	layers.AddLayer(NewExistingLayer(uint32(3), []*Block{block2}))
	layers.AddLayer(NewExistingLayer(uint32(2), []*Block{block3}))
	assert.True(t, layers.LocalLayerCount() == 2, "wrong layer count")
	_, err := layers.GetLayer(1)
	assert.True(t, err == nil, "error: ", err)
	_, err1 := layers.GetLayer(1)
	assert.True(t, err1 == nil, "error: ", err1)
	_, err2 := layers.GetLayer(3)
	assert.True(t, err2 != nil, "added wrong layer ", err2)
}

//func TestLayers_AddInvalidLayer(t *testing.T) {
//	//todo complete layer validation and write test
//}

func TestLayers_GetLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	block1 := NewExistingBlock(uuid.New().ID(), uint32(1), nil)
	block2 := NewExistingBlock(uuid.New().ID(), uint32(2), nil)
	block3 := NewExistingBlock(uuid.New().ID(), uint32(3), nil)
	layers.AddLayer(NewExistingLayer(uint32(1), []*Block{block1}))
	layers.AddLayer(NewExistingLayer(uint32(3), []*Block{block2}))
	layers.AddLayer(NewExistingLayer(uint32(2), []*Block{block3}))
	l, err := layers.GetLayer(2)
	assert.True(t, err == nil, "error: ", err)
	assert.True(t, l.Index() == 2, "wrong layer")

}

func TestLayers_LocalLayerCount(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	block1 := NewExistingBlock(uuid.New().ID(), uint32(1), nil)
	block2 := NewExistingBlock(uuid.New().ID(), uint32(2), nil)
	block3 := NewExistingBlock(uuid.New().ID(), uint32(3), nil)
	block4 := NewExistingBlock(uuid.New().ID(), uint32(2), nil)
	layers.AddLayer(NewExistingLayer(uint32(1), []*Block{block1}))
	layers.AddLayer(NewExistingLayer(uint32(3), []*Block{block2}))
	layers.AddLayer(NewExistingLayer(uint32(2), []*Block{block3}))
	layers.AddLayer(NewExistingLayer(uint32(2), []*Block{block4}))
	assert.True(t, layers.LocalLayerCount() == 3, "wrong layer count")

}

func TestLayers_LatestKnownLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	layers.SetLatestKnownLayer(10)
	assert.True(t, layers.LatestKnownLayer() == 10, "wrong layer")

}
