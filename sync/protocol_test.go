package sync

import (
	"github.com/gogo/protobuf/proto"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var (
	syncObj = NewSync(
		&PeersMocks{},
		mesh.NewLayers(nil, nil),
		nil,
		Configuration{1, 1, 1 * time.Millisecond, 1},
	)
)

func SendBlockRequest(sp *p2p.Protocol, peer Peer, id uint32) (chan *pb.FetchBlockResp, error) {

	data := &pb.FetchBlockReq{
		BlockId: id,
		Layer:   uint32(1),
	}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	foo, ch := NewBlockResponseHandler()
	return ch, sp.SendAsyncRequest(blockMsg, payload, peer.String(), foo)
}

func SendLayerHashRequest(sp *p2p.Protocol, peer Peer, layer int32) (chan *pb.LayerHashResp, error) {

	data := &pb.LayerHashReq{Layer: layer}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	foo, ch := NewLayerHashHandler()
	return ch, sp.SendAsyncRequest(layerHash, payload, peer.String(), foo)
}

const protocol = "/sync/fblock/1.0/"
const blockMsg = 1
const layerHash = 2

func TestSyncProtocol_AddMsgHandlers(t *testing.T) {

	sim := simulator.New()
	fnd1 := p2p.NewProtocol(sim.NewNode(), protocol)

	n2 := sim.NewNode()

	id := uuid.New().ID()
	block := mesh.NewExistingBlock(id, 1, nil)

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block}))

	fnd2 := p2p.NewProtocol(n2, protocol)
	fnd2.RegisterMsgHandler(blockMsg, syncObj.BlockRequestHandler)

	ch, err := SendBlockRequest(fnd1, n2.Node.PublicKey(), block.Id())
	a := <-ch

	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, a.BlockId, block.Id(), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers2(t *testing.T) {

	sim := simulator.New()
	p1 := PeersMocks{sim.NewNode()}
	fnd1 := p2p.NewProtocol(p1, protocol)

	n2 := sim.NewNode()
	p2 := PeersMocks{n2}

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 0, 10)))

	fnd2 := p2p.NewProtocol(p2, protocol)
	fnd2.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)

	ch, err := SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 1)
	a := <-ch
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(a.Hash), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers3(t *testing.T) {

	sim := simulator.New()
	p1 := PeersMocks{sim.NewNode()}
	fnd1 := p2p.NewProtocol(p1, protocol)
	fnd1.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)
	fnd1.RegisterMsgHandler(blockMsg, syncObj.BlockRequestHandler)
	n2 := sim.NewNode()
	p2 := PeersMocks{n2}

	block1 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block2 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block3 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block1}))
	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(2), []*mesh.Block{block2}))
	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(3), []*mesh.Block{block3}))

	fnd2 := p2p.NewProtocol(p2, protocol)
	fnd2.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)
	fnd2.RegisterMsgHandler(blockMsg, syncObj.BlockRequestHandler)

	ch, err := SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 1)
	assert.NoError(t, err, "Should not return error")
	msg := <-ch
	assert.Equal(t, "some hash representing the layer", string(msg.Hash), "wrong block")

	ch2, err2 := SendBlockRequest(fnd1, n2.Node.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 := <-ch2
	assert.Equal(t, msg2.BlockId, block1.Id(), "wrong block")

	ch, err = SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 2)
	assert.NoError(t, err, "Should not return error")
	msg = <-ch
	assert.Equal(t, "some hash representing the layer", string(msg.Hash), "wrong block")

	ch2, err2 = SendBlockRequest(fnd1, n2.Node.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.BlockId, block1.Id(), "wrong block")

	ch, err = SendLayerHashRequest(fnd1, n2.Node.PublicKey(), 3)
	assert.NoError(t, err, "Should not return error")
	msg = <-ch
	assert.Equal(t, "some hash representing the layer", string(msg.Hash), "wrong block")

	ch2, err2 = SendBlockRequest(fnd1, n2.Node.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.BlockId, block1.Id(), "wrong block")
}
