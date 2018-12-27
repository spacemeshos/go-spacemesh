package sync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestBlockListener(t *testing.T) {

	fmt.Println("test sync start")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := NewBlockListener(PeersImpl{n1, func() []Peer { return []Peer{n2.PublicKey()} }},
		BlockValidatorMock{}, getMesh(make(chan *mesh.Block), "TestBlockListener_1"), 10*time.Second)
	bl2 := NewBlockListener(PeersImpl{n2, func() []Peer { return []Peer{n1.PublicKey()} }},
		BlockValidatorMock{}, getMesh(make(chan *mesh.Block), "TestBlockListener_2"), 10*time.Second)
	bl1.Start()
	bl2.Start()

	block1 := mesh.NewExistingBlock(mesh.BlockID(123), 0, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(321), 1, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(222), 2, nil)

	bl1.AddLayer(mesh.NewExistingLayer(0, []*mesh.Block{block1}))
	bl1.AddLayer(mesh.NewExistingLayer(1, []*mesh.Block{block2}))
	bl1.AddLayer(mesh.NewExistingLayer(2, []*mesh.Block{block3}))
	bl2.fetchBlock(block1.Id)
	_, err := bl2.GetBlock(block1.Id)
	assert.NoError(t, err, "Should be able to establish a connection on a port")
}

//todo more unit tests
//todo integration testing
