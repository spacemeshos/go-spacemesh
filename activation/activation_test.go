package activation

import (
	"fmt"
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

// ========== Vars / Consts ==========

const (
	defaultActiveSetSize  = uint32(10)
	layersPerEpoch        = 10
	postGenesisEpoch      = 2
	postGenesisEpochLayer = 22
	defaultMeshLayer      = 12
)

var (
	nodeId       = types.NodeId{Key: "11111", VRFPublicKey: []byte("22222")}
	otherNodeId  = types.NodeId{Key: "00000", VRFPublicKey: []byte("00000")}
	coinbase     = address.HexToAddress("33333")
	prevAtxId    = types.AtxId{Hash: common.HexToHash("44444")}
	chlng        = common.HexToHash("55555")
	poetRef      = []byte("66666")
	defaultView  = []types.BlockID{1, 2, 3}
	net          = &NetMock{}
	meshProvider = &MeshProviderMock{latestLayer: 12}
	nipstBuilder = &NipstBuilderMock{}
	npst         = nipst.NewNIPSTWithChallenge(&chlng, poetRef)
	lg           = log.NewDefault(nodeId.Key[:5])
)

// ========== Mocks ==========

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
	return defaultActiveSetSize, nil
}

type MeshProviderMock struct {
	GetOrphanBlocksBeforeFunc func(l types.LayerID) ([]types.BlockID, error)
	latestLayer               types.LayerID
}

func (mpm *MeshProviderMock) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	if mpm.GetOrphanBlocksBeforeFunc != nil {
		return mpm.GetOrphanBlocksBeforeFunc(l)
	}
	return defaultView, nil
}

func (mpm *MeshProviderMock) LatestLayer() types.LayerID {
	layer := mpm.latestLayer
	mpm.latestLayer = defaultMeshLayer
	return layer
}

type NetMock struct {
	lastTransmission []byte
}

func (n *NetMock) Broadcast(id string, d []byte) error {
	n.lastTransmission = d
	return nil
}

type NipstBuilderMock struct {
	poetRef        []byte
	buildNipstFunc func(challenge *common.Hash) (*types.NIPST, error)
}

func (np *NipstBuilderMock) IsPostInitialized() bool {
	return true
}

func (np *NipstBuilderMock) InitializePost() (*types.PostProof, error) {
	return nil, nil
}

func (np *NipstBuilderMock) BuildNIPST(challenge *common.Hash) (*types.NIPST, error) {
	if np.buildNipstFunc != nil {
		return np.buildNipstFunc(challenge)
	}
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
	return nil, fmt.Errorf("nipst builder error")
}

type MockIdStore struct{}

func (*MockIdStore) StoreNodeIdentity(id types.NodeId) error {
	return nil
}

func (*MockIdStore) GetIdentity(id string) (types.NodeId, error) {
	return types.NodeId{}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(nipst *types.NIPST, expectedChallenge common.Hash) error {
	return nil
}

func NewMockDB() *MockDB {
	return &MockDB{
		make(map[string][]byte),
		false,
	}
}

type MockDB struct {
	mp      map[string][]byte
	hadNone bool
}

func (m *MockDB) Put(key, val []byte) error {
	if len(val) == 0 {
		m.hadNone = true
	}
	m.mp[common.Bytes2Hex(key)] = val
	return nil
}

func (m *MockDB) Get(key []byte) ([]byte, error) {
	return m.mp[common.Bytes2Hex(key)], nil
}

type FaultyNetMock struct {
	bt []byte
}

func (n *FaultyNetMock) Broadcast(id string, d []byte) error {
	n.bt = d
	return fmt.Errorf(" I'm faulty")
}

// ========== Helper functions ==========

func newActivationDb() *ActivationDb {
	return NewActivationDb(database.NewMemDatabase(), &MockIdStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
}

func isSynced(b bool) func() bool {
	return func() bool {
		return b
	}
}

func newChallenge(nodeId types.NodeId, sequence uint64, prevAtxId, posAtxId types.AtxId, pubLayerId types.LayerID) types.NIPSTChallenge {
	challenge := types.NIPSTChallenge{
		NodeId:         nodeId,
		Sequence:       sequence,
		PrevATXId:      prevAtxId,
		PubLayerIdx:    pubLayerId,
		PositioningAtx: posAtxId,
	}
	return challenge
}

func newAtx(challenge types.NIPSTChallenge, ActiveSetSize uint32, View []types.BlockID, nipst *types.NIPST) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		ActivationTxHeader: types.ActivationTxHeader{
			NIPSTChallenge: challenge,
			Coinbase:       coinbase,
			ActiveSetSize:  ActiveSetSize,
		},
		Nipst: nipst,
		View:  View,
	}
	activationTx.CalcAndSetId()
	return activationTx
}

func newBuilder(activationDb ATXDBProvider) *Builder {
	return NewBuilder(nodeId, coinbase, activationDb, net, &ActiveSetProviderMock{}, meshProvider, layersPerEpoch, nipstBuilder, nil, isSynced(true), NewMockDB(), lg.WithName("atxBuilder"))
}

func setActivesetSizeInCache(t *testing.T, activesetSize uint32) {
	view, err := meshProvider.GetOrphanBlocksBefore(meshProvider.LatestLayer())
	assert.NoError(t, err)
	v, err := types.ViewAsBytes(view)
	assert.NoError(t, err)
	activesetCache.put(common.BytesToHash(v), activesetSize)
}

func lastTransmittedAtx(t *testing.T) (atx types.ActivationTx) {
	err := types.BytesToInterface(net.lastTransmission, &atx)
	require.NoError(t, err)
	return atx
}

func assertLastAtx(r *require.Assertions, posAtx, prevAtx *types.ActivationTxHeader, layersPerEpoch uint16) {
	atx, err := types.BytesAsAtx(net.lastTransmission, nil)
	r.NoError(err)

	r.Equal(nodeId, atx.NodeId)
	if prevAtx != nil {
		r.Equal(prevAtx.Sequence+1, atx.Sequence)
		r.Equal(prevAtx.Id(), atx.PrevATXId)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(*types.EmptyAtxId, atx.PrevATXId)
	}
	r.Equal(posAtx.Id(), atx.PositioningAtx)
	r.Equal(posAtx.PubLayerIdx.Add(layersPerEpoch), atx.PubLayerIdx)
	r.Equal(defaultActiveSetSize, atx.ActiveSetSize)
	r.Equal(defaultView, atx.View)
	r.Equal(poetRef, atx.GetPoetProofRef())
}

func storeAtx(r *require.Assertions, activationDb *ActivationDb, atx *types.ActivationTx) {
	epoch := atx.PubLayerIdx.GetEpoch(layersPerEpoch)
	log.Info("stored ATX in epoch %v", epoch)
	err := activationDb.StoreAtx(epoch, atx)
	r.NoError(err)
}

func publishAtx(b *Builder, meshLayer types.LayerID, clockEpoch types.EpochId, buildNipstLayerDuration uint16) (published bool, err error) {
	net.lastTransmission = nil
	meshProvider.latestLayer = meshLayer
	nipstBuilder.buildNipstFunc = func(challenge *common.Hash) (*types.NIPST, error) {
		meshProvider.latestLayer = meshLayer.Add(buildNipstLayerDuration)
		return nipst.NewNIPSTWithChallenge(challenge, poetRef), nil
	}
	err = b.PublishActivationTx(clockEpoch)
	nipstBuilder.buildNipstFunc = nil
	return net.lastTransmission != nil, err
}

// ========== Tests ==========

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(nodeId, 1, prevAtxId, prevAtxId, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, prevAtx)

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, &prevAtx.ActivationTxHeader, &prevAtx.ActivationTxHeader, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(otherNodeId /*👀*/, 1, prevAtxId, prevAtxId, postGenesisEpochLayer)
	posAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, posAtx)

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, &posAtx.ActivationTxHeader, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_FailsWhenNoPosAtx(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(otherNodeId /*👀*/, 1, prevAtxId, prevAtxId, postGenesisEpochLayer-layersPerEpoch /*👀*/)
	posAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, posAtx)

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "cannot find pos atx in epoch 2: cannot find pos atx id: leveldb: not found")
	r.False(published)
}

func TestBuilder_PublishActivationTx_FailsWhenNoPosAtxButPrevAtxFromWrongEpochExists(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(nodeId, 1, prevAtxId, prevAtxId, postGenesisEpochLayer-layersPerEpoch /*👀*/)
	prevAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, prevAtx)

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "cannot find pos atx in epoch 2: cannot find pos atx id: leveldb: not found")
	r.False(published)
}

func TestBuilder_PublishActivationTx_DoesNotPublish2AtxsInSameEpoch(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(nodeId, 1, prevAtxId, prevAtxId, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, prevAtx)

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, &prevAtx.ActivationTxHeader, &prevAtx.ActivationTxHeader, layersPerEpoch)

	// assert that another ATX cannot be published
	published, err = publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch) // 👀
	r.NoError(err)
	r.False(published)
}

func TestBuilder_PublishActivationTx_FailsWhenNipstBuilderFails(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	nipstBuilder := &NipstErrBuilderMock{} // 👀 mock that returns error from BuildNipst()
	b := NewBuilder(nodeId, coinbase, activationDb, net, &ActiveSetProviderMock{}, meshProvider, layersPerEpoch,
		nipstBuilder, nil, isSynced(true), NewMockDB(), lg.WithName("atxBuilder"))

	challenge := newChallenge(otherNodeId /*👀*/, 1, prevAtxId, prevAtxId, postGenesisEpochLayer)
	posAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, posAtx)

	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "cannot create nipst: nipst builder error")
	r.False(published)
}

func TestBuilder_PublishActivationTx_ActiveSetSizeMismatchPanics(t *testing.T) {
	r := require.New(t)

	// setup
	atxDb := &ATXDBMock{}
	atxDb.activeSet = 30

	b := newBuilder(atxDb)
	b.activeSet.(*ActiveSetProviderMock).setActiveSetSize(0)

	b.challenge = &types.NIPSTChallenge{PubLayerIdx: 1}
	b.nipst = npst

	published, err := publishAtx(b, postGenesisEpochLayer, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)

	// assert that the active set size of the published ATX is zero
	atx := lastTransmittedAtx(t)
	r.Zero(atx.ActiveSetSize)

	b.challenge = &types.NIPSTChallenge{PubLayerIdx: postGenesisEpochLayer}
	b.nipst = npst

	// not genesis, should panic
	r.PanicsWithValue("active set size mismatch! size based on view: 30, size reported: 0", func() {
		_, _ = publishAtx(b, postGenesisEpochLayer, postGenesisEpoch, layersPerEpoch)
	})
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	atx := types.NewActivationTx(nodeId, coinbase, 1, prevAtxId, 5, 1, prevAtxId, 5, []types.BlockID{1, 2, 3}, npst)
	storeAtx(r, activationDb, atx)

	view, err := b.mesh.GetOrphanBlocksBefore(meshProvider.LatestLayer())
	assert.NoError(t, err)
	activeSetSize, err := b.activeSet.ActiveSetSize(1)
	assert.NoError(t, err)
	act := types.NewActivationTx(b.nodeId, coinbase, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), activeSetSize, view, npst)

	bt, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	a, err := types.BytesAsAtx(bt, nil)
	assert.NoError(t, err)
	bt2, err := types.AtxAsBytes(a)
	assert.Equal(t, bt, bt2)
}

func TestBuilder_PublishActivationTx_PosAtxOnSameLayerAsPrevAtx(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, 10)
	defer activesetCache.Purge()

	for i := postGenesisEpochLayer; i < postGenesisEpochLayer+3; i++ {
		challenge := newChallenge(nodeId, 1, prevAtxId, prevAtxId, types.LayerID(i))
		atx := newAtx(challenge, 5, defaultView, npst)
		storeAtx(r, activationDb, atx)
	}

	challenge := newChallenge(nodeId, 1, prevAtxId, prevAtxId, postGenesisEpochLayer+3)
	prevATX := newAtx(challenge, 5, defaultView, npst)
	b.prevATX = &prevATX.ActivationTxHeader

	published, err := publishAtx(b, postGenesisEpochLayer+4, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)

	newAtx := lastTransmittedAtx(t)
	r.Equal(prevATX.Id(), newAtx.PrevATXId)

	posAtx, err := activationDb.GetAtx(newAtx.PositioningAtx)
	r.NoError(err)

	assertLastAtx(r, posAtx, &prevATX.ActivationTxHeader, layersPerEpoch)

	t.Skip("proves https://github.com/spacemeshos/go-spacemesh/issues/1166")
	// check pos & prev has the same PubLayerIdx
	r.Equal(prevATX.PubLayerIdx, posAtx.PubLayerIdx)
}

func TestBuilder_NipstPublishRecovery(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
	coinbase := address.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	db := NewMockDB()
	activationDb := NewActivationDb(database.NewMemDatabase(), &MockIdStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(id, coinbase, activationDb, &FaultyNetMock{}, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	prevAtx := types.AtxId{Hash: common.HexToHash("0x111")}
	chlng := common.HexToHash("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipstBuilder.poetRef = poetRef
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)

	atx := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase, 1, prevAtx, 15, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst)

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
	err = b.PublishActivationTx(1)
	assert.Error(t, err)

	//test load in correct epoch
	b = NewBuilder(id, coinbase, activationDb, net, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layers.latestLayer = 22
	err = b.PublishActivationTx(1)
	assert.NoError(t, err)
	bts, err := types.AtxAsBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.lastTransmission)

	b = NewBuilder(id, coinbase, activationDb, &FaultyNetMock{}, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	err = b.PublishActivationTx(1)
	assert.Error(t, err)
	db.hadNone = false
	//test load challenge in later epoch - nipst should be truncated
	b = NewBuilder(id, coinbase, activationDb, &FaultyNetMock{}, &ActiveSetProviderMock{}, layers, layersPerEpoch, nipstBuilder, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	assert.Error(t, err)
	err = b.loadChallenge()
	assert.NoError(t, err)
	err = b.PublishActivationTx(3)
	assert.True(t, db.hadNone)

}
