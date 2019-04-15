package activation

import (
	"fmt"
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

func (ActiveSetProviderMock) ActiveSetIds(l types.EpochId) uint32 {
	return 10
}

type MeshProviderrMock struct{}

func (MeshProviderrMock) GetLatestVerified() []types.BlockID {
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
	Challenge *common.Hash
}

func (np *NipstBuilderMock) BuildNIPST(challenge *common.Hash) (*nipst.NIPST, error) {
	np.Challenge = challenge
	return &nipst.NIPST{}, nil
}

type NipstErrBuilderMock struct{}

func (np *NipstErrBuilderMock) BuildNIPST(challenge *common.Hash) (*nipst.NIPST, error) {
	return nil, fmt.Errorf("error")
}

func TestBuilder_BuildActivationTx(t *testing.T) {
	//todo: implement test
	id := types.NodeId{"aaaa", []byte("bbb")}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	layersPerEpcoh := types.LayerID(10)
	b := NewBuilder(id, database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), net, ActiveSetProviderMock{}, layers, 10, &NipstBuilderMock{}, nil)
	adb := b.db
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	npst := nipst.NIPST{}
	atx := types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, &npst, true)
	adb.StoreAtx(echp.Epoch(atx.LayerIdx), atx)
	act := types.NewActivationTx(b.nodeId, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.LayerIdx+layersPerEpcoh, 0, atx.Id(), b.activeSet.ActiveSetIds(1), b.mesh.GetLatestVerified(), &npst, true)
	err := b.PublishActivationTx(types.EpochId(layers.LatestLayerId() / layersPerEpcoh))
	assert.NoError(t, err)
	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
}

func TestBuilder_NoPrevATX(t *testing.T) {
	//todo: implement test
	id := types.NodeId{"aaaa", []byte("bbb")}
	net := &NetMock{}
	layers := MeshProviderrMock{}
	b := NewBuilder(id, database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), net, ActiveSetProviderMock{}, layers, 10, &NipstBuilderMock{}, nil)
	err := b.PublishActivationTx(1)
	assert.Error(t, err)
}

func TestBuilder_PublishActivationTx(t *testing.T) {
	id := types.NodeId{"aaaa", []byte("bbb")}
	net := &NetMock{}
	echp := &EchProvider{}
	layers := MeshProviderrMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpcoh := uint64(10)
	b := NewBuilder(id, database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), net, ActiveSetProviderMock{}, layers, layersPerEpcoh, nipstBuilder, nil)
	adb := b.db
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	npst := nipst.NIPST{}

	atx := types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, &npst, true)

	err := adb.StoreAtx(echp.Epoch(types.LayerID(uint64(atx.LayerIdx)/layersPerEpcoh)), atx)
	assert.NoError(t, err)

	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Sequence:       b.GetLastSequence(b.nodeId) + 1,
		PrevATXId:      atx.Id(),
		LayerIdx:       types.LayerID(uint64(atx.LayerIdx) + b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: atx.Id(),
	}

	bytes, err := challenge.Hash()
	assert.NoError(t, err)

	act := types.NewActivationTx(b.nodeId, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.LayerIdx+10, 0, atx.Id(), b.activeSet.ActiveSetIds(1), b.mesh.GetLatestVerified(), &npst, true)
	err = b.PublishActivationTx(1)
	assert.NoError(t, err)
	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
	assert.Equal(t, bytes, nipstBuilder.Challenge)

	b.nipstBuilder = &NipstErrBuilderMock{}
	err = b.PublishActivationTx(echp.Epoch(1))
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "error")

	bt := NewBuilder(id, database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), net, ActiveSetProviderMock{}, layers, layersPerEpcoh, &NipstBuilderMock{}, nil)
	err = bt.PublishActivationTx(1)
	assert.Error(t, err)

}
