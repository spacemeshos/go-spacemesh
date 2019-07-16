package sync

import (
	"errors"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type mockTxProcessor struct {
	notValid bool
}

func (m mockTxProcessor) ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error) {
	if !m.notValid {

		return address.HexToAddress("0xFFFF"), nil
	}
	return address.Address{}, errors.New("invalid sig for tx")
}

func ListenerFactory(serv service.Service, peers p2p.Peers, name string, layer types.LayerID) *BlockListener {
	ch := make(chan types.LayerID, 1)
	ch <- layer
	l := log.New(name, "", "")
	poetDb := activation.NewPoetDb(database.NewMemDatabase(), l.WithName("poetDb"))
	blockValidator := NewBlockValidator(BlockEligibilityValidatorMock{})
	sync := NewSync(serv, getMesh(memoryDB, name), miner.NewTypesTransactionIdMemPool(), miner.NewTypesAtxIdMemPool(), mockTxProcessor{}, blockValidator, poetDb, conf, ch, layer, l)
	sync.Peers = peers
	nbl := NewBlockListener(serv, sync, 2, 4, log.New(name, "", ""))
	return nbl
}

func TestBlockListener(t *testing.T) {
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "listener1", 3)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "listener2", 3)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()
	atx1 := atx()
	atx2 := atx()
	atx3 := atx()

	bl1.ProcessAtx(atx1)
	bl1.ProcessAtx(atx2)
	bl1.ProcessAtx(atx3)

	bl2.ProcessAtx(atx1)

	block1 := types.NewExistingBlock(types.BlockID(123), 0, nil)
	block1.Signature = signer.Sign(block1.Bytes())
	block1.ATXID = *types.EmptyAtxId
	block2 := types.NewExistingBlock(types.BlockID(321), 1, nil)
	block2.Signature = signer.Sign(block2.Bytes())
	block2.AtxIds = append(block2.AtxIds, atx2.Id())
	block3 := types.NewExistingBlock(types.BlockID(222), 2, nil)
	block3.Signature = signer.Sign(block3.Bytes())
	block3.AtxIds = append(block3.AtxIds, atx3.Id())

	block1.AddView(block2.ID())
	block1.AddView(block3.ID())

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)

	_, err := bl1.GetBlock(block1.Id)
	if err != nil {
		t.Error(err)
	}

	bl2.GetFullBlocks([]types.BlockID{block1.Id})

	b, err := bl2.GetBlock(block1.Id)
	if err != nil {
		t.Error(err)
	}

	t.Log("  ", b)
	t.Log("done!")
}

func TestBlockListenerViewTraversal(t *testing.T) {

	t.Log("TestBlockListener2 start")
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()

	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_1", 2)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "TestBlockListener_2", 2)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()

	atx := atx()

	byts, _ := types.InterfaceToBytes(atx)
	var atx1 types.ActivationTx
	types.BytesToInterface(byts, &atx1)

	bl1.ProcessAtx(atx)
	bl2.ProcessAtx(&atx1)

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block1.ATXID = *types.EmptyAtxId
	block1.Signature = signer.Sign(block1.Bytes())
	block1.Id = 1

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2.ATXID = *types.EmptyAtxId
	block2.Signature = signer.Sign(block2.Bytes())
	block2.Id = 2

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3.ATXID = *types.EmptyAtxId
	block3.Signature = signer.Sign(block3.Bytes())
	block3.Id = 3

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block4.ATXID = *types.EmptyAtxId
	block4.Signature = signer.Sign(block4.Bytes())
	block4.Id = 4

	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block5.ATXID = *types.EmptyAtxId
	block5.Signature = signer.Sign(block5.Bytes())
	block5.Id = 5

	block6 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block6.ATXID = *types.EmptyAtxId
	block6.Signature = signer.Sign(block6.Bytes())
	block6.Id = 6

	block7 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block7.ATXID = *types.EmptyAtxId
	block7.Signature = signer.Sign(block7.Bytes())
	block7.Id = 7

	block8 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block8.ATXID = *types.EmptyAtxId
	block8.Signature = signer.Sign(block8.Bytes())
	block8.Id = 8

	block9 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block9.ATXID = *types.EmptyAtxId
	block9.Signature = signer.Sign(block9.Bytes())
	block9.Id = 9

	block10 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block10.ATXID = *types.EmptyAtxId
	block10.Signature = signer.Sign(block10.Bytes())
	block10.Id = 10

	block2.AddView(block1.ID())
	block3.AddView(block2.ID())
	block4.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	block6.AddView(block4.ID())
	block7.AddView(block6.ID())
	block7.AddView(block5.ID())
	block8.AddView(block7.ID())
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

	bl2.GetFullBlocks([]types.BlockID{block10.Id})

	b, err := bl1.GetBlock(block1.Id)
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block1.Id)
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block2.Id)
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block3.Id)
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block4.Id)
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block5.Id)
	if err != nil {
		t.Error(err)
	}

	t.Log("  ", b)
	t.Log("done!")
}

func TestBlockListener_TraverseViewBadFlow(t *testing.T) {

	t.Log("TestBlockListener2 start")
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()

	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_1", 2)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "TestBlockListener_2", 2)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()

	atx := atx()

	byts, _ := types.InterfaceToBytes(atx)
	var atx1 types.ActivationTx
	types.BytesToInterface(byts, &atx1)

	bl1.ProcessAtx(atx)
	bl2.ProcessAtx(&atx1)

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block1.Id = 1
	block1.ATXID = *types.EmptyAtxId
	block1.Signature = signer.Sign(block1.Bytes())

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2.ATXID = *types.EmptyAtxId
	block2.Signature = signer.Sign(block2.Bytes())
	block2.Id = 2

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3.ATXID = *types.EmptyAtxId
	block3.Signature = signer.Sign(block3.Bytes())
	block3.Id = 3

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block4.ATXID = *types.EmptyAtxId
	block4.Signature = signer.Sign(block4.Bytes())
	block4.Id = 4

	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block5.ATXID = *types.EmptyAtxId
	block5.Signature = signer.Sign(block5.Bytes())
	block5.Id = 5

	block2.AddView(block1.ID())
	block3.AddView(block2.ID())
	block4.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())

	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)

	bl2.GetFullBlocks([]types.BlockID{block5.Id})

	b, err := bl2.GetBlock(block1.Id)
	if err == nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block2.Id)
	if err == nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block3.Id)
	if err == nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block4.Id)
	if err == nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block5.Id)
	if err == nil {
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

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_ListenToGossipBlocks1", 1)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "TestBlockListener_ListenToGossipBlocks2", 1)

	bl1.Start()
	bl2.Start() // TODO: @almog make sure data is available without starting

	blk := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	tx := types.NewAddressableTx(0, address.BytesToAddress([]byte{0x01}), address.BytesToAddress([]byte{0x02}), 10, 10, 10)
	atx := atx()
	bl2.AddBlockWithTxs(blk, []*types.AddressableSignedTransaction{tx}, []*types.ActivationTx{atx})

	mblk := types.Block{MiniBlock: types.MiniBlock{BlockHeader: blk.BlockHeader, TxIds: []types.TransactionId{types.GetTransactionId(tx.SerializableSignedTransaction)}, AtxIds: []types.AtxId{atx.Id()}}}
	signer := signing.NewEdSigner()
	mblk.Signature = signer.Sign(mblk.Bytes())

	data, err := types.InterfaceToBytes(&mblk)
	require.NoError(t, err)

	err = n2.Broadcast(config.NewBlockProtocol, data)
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
				assert.True(t, mblk.Compare(b))
				t.Log("done!")
				return
			}
		}
	}

	bl2.Close()
	bl1.Close()
	time.Sleep(1 * time.Second)
}

//todo integration testing
