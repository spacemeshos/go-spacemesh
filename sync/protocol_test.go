package sync

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/mesh"
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
		mesh.NewLayers(nil, nil),
		nil,
		Configuration{1, 1, 1 * time.Millisecond, 1},
	)
)

func SendBlockRequest(sp *p2p.Protocol, peer Peer, id uint32) (Block, error) {
	data := &pb.FetchBlockReq{
		BlockId: id,
		Layer:   uint32(1),
	}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	b, err := sp.SendAsyncRequest(blockMsg, payload, peer.String(), fetchBlockTimeout, HandleBlockResponse)

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

	b, err := sp.SendAsyncRequest(layerHash, payload, peer.String(), layerHashTimeout, HandleLayerHashResponse)

	if err != nil {
		return "", err
	}
	str := b.([]uint8)
	return string(str), nil
}

const protocol = "/sync/fblock/1.0/"
const blockMsg = "block"
const layerHash = "layerHash"

func TestSyncProtocol_AddMsgHandlers(t *testing.T) {

	sim := simulator.New()
	fnd1 := p2p.NewProtocol(sim.NewNode(), protocol)

	n2 := sim.NewNode()
	block := mesh.NewBlock(false, nil, time.Now())

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block}))

	fnd2 := p2p.NewProtocol(n2, protocol)
	fnd2.RegisterMsgHandler(blockMsg, syncObj.BlockRequestHandler)
	idarr, err := SendBlockRequest(fnd1, n2.Node.PublicKey(), block.Id())

	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, idarr.Id(), block.Id(), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers2(t *testing.T) {

	sim := simulator.New()
	p1 := PeersMocks{sim.NewNode()}
	fnd1 := p2p.NewProtocol(p1, protocol)

	n2 := sim.NewNode()
	p2 := PeersMocks{n2}

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 10)))

	fnd2 := p2p.NewProtocol(p2, protocol)
	fnd2.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)

	idarr, err := SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 1)

	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)
}

func TestSyncProtocol_AddMsgHandlers3(t *testing.T) {
	sim := simulator.New()
	p1 := PeersMocks{sim.NewNode()}
	fnd1 := p2p.NewProtocol(p1, protocol)
	fnd1.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)
	fnd1.RegisterMsgHandler(blockMsg, syncObj.BlockRequestHandler)
	n2 := sim.NewNode()
	p2 := PeersMocks{n2}

	block1 := mesh.NewBlock(false, nil, time.Now())
	block2 := mesh.NewBlock(false, nil, time.Now())
	block3 := mesh.NewBlock(false, nil, time.Now())

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block1}))
	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(2), []*mesh.Block{block2}))
	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(3), []*mesh.Block{block3}))

	fnd2 := p2p.NewProtocol(p2, protocol)
	fnd2.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)
	fnd2.RegisterMsgHandler(blockMsg, syncObj.BlockRequestHandler)

	idarr, err := SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 1)
	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)

	idarr2, err2 := SendBlockRequest(fnd1, n2.Node.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	assert.Equal(t, idarr2.Id(), block1.Id(), "wrong block")

	idarr, err = SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 2)
	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)

	idarr2, err2 = SendBlockRequest(fnd1, n2.Node.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	assert.Equal(t, idarr2.Id(), block1.Id(), "wrong block")

	idarr, err = SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 3)
	assert.NoError(t, err, "Should not return error")
	fmt.Println(idarr)

	idarr2, err2 = SendBlockRequest(fnd1, n2.Node.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	assert.Equal(t, idarr2.Id(), block1.Id(), "wrong block")
}
