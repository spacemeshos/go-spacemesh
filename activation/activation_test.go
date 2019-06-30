package activation

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
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

func (ActiveSetProviderMock) ActiveSetSize(epochId types.EpochId) (uint32, error) {
	return 10, nil
}

type MeshProviderMock struct{}

func (MeshProviderMock) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	return []types.BlockID{1, 2, 3}, nil
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

func (np *NipstBuilderMock) InitializePost() (*types.PostProof, error) {
	return nil, nil
}

func (np *NipstBuilderMock) BuildNIPST(challenge *common.Hash) (*types.NIPST, error) {
	np.Challenge = challenge
	return nipst.NewNIPSTWithChallenge(challenge), nil
}

type NipstErrBuilderMock struct{}

func (np *NipstErrBuilderMock) IsPostInitialized() bool {

	return true
}

func (np *NipstErrBuilderMock) InitializePost() (*types.PostProof, error) {
	return nil, nil
}

func (np *NipstErrBuilderMock) BuildNIPST(challenge *common.Hash) (*types.NIPST, error) {
	return nil, fmt.Errorf("error")
}

type MockIStore struct {
}

func (*MockIStore) StoreNodeIdentity(id types.NodeId) error {
	return nil
}

func (*MockIStore) GetIdentity(id string) (types.NodeId, error) {
	return types.NodeId{}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(nipst *types.NIPST, expectedChallenge common.Hash) error {
	return nil
}

func TestBuilder_BuildActivationTx(t *testing.T) {
	//todo: implement test
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := MeshProviderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
	view, err := layers.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	bt, err := types.ViewAsBytes(view)
	assert.NoError(t, err)
	activesetCache.put(common.BytesToHash(bt), 10)
	b := NewBuilder(id, coinbase, activationDb, net, ActiveSetProviderMock{}, layers, 10, &NipstBuilderMock{}, nil, func() bool { return true }, lg.WithName("atxBuilder"), )
	adb := b.db
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	npst := nipst.NewNIPSTWithChallenge(&chlng)
	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst, true )
	err = adb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Coinbase:       coinbase,
		Sequence:       b.GetLastSequence(b.nodeId) + 1,
		PrevATXId:      atx.Id(),
		PubLayerIdx:    atx.PubLayerIdx.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: atx.Id(),
	}

	bytes, err := challenge.Hash()
	npst2 := nipst.NewNIPSTWithChallenge(bytes)
	view, err = b.mesh.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	activeSetSize, err := b.activeSet.ActiveSetSize(1)
	assert.NoError(t, err)
	act := types.NewActivationTxWithChallenge(challenge, activeSetSize, view, npst2, true)

	_, err = b.PublishActivationTx(layers.LatestLayer().GetEpoch(layersPerEpoch))
	assert.NoError(t, err)
	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
	activesetCache.Purge()
}

func TestBuilder_NoPrevATX(t *testing.T) {
	//todo: implement test
	id := types.NodeId{uuid.New().String(), []byte("bbbbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := MeshProviderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
	b := NewBuilder(id, coinbase, activationDb, net, ActiveSetProviderMock{}, layers, layersPerEpoch, &NipstBuilderMock{}, nil, func() bool { return true }, lg.WithName("atxBuilder"), )
	_, err := b.PublishActivationTx(2) // non genesis epoch
	assert.EqualError(t, err, "cannot find pos atx: cannot find pos atx id: leveldb: not found")
}

func TestBuilder_PublishActivationTx(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(id, coinbase, activationDb, net, ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"), )
	adb := b.db
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	npst := nipst.NewNIPSTWithChallenge(&chlng)

	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")},coinbase, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst, true )

	err := adb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Coinbase:       coinbase,
		Sequence:       b.GetLastSequence(b.nodeId) + 1,
		PrevATXId:      atx.Id(),
		PubLayerIdx:    atx.PubLayerIdx.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: atx.Id(),
	}

	bytes, err := challenge.Hash()
	npst2 := nipst.NewNIPSTWithChallenge(bytes)
	assert.NoError(t, err)

	//ugly hack to set view size in db
	view, err := layers.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	v, err := types.ViewAsBytes(view)
	assert.NoError(t, err)
	activesetCache.put(common.BytesToHash(v), 10)

	view, err = b.mesh.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	activeSetSize, err := b.activeSet.ActiveSetSize(1)
	assert.NoError(t, err)
	act := types.NewActivationTx(b.nodeId, coinbase, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), activeSetSize, view, npst2, true, )
	_, err = b.PublishActivationTx(1)
	assert.NoError(t, err)

	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.bt)
	assert.Equal(t, bytes, nipstBuilder.Challenge)

	//test publish 2 transaction in same epoch
	published, err := b.PublishActivationTx(2)
	assert.False(t, published)

	activationDb2 := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB2"))
	b = NewBuilder(id, coinbase, activationDb2, net, ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"), )
	b.nipstBuilder = &NipstErrBuilderMock{}
	_, err = b.PublishActivationTx(0)
	assert.Error(t, err)
	assert.EqualError(t, err, "cannot create nipst: error")

	activationDb3 := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB3"))
	bt := NewBuilder(id, coinbase, activationDb3, net, ActiveSetProviderMock{}, layers, layersPerEpoch, &NipstBuilderMock{}, nil, func() bool { return true }, lg.WithName("atxBuilder"), )
	_, err = bt.PublishActivationTx(2)
	assert.EqualError(t, err, "cannot find pos atx: cannot find pos atx id: leveldb: not found")
	activesetCache.Purge()
}

func TestBuilder_PublishActivationTxSerialize(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
	b := NewBuilder(id, coinbase, activationDb, net, ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"), )
	adb := b.db
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	challenge1 := common.HexToHash("0x222222")
	npst := nipst.NewNIPSTWithChallenge(&challenge1)

	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbb")}, coinbase, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst, true, )

	err := adb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	assert.NoError(t, err)

	view, err := b.mesh.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	activeSetSize, err := b.activeSet.ActiveSetSize(1)
	assert.NoError(t, err)
	act := types.NewActivationTx(b.nodeId, coinbase, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), activeSetSize, view, npst, true, )

	bt, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	a, err := types.BytesAsAtx(bt)
	assert.NoError(t, err)
	bt2, err := types.AtxAsBytes(a)
	assert.Equal(t, bt, bt2)
}
