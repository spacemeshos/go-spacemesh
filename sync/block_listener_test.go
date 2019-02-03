package sync

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
	"time"
)

type PeersMock struct {
	getPeers func() []p2p.Peer
}

func (pm PeersMock) GetPeers() []p2p.Peer {
	return pm.getPeers()
}

func (pm PeersMock) Close() {
	return
}
func ListenerFactory(serv server.Service, peers p2p.Peers, name string) *BlockListener {
	nbl := NewBlockListener(serv, BlockValidatorMock{}, getMesh(memoryDB, "TestBlockListener_"+name), 1*time.Second, 2, log.New(name, "", ""))
	nbl.Peers = peers //override peers with mock
	return nbl
}

func TestBlockListener(t *testing.T) {

	fmt.Println("test sync start")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "1")
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "2")
	bl2.Start()

	block1 := mesh.NewExistingBlock(mesh.BlockID(123), 0, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(321), 1, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(222), 2, nil)

	block1.AddView(block2.ID())
	block1.AddView(block3.ID())

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)

	bl2.FetchBlock(block1.Id)
	timeout := time.After(30 * time.Second)
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
		default:
			if b, err := bl2.GetBlock(block1.Id); err == nil {
				fmt.Println("  ", b)
				t.Log("done!")
				break loop
			}
		}
	}
}

func TestBlockListener2(t *testing.T) {

	fmt.Println("test sync start")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "3")
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "4")

	bl2.Start()

	block1 := mesh.NewBlock(true, nil, time.Now(), 0)
	block2 := mesh.NewBlock(true, nil, time.Now(), 1)
	block3 := mesh.NewBlock(true, nil, time.Now(), 2)
	block4 := mesh.NewBlock(true, nil, time.Now(), 2)
	block5 := mesh.NewBlock(true, nil, time.Now(), 3)
	block6 := mesh.NewBlock(true, nil, time.Now(), 3)
	block7 := mesh.NewBlock(true, nil, time.Now(), 4)
	block8 := mesh.NewBlock(true, nil, time.Now(), 4)
	block9 := mesh.NewBlock(true, nil, time.Now(), 4)
	block10 := mesh.NewBlock(true, nil, time.Now(), 5)

	block2.AddView(block1.ID())
	block3.AddView(block2.ID())
	block4.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	block6.AddView(block4.ID())
	block7.AddView(block6.ID())
	block7.AddView(block5.ID())
	block8.AddView(block6.ID())
	block9.AddView(block5.ID())
	block10.AddView(block8.ID())
	block10.AddView(block9.ID())

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)
	bl1.AddBlock(block6)
	bl1.AddBlock(block7)
	bl1.AddBlock(block8)
	bl1.AddBlock(block9)
	bl1.AddBlock(block10)

	bl2.FetchBlock(block10.Id)

	timeout := time.After(10 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if b, err := bl2.GetBlock(block1.Id); err == nil {
				fmt.Println("  ", b)
				t.Log("done!")
				return
			}
		}
	}
}

func TestBlockListener_ListenToGossipBlocks(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "5")
	bl1.Start()

	blk := mesh.NewBlock(false, nil, time.Now(), 1)
	tx := mesh.NewSerializableTransaction(0, address.BytesToAddress([]byte{0x01}), address.BytesToAddress([]byte{0x02}), big.NewInt(10), big.NewInt(10), 10)
	blk.AddTransaction(tx)
	blk.AddVote(1)
	blk.AddView(2)

	data, err := mesh.BlockAsBytes(*blk)
	blk2, ok := mesh.BytesAsBlock(bytes.NewReader(data))
	assert.NoError(t, ok)
	assert.Equal(t, *blk, blk2)

	assert.NoError(t, err)
	n2.Broadcast(NewBlockProtocol, data)

	timeout := time.After(5 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if b, err := bl1.GetBlock(blk.Id); err == nil {
				assert.Equal(t, blk, b)
				fmt.Println("  ", b)
				t.Log("done!")
				return
			}
		}
	}

}

//todo integration testing
