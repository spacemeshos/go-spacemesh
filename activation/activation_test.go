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
	"github.com/stretchr/testify/require"
	"testing"
)

type ActiveSetProviderMock struct {
	i      uint32
	wasset bool
}

func (aspm *ActiveSetProviderMock) setActiveSetSize(i uint32) {
	aspm.wasset = true
	aspm.i = i
}

func (aspm *ActiveSetProviderMock) ActiveSetSize(epochId types.EpochId) (uint32, error) {
	if aspm.wasset {
		return aspm.i, nil
	}
	return 10, nil
}

type MeshProviderMock struct {
	GetOrphanBlocksBeforeFunc func(l types.LayerID) ([]types.BlockID, error)
	LatestLayerFunc           func() types.LayerID
}

func (mpm *MeshProviderMock) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	if mpm.GetOrphanBlocksBeforeFunc != nil {
		return mpm.GetOrphanBlocksBeforeFunc(l)
	}
	return []types.BlockID{1, 2, 3}, nil
}

func (mpm *MeshProviderMock) LatestLayer() types.LayerID {
	if mpm.LatestLayerFunc != nil {
		return mpm.LatestLayerFunc()
	}
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
	poetRef   []byte
}

func (np *NipstBuilderMock) SetPoetRef(ref []byte) {
	np.poetRef = ref
}

func (np *NipstBuilderMock) IsPostInitialized() bool {
	return true
}

func (np *NipstBuilderMock) InitializePost() (*types.PostProof, error) {
	return nil, nil
}

func (np *NipstBuilderMock) BuildNIPST(challenge *common.Hash) (*types.NIPST, error) {
	np.Challenge = challenge
	return nipst.NewNIPSTWithChallenge(challenge, np.poetRef), nil
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
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])

	net := &NetMock{}
	layers := &MeshProviderMock{}
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
	asProvider := ActiveSetProviderMock{}
	nipstBuilder := NipstBuilderMock{}

	view, err := layers.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	bt, err := types.ViewAsBytes(view)
	assert.NoError(t, err)
	activesetCache.put(common.BytesToHash(bt), 10)
	b := NewBuilder(id, coinbase, activationDb, net, &asProvider, layers, 10, &nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	poetRef := []byte{0xde, 0xad}
	nipstBuilder.SetPoetRef(poetRef)
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)
	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst)
	err = activationDb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	// calc expected result
	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Sequence:       b.GetLastSequence(b.nodeId) + 1,
		PrevATXId:      atx.Id(),
		PubLayerIdx:    atx.PubLayerIdx.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: atx.Id(),
	}
	bytes, err := challenge.Hash()
	npst2 := nipst.NewNIPSTWithChallenge(bytes, poetRef)
	view, err = b.mesh.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	activeSetSize, err := b.activeSet.ActiveSetSize(1)
	assert.NoError(t, err)
	act := types.NewActivationTxWithChallenge(challenge, coinbase, activeSetSize, view, npst2)
	assert.Equal(t, act.GetPoetProofRef(), npst2.PoetProofRef)

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
	layers := &MeshProviderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
	b := NewBuilder(id, coinbase, activationDb, net, &ActiveSetProviderMock{}, layers, layersPerEpoch, &NipstBuilderMock{}, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	_, err := b.PublishActivationTx(2) // non genesis epoch
	assert.EqualError(t, err, "cannot find pos atx: cannot find pos atx id: leveldb: not found")
}

func TestBuilder_PublishActivationTx(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(id, coinbase, activationDb, net, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipstBuilder.SetPoetRef(poetRef)
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)

	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst)

	err := activationDb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Sequence:       b.GetLastSequence(b.nodeId) + 1,
		PrevATXId:      atx.Id(),
		PubLayerIdx:    atx.PubLayerIdx.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: atx.Id(),
	}

	bytes, err := challenge.Hash()
	npst2 := nipst.NewNIPSTWithChallenge(bytes, poetRef)
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
	act := types.NewActivationTx(b.nodeId, coinbase, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), activeSetSize, view, npst2)
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
	b = NewBuilder(id, coinbase, activationDb2, net, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	b.nipstBuilder = &NipstErrBuilderMock{}
	_, err = b.PublishActivationTx(0)
	assert.Error(t, err)
	assert.EqualError(t, err, "cannot create nipst: error")

	activationDb3 := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB3"))
	bt := NewBuilder(id, coinbase, activationDb3, net, &ActiveSetProviderMock{}, layers, layersPerEpoch, &NipstBuilderMock{}, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	_, err = bt.PublishActivationTx(2)
	assert.EqualError(t, err, "cannot find pos atx: cannot find pos atx id: leveldb: not found")
	activesetCache.Purge()
}

func TestBuilder_PublishActivationTxActiveSetSize(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	layers.LatestLayer()
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(3)
	aspm := &ActiveSetProviderMock{}
	lg := log.NewDefault(id.Key[:5])
	dbmock := &ATXDBMock{}
	b := NewBuilder(id, coinbase, dbmock, net, aspm, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	chlng := common.HexToHash("0x3333")
	poetRef := []byte{0x66, 0x45}
	nipstBuilder.SetPoetRef(poetRef)
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)

	b.challenge = &types.NIPSTChallenge{
		PubLayerIdx: types.LayerID(1),
	}

	aspm.setActiveSetSize(0)

	layers.LatestLayerFunc = func() types.LayerID {
		return 0
	}

	b.nipst = npst

	dbmock.CalcActiveSetFromViewFunc = func(a *types.ActivationTx) (u uint32, e error) {
		return 30, nil
	}

	_, err := b.PublishActivationTx(types.EpochId(0))

	require.NoError(t, err)

	var atx types.ActivationTx
	require.NoError(t, types.BytesToInterface(net.bt, &atx))

	require.Equal(t, atx.ActiveSetSize, uint32(0))

	// not genesis should panic

	defer func() {
		err := recover()
		require.Equal(t, err, "active set size mismatch! size based on view: 30, size reported: 0")
	}()

	b.posLayerID = 6

	layers.LatestLayerFunc = func() types.LayerID {
		return 7 // after genesis
	}

	b.nipst = npst
	b.challenge = &types.NIPSTChallenge{
		PubLayerIdx: types.LayerID(8),
	}
	_, err = b.PublishActivationTx(types.EpochId(2))
}

func TestBuilder_PublishActivationTxSerialize(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(log.NewDefault("")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
	b := NewBuilder(id, coinbase, activationDb, net, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	challenge1 := common.HexToHash("0x222222")
	poetRef := []byte{0xba, 0xbe}
	npst := nipst.NewNIPSTWithChallenge(&challenge1, poetRef)

	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbb")}, coinbase, 1, prevAtx, 5, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst)

	err := activationDb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	assert.NoError(t, err)

	view, err := b.mesh.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	activeSetSize, err := b.activeSet.ActiveSetSize(1)
	assert.NoError(t, err)
	act := types.NewActivationTx(b.nodeId, coinbase, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), activeSetSize, view, npst)

	bt, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	a, err := types.BytesAsAtx(bt)
	assert.NoError(t, err)
	bt2, err := types.AtxAsBytes(a)
	assert.Equal(t, bt, bt2)
}

func TestBuilder_PublishActivationTx_PosAtxOnSameLayerAsPrevAtx(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	activationDb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(id, coinbase, activationDb, net, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, lg.WithName("atxBuilder"))
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipstBuilder.SetPoetRef(poetRef)
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)

	for i := 3; i < 6; i++ {
		atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase, 1, prevAtx, types.LayerID(i), 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst)
		err := activationDb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
		assert.NoError(t, err)
	}

	prevATX := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase, 1, prevAtx, types.LayerID(6), 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst)
	b.prevATX = prevATX

	//ugly hack to set view size in db
	view, err := layers.GetOrphanBlocksBefore(layers.LatestLayer())
	assert.NoError(t, err)
	v, err := types.ViewAsBytes(view)
	assert.NoError(t, err)
	activesetCache.put(common.BytesToHash(v), 10)

	_, err = b.PublishActivationTx(0)
	assert.NoError(t, err)

	newAtx, err := types.BytesAsAtx(net.bt)
	assert.NoError(t, err)
	posAtx, err := activationDb.GetAtx(newAtx.PositioningAtx)
	assert.NoError(t, err)
	assert.Equal(t, prevATX.Id(), newAtx.PrevATXId)

	// check atx.PubLayerId - posAtx.PubLayerId = number of layers per epoch
	assert.Equal(t, types.LayerID(layersPerEpoch), newAtx.PubLayerIdx-posAtx.PubLayerIdx)

	// check pos & prev has the same PubLayerIdx
	assert.Equal(t, prevATX.PubLayerIdx, posAtx.PubLayerIdx)
}
