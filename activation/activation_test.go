package activation

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

type EchProvider struct{}

func (EchProvider) Epoch(l types.LayerID) types.EpochId {
	return types.EpochId(l / 10)
}

type ActiveSetProviderMock struct{}

func (ActiveSetProviderMock) GetActiveSetSize(l types.LayerID) uint32 {
	return 10
}

type MeshProviderrMock struct{}

func (MeshProviderrMock) GetLatestView() []types.BlockID {
	return []types.BlockID{1, 2, 3}
}

func (MeshProviderrMock) LatestLayerId() types.LayerID {
	return 12
}

type NetMock struct {
	bt []byte
}

func (n *NetMock) Broadcast(id string, d []byte) error {
	n.bt = d
	return nil
}

type NipstBuilderMock struct {

}

func (np *NipstBuilderMock) BuildNIPST(challange []byte) (*nipst.NIPST, error){
	return &nipst.NIPST{}, nil
}

func TestBuilder_BuildActivationTx(t *testing.T) {
	//todo: implement test
	id := types.NodeId{"aaaa", []byte("bbb")}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	b := NewBuilder(id, database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), net, ActiveSetProviderMock{}, layers, echp, 10, &NipstBuilderMock{})
	adb := b.db
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	npst := nipst.NIPST{}
	atx := types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")},
		1,
		prevAtx,
		5,
		1,
		prevAtx,
		5,
		[]types.BlockID{1, 2, 3},
		&npst)
	adb.StoreAtx(echp.Epoch(atx.LayerIdx), atx)
	act := types.NewActivationTx(b.nodeId, b.GetLastSequence(b.nodeId)+1, atx.Id(), layers.LatestLayerId(), 0, atx.Id(), b.activeSet.GetActiveSetSize(1), b.mesh.GetLatestView(), &npst)
	err := b.PublishActivationTx(&nipst.NIPST{})
	assert.NoError(t, err)
	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
}

func TestBuilder_NoPrevATX(t *testing.T) {
	//todo: implement test
	id := types.NodeId{"aaaa", []byte("bbb")}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	b := NewBuilder(id, database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), net, ActiveSetProviderMock{}, layers, echp, 10,&NipstBuilderMock{})
	err := b.PublishActivationTx(&nipst.NIPST{})
	assert.Error(t, err)
}

func TestBuilder_CreatePoETChallenge(t *testing.T) {

}
