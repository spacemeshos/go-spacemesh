package activation

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/stretchr/testify/assert"
	"testing"
)

type EchProvider struct{}

func (EchProvider) Epoch(l mesh.LayerID) EpochId {
	return EpochId(l / 10)
}

type ActiveSetProviderMock struct{}

func (ActiveSetProviderMock) GetActiveSetSize(l mesh.LayerID) uint32 {
	return 10
}

type MeshProviderrMock struct{}

func (MeshProviderrMock) GetLatestView() []mesh.BlockID {
	return []mesh.BlockID{1, 2, 3}
}

func (MeshProviderrMock) LatestLayerId() mesh.LayerID {
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
	id := mesh.NodeId{"aaaa", "bbb"}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	b := NewBuilder(id, database.NewMemDatabase(), net, ActiveSetProviderMock{}, layers, echp)
	adb := b.db
	prevAtx := mesh.AtxId{Hash: common.HexToHash("0x111")}
	npst := nipst.NIPST{}
	atx := mesh.NewActivationTx(mesh.NodeId{"aaaa", "bbb"},
		1,
		prevAtx,
		5,
		1,
		prevAtx,
		5,
		[]mesh.BlockID{1, 2, 3},
		&npst)
	adb.StoreAtx(echp.Epoch(atx.LayerIndex), atx)
	act := mesh.NewActivationTx(b.nodeId, b.GetLastSequence(b.nodeId)+1, atx.Id(), layers.LatestLayerId(), 0, atx.Id(), b.activeSet.GetActiveSetSize(1), b.mesh.GetLatestView(), &npst)
	err := b.PublishActivationTx(&nipst.NIPST{})
	assert.NoError(t, err)
	bts, err := mesh.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
}

func TestBuilder_NoPrevATX(t *testing.T) {
	//todo: implement test
	id := mesh.NodeId{"aaaa", "bbb"}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	b := NewBuilder(id, database.NewMemDatabase(), net, ActiveSetProviderMock{}, layers, echp)
	err := b.PublishActivationTx(&nipst.NIPST{})
	assert.Error(t, err)

}
