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
	return nipst.NewNIPSTWithChallenge(challenge, poetRef), nil
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

// ========== Helper functions ==========

func newActivationDb() *ActivationDb {
	return NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIdStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
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
			View:           View,
		},
		Nipst: nipst,
	}
	return activationTx
}

func newBuilder(activationDb ATXDBProvider) *Builder {
	return NewBuilder(nodeId, coinbase, activationDb, net, &ActiveSetProviderMock{}, meshProvider, layersPerEpoch, nipstBuilder, nil, isSynced(true), lg.WithName("atxBuilder"))
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

func assertLastAtx(r *require.Assertions, posAtx, prevAtx *types.ActivationTx, layersPerEpoch uint16) {
	atx, err := types.BytesAsAtx(net.lastTransmission)
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
	_, err = b.PublishActivationTx(clockEpoch)
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
	assertLastAtx(r, prevAtx, prevAtx, layersPerEpoch)
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
	assertLastAtx(r, posAtx, nil, layersPerEpoch)
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
	assertLastAtx(r, prevAtx, prevAtx, layersPerEpoch)

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
		nipstBuilder, nil, isSynced(true), lg.WithName("atxBuilder"))

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
	a, err := types.BytesAsAtx(bt)
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
	b.prevATX = prevATX

	published, err := publishAtx(b, postGenesisEpochLayer+4, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)

	newAtx := lastTransmittedAtx(t)
	r.Equal(prevATX.Id(), newAtx.PrevATXId)

	posAtx, err := activationDb.GetAtx(newAtx.PositioningAtx)
	r.NoError(err)

	assertLastAtx(r, posAtx, prevATX, layersPerEpoch)

	t.Skip("proves https://github.com/spacemeshos/go-spacemesh/issues/1166")
	// check pos & prev has the same PubLayerIdx
	r.Equal(prevATX.PubLayerIdx, posAtx.PubLayerIdx)
}
