package sync

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

const fetchBlockTimeout = 1 * time.Minute

const layerHashTimeout = 1 * time.Minute

var (
	syncObj = NewSync(
		&PeersMocks{},
		&LayersMocks{0, 0, make([]LayerMock, 0, 10)},
		nil,
		Configuration{1, 1, 1 * time.Millisecond, 1},
	)
)

func SendBlockRequest(sp *p2p.Protocol, peer Peer, id []byte, layer int32) (Block, error) {
	data := &pb.FetchBlockReq{
		BlockId: []byte("block id 1"),
		Layer:   int32(1),
	}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	b, err := sp.SendAsyncRequest(block, payload, peer.PublicKey().String(), fetchBlockTimeout, HandleBlockResponse)

	if err != nil {
		return nil, err
	}

	return b.(Block), nil
}

func SendLayerHashRequest(sp *p2p.Protocol, peer Peer, layer int32) (string, error) {
	data := &pb.LayerHashReq{Layer: layer}
	payload, err := proto.Marshal(data)
	if err != nil {
		return "", err
	}

	b, err := sp.SendAsyncRequest(layerHash, payload, peer.PublicKey().String(), layerHashTimeout, HandleLayerHashResponse)

	if err != nil {
		return "", err
	}
	str := b.([]uint8)
	return string(str), nil
}

const protocol = "/sync/fblock/1.0/"
const block = "block"
const layerHash = "layerHash"

func TestSyncProtocol_AddMsgHandlers(t *testing.T) {

	sim := simulator.New()
	fnd1 := p2p.NewProtocol(sim.NewNode(), protocol)

	n2 := sim.NewNode()

	syncObj.layers.AddLayer(1, []Block{&BlockMock{[]byte("block id 1"), 1}})

	fnd2 := p2p.NewProtocol(n2, protocol)
	fnd2.RegisterMsgHandler(block, syncObj.BlockRequestHandler)
	idarr, err := SendBlockRequest(fnd1, n2.Node, []byte("block id 1"), 1)

	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, idarr.Id(), []byte("block id 1"), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers2(t *testing.T) {

	sim := simulator.New()
	p1 := PeersMocks{sim.NewNode()}
	fnd1 := p2p.NewProtocol(p1, protocol)

	n2 := sim.NewNode()
	p2 := PeersMocks{n2}

	syncObj.layers.AddLayer(1, []Block{&BlockMock{[]byte("block id 1"), 1}})

	fnd2 := p2p.NewProtocol(p2, protocol)
	fnd2.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)

	idarr, err := SendLayerHashRequest(fnd1, n2.Node, 1)

	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)
}

func TestSyncProtocol_AddMsgHandlers3(t *testing.T) {
	sim := simulator.New()
	p1 := PeersMocks{sim.NewNode()}
	fnd1 := p2p.NewProtocol(p1, protocol)
	fnd1.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)
	fnd1.RegisterMsgHandler(block, syncObj.BlockRequestHandler)
	n2 := sim.NewNode()
	p2 := PeersMocks{n2}
	syncObj.layers.AddLayer(1, []Block{&BlockMock{[]byte("block id 1"), 1}})
	syncObj.layers.AddLayer(2, []Block{&BlockMock{[]byte("block id 2"), 1}})
	syncObj.layers.AddLayer(3, []Block{&BlockMock{[]byte("block id 3"), 1}})
	fnd2 := p2p.NewProtocol(p2, protocol)
	fnd2.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)
	fnd2.RegisterMsgHandler(block, syncObj.BlockRequestHandler)

	idarr, err := SendLayerHashRequest(fnd1, n2.Node, 1)
	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)

	idarr2, err2 := SendBlockRequest(fnd1, n2.Node, []byte("block id 1"), 1)
	assert.NoError(t, err2, "Should not return error")
	assert.Equal(t, idarr2.Id(), []byte("block id 1"), "wrong block")

	idarr, err = SendLayerHashRequest(fnd1, n2.Node, 2)
	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)

	idarr2, err2 = SendBlockRequest(fnd1, n2.Node, []byte("block id 1"), 1)
	assert.NoError(t, err2, "Should not return error")
	assert.Equal(t, idarr2.Id(), []byte("block id 1"), "wrong block")

	idarr, err = SendLayerHashRequest(fnd1, n2.Node, 3)
	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)

	idarr2, err2 = SendBlockRequest(fnd1, n2.Node, []byte("block id 1"), 1)
	assert.NoError(t, err2, "Should not return error")
	assert.Equal(t, idarr2.Id(), []byte("block id 1"), "wrong block")
}
