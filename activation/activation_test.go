package activation

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

type ActiveSetProviderMock struct{}

func (ActiveSetProviderMock) ActiveSetIds(l types.EpochId) uint32 {
	return 10
}

type MeshProviderMock struct{}

func (MeshProviderMock) GetLatestView() []types.BlockID {
	return []types.BlockID{1, 2, 3}
}

func (MeshProviderMock) LatestLayer() types.LayerID {
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

func (np *NipstBuilderMock) IsPostInitialized() bool {
	return true
}

func (np *NipstBuilderMock) InitializePost() (*nipst.PostProof, error) {
	return nil, nil
}

func (np *NipstBuilderMock) BuildNIPST(challenge *common.Hash) (*nipst.NIPST, error) {
	np.Challenge = challenge
	return nipst.NewNIPSTWithChallenge(challenge), nil
}

type NipstErrBuilderMock struct{}

func (np *NipstErrBuilderMock) IsPostInitialized() bool {

	return true
}

func (np *NipstErrBuilderMock) InitializePost() (*nipst.PostProof, error) {
	return nil, nil
}

func (np *NipstErrBuilderMock) BuildNIPST(challenge *common.Hash) (*nipst.NIPST, error) {
	return nil, fmt.Errorf("error")
}

func TestBuilder_BuildActivationTx(t *testing.T) {
	//todo: implement test
	id := types.NodeId{"aaaaaa", []byte("bbb")}
	net := &NetMock{}
	layers := MeshProviderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), uint64(layersPerEpoch), lg.WithName("atxDB"))
	b := NewBuilder(id, activationDb, net, ActiveSetProviderMock{}, layers, 10, &NipstBuilderMock{}, nil, lg.WithName("atxBuilder"))
	adb := b.db
	prevAtx := &types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	npst := nipst.NewNIPSTWithChallenge(&chlng)
	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbb")}, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst, true)
	err := adb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Sequence:       b.GetLastSequence(b.nodeId) + 1,
		PrevATXId:      *atx.Id(),
		PubLayerIdx:    atx.PubLayerIdx.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: *atx.Id(),
	}

	bytes, err := challenge.Hash()
	npst2 := nipst.NewNIPSTWithChallenge(bytes)
	act := types.NewActivationTxWithChallenge(challenge, b.activeSet.ActiveSetIds(1), b.mesh.GetLatestView(), npst2, true)

	err = b.PublishActivationTx(layers.LatestLayer().GetEpoch(layersPerEpoch))
	assert.NoError(t, err)
	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
}

func TestBuilder_NoPrevATX(t *testing.T) {
	//todo: implement test
	id := types.NodeId{uuid.New().String(), []byte("bbb")}
	net := &NetMock{}
	layers := MeshProviderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), uint64(layersPerEpoch), lg.WithName("atxDB"))
	b := NewBuilder(id, activationDb, net, ActiveSetProviderMock{}, layers, layersPerEpoch, &NipstBuilderMock{}, nil, lg.WithName("atxBuilder"))
	err := b.PublishActivationTx(2) // non genesis epoch
	assert.EqualError(t, err, "cannot find pos atx: cannot find pos atx id: not found")
}

func TestBuilder_PublishActivationTx(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbb")}
	net := &NetMock{}
	layers := MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), mesh.NewMemMeshDB(lg.WithName("meshDB")), uint64(layersPerEpoch), lg.WithName("atxDB1"))
	b := NewBuilder(id, activationDb, net, ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, lg.WithName("atxBuilder"))
	adb := b.db
	prevAtx := &types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	npst := nipst.NewNIPSTWithChallenge(&chlng)

	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbb")}, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst, true)

	err := adb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Sequence:       b.GetLastSequence(b.nodeId) + 1,
		PrevATXId:      *atx.Id(),
		PubLayerIdx:    atx.PubLayerIdx.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: *atx.Id(),
	}

	bytes, err := challenge.Hash()
	npst2 := nipst.NewNIPSTWithChallenge(bytes)
	assert.NoError(t, err)

	act := types.NewActivationTx(b.nodeId, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), b.activeSet.ActiveSetIds(1), b.mesh.GetLatestView(), npst2, true)
	err = b.PublishActivationTx(1)
	assert.NoError(t, err)

	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
	assert.Equal(t, bytes, nipstBuilder.Challenge)

	//test publish 2 transaction in same epoch
	err = b.PublishActivationTx(2)
	assert.Error(t, err)

	activationDb2 := NewActivationDb(database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), uint64(layersPerEpoch), lg.WithName("atxDB2"))
	b = NewBuilder(id, activationDb2, net, ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, lg.WithName("atxBuilder"))
	b.nipstBuilder = &NipstErrBuilderMock{}
	err = b.PublishActivationTx(0)
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "cannot create nipst error")

	activationDb3 := NewActivationDb(database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")), uint64(layersPerEpoch), lg.WithName("atxDB3"))
	bt := NewBuilder(id, activationDb3, net, ActiveSetProviderMock{}, layers, layersPerEpoch, &NipstBuilderMock{}, nil, lg.WithName("atxBuilder"))
	err = bt.PublishActivationTx(2)
	assert.EqualError(t, err, "cannot find pos atx: cannot find pos atx id: not found")
}

func TestBuilder_PublishActivationTxSerialize(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbb")}
	net := &NetMock{}
	layers := MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), mesh.NewMemMeshDB(log.NewDefault("")),
		uint64(layersPerEpoch), lg.WithName("atxDB"))
	b := NewBuilder(id, activationDb, net, ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, lg.WithName("atxBuilder"))
	adb := b.db
	prevAtx := &types.AtxId{Hash: common.HexToHash("0x111")}
	challenge1 := common.HexToHash("0x222222")
	npst := nipst.NewNIPSTWithChallenge(&challenge1)

	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbb")}, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst, true)

	err := adb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	assert.NoError(t, err)

	act := types.NewActivationTx(b.nodeId, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), b.activeSet.ActiveSetIds(1), b.mesh.GetLatestView(), npst, true)

	bt, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	a, err := types.BytesAsAtx(bt)
	assert.NoError(t, err)
	bt2, err := types.AtxAsBytes(a)
	assert.Equal(t, bt, bt2)
}
