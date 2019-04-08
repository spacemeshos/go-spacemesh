package activation

import (
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/stretchr/testify/assert"
	"testing"
)

type EchProvider struct{}

func (EchProvider) Epoch(l block.LayerID) block.EpochId {
	return block.EpochId(l / 10)
}

type ActiveSetProviderMock struct{}

func (ActiveSetProviderMock) GetActiveSetSize(l block.LayerID) uint32 {
	return 10
}

type MeshProviderrMock struct{}

func (MeshProviderrMock) GetLatestView() []block.BlockID {
	return []block.BlockID{1, 2, 3}
}

func (MeshProviderrMock) LatestLayerId() block.LayerID {
	return 12
}

type NetMock struct {
	bt []byte
}

func (n *NetMock) Broadcast(id string, d []byte) error {
	n.bt = d
	return nil
}

func TestBuilder_BuildActivationTx(t *testing.T) {
	//todo: implement test
	id := block.NodeId{"aaaa", []byte("bbb")}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	b := NewBuilder(id, database.NewMemDatabase(),mesh.NewMemMeshDB(log.NewDefault("")) ,net, ActiveSetProviderMock{}, layers, echp, 10)
	adb := b.db
	prevAtx := block.AtxId{Hash: common.HexToHash("0x111")}
	npst := nipst.NIPST{}
	atx := block.NewActivationTx(block.NodeId{"aaaa", []byte("bbb")},
		1,
		prevAtx,
		5,
		1,
		prevAtx,
		5,
		[]block.BlockID{1, 2, 3},
		&npst)
	adb.StoreAtx(echp.Epoch(atx.LayerIndex), atx)
	act := block.NewActivationTx(b.nodeId, b.GetLastSequence(b.nodeId)+1, atx.Id(), layers.LatestLayerId(), 0, atx.Id(), b.activeSet.GetActiveSetSize(1), b.mesh.GetLatestView(), &npst)
	err := b.PublishActivationTx(&nipst.NIPST{})
	assert.NoError(t, err)
	bts, err := block.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
}

func TestBuilder_NoPrevATX(t *testing.T) {
	//todo: implement test
	id := block.NodeId{"aaaa", []byte("bbb")}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	b := NewBuilder(id, database.NewMemDatabase(),mesh.NewMemMeshDB(log.NewDefault("")), net, ActiveSetProviderMock{}, layers, echp,10)
	err := b.PublishActivationTx(&nipst.NIPST{})
	assert.Error(t, err)
}
