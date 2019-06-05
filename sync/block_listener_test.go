package sync

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func ListenerFactory(serv service.Service, peers p2p.Peers, name string) *BlockListener {

	tick := 200 * time.Millisecond
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	ts := timesync.NewTicker(MockTimer{}, tick, start)
	tk := ts.Subscribe()
	l := log.New(name, "", "")
	sync := NewSync(serv, getMesh(memoryDB, name+"_"+time.Now().String()), BlockValidatorMock{}, TxValidatorMock{}, conf, tk, l)
	sync.Peers = peers
	nbl := NewBlockListener(serv, BlockValidatorMock{}, sync, 2, log.New(name, "", ""))
	return nbl
}

func TestBlockListener(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "1")
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "2")
	bl2.Start()

	bl1.ProcessAtx(atx1)
	bl1.ProcessAtx(atx2)
	bl1.ProcessAtx(atx3)

	block1 := types.NewExistingBlock(types.BlockID(123), 0, nil)
	block1.BlockHeader.ATXID = atx1.Id()
	block2 := types.NewExistingBlock(types.BlockID(321), 1, nil)
	block1.BlockHeader.ATXID = atx2.Id()
	block3 := types.NewExistingBlock(types.BlockID(222), 2, nil)
	block1.BlockHeader.ATXID = atx3.Id()

	block1.AddView(block2.ID())
	block1.AddView(block3.ID())

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)

	_, err := bl2.fetchFullBlocks([]types.BlockID{block1.Id})
	if err != nil {
		t.Error(err)
	}

	b, err := bl2.GetBlock(block1.Id)
	if err != nil {
		t.Error(err)
	}

	t.Log("  ", b)
	t.Log("done!")
}

func TestBlockListener2(t *testing.T) {

	t.Log("TestBlockListener2 start")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener1")
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "TestBlockListener2")

	bl2.Start()

	bl1.ProcessAtx(atx1)
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block1.ATXID = atx1.Id()

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2.ATXID = atx1.Id()

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3.ATXID = atx1.Id()

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block4.ATXID = atx1.Id()

	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block5.ATXID = atx1.Id()

	block6 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block6.ATXID = atx1.Id()

	block7 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block7.ATXID = atx1.Id()

	block8 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block8.ATXID = atx1.Id()

	block9 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block9.ATXID = atx1.Id()

	block10 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block10.ATXID = atx1.Id()

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

	_, err := bl2.fetchFullBlocks([]types.BlockID{block10.Id})
	if err != nil {
		t.Error(err)
	}

	b, err := bl2.GetBlock(block10.Id)
	if err != nil {
		t.Error(err)
	}

	t.Log("  ", b)
	t.Log("done!")
}

func TestBlockListener_ListenToGossipBlocks(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	//n2.RegisterGossipProtocol(NewBlockProtocol)

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "5")
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "5")
	bl1.Start()

	blk := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	tx := types.NewSerializableTransaction(0, address.BytesToAddress([]byte{0x01}), address.BytesToAddress([]byte{0x02}), big.NewInt(10), big.NewInt(10), 10)
	blk.AddTransaction(tx)
	atx := types.NewActivationTx(types.NodeId{"whatwhatwhatwhat", []byte("bbb")}, 1, types.AtxId{}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}), false)
	blk.AddAtx(atx)

	bl2.AddBlock(blk)

	mblk := &types.MiniBlock{BlockHeader: blk.BlockHeader, TxIds: []types.TransactionId{types.GetTransactionId(tx)}, ATxIds: []types.AtxId{atx.Id()}}

	data, err := types.InterfaceToBytes(*mblk)
	require.NoError(t, err)

	err = n2.Broadcast(NewBlockProtocol, data)
	assert.NoError(t, err)

	time.Sleep(3 * time.Second)
	timeout := time.After(5 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if b, err := bl1.GetBlock(blk.Id); err == nil {
				assert.True(t, blk.Compare(b))
				t.Log("  ", b)
				t.Log("done!")
				return
			}
		}
	}
}

//todo integration testing
