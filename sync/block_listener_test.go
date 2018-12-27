package sync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"testing"
	"time"
)

func TestBlockListener(t *testing.T) {

	fmt.Println("test sync start")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := NewBlockListener(PeersImpl{n1, func() []Peer { return []Peer{n2.PublicKey()} }},
		BlockValidatorMock{}, getMesh("TestBlockListener_1"), 1*time.Second)
	bl2 := NewBlockListener(PeersImpl{n2, func() []Peer { return []Peer{n1.PublicKey()} }},
		BlockValidatorMock{}, getMesh("TestBlockListener_2"), 1*time.Second)

	bl2.Start()

	block1 := mesh.NewExistingBlock(mesh.BlockID(123), 0, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(321), 1, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(222), 2, nil)

	block1.BlockVotes[block2.ID()] = true
	block1.BlockVotes[block3.ID()] = true

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)

	bl2.FetchBlock(block1.Id)
	b, _ := bl2.GetBlock(block1.Id)
	fmt.Println("  ", b)
	time.Sleep(10 * time.Second)
}

//todo more unit tests
//todo integration testing
