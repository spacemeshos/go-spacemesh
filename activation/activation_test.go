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
	defaultActiveSetSize = 10
	layersPerEpoch       = uint16(10)
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
	meshProvider = &MeshProviderMock{}
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
	LatestLayerFunc           func() types.LayerID
}

func (mpm *MeshProviderMock) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	if mpm.GetOrphanBlocksBeforeFunc != nil {
		return mpm.GetOrphanBlocksBeforeFunc(l)
	}
	return defaultView, nil
}

func (mpm *MeshProviderMock) LatestLayer() types.LayerID {
	if mpm.LatestLayerFunc != nil {
		return mpm.LatestLayerFunc()
	}
	return 12
}

type NetMock struct {
	lastTransmission []byte
}

func (n *NetMock) Broadcast(id string, d []byte) error {
	n.lastTransmission = d
	return nil
}

type NipstBuilderMock struct {
	Challenge *common.Hash
	poetRef   []byte
}

func (np *NipstBuilderMock) IsPostInitialized() bool {
	return true
}

func (np *NipstBuilderMock) InitializePost() (*types.PostProof, error) {
	return nil, nil
}

func (np *NipstBuilderMock) BuildNIPST(challenge *common.Hash) (*types.NIPST, error) {
	np.Challenge = challenge
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

func newBuilder(activationDb *ActivationDb) *Builder {
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

// ========== Tests ==========

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)

	prevAtx := newAtx(newChallenge(nodeId, 1, prevAtxId, prevAtxId, 5), 5, []types.BlockID{1, 2, 3}, npst)
	err := activationDb.StoreAtx(prevAtx.PubLayerIdx.GetEpoch(layersPerEpoch), prevAtx)
	r.NoError(err)

	// create and publish ATX
	_, err = b.PublishActivationTx(meshProvider.LatestLayer().GetEpoch(layersPerEpoch))
	r.NoError(err)

	// assert that last transmission matches the ATX we expect
	challenge := newChallenge(nodeId, prevAtx.Sequence+1, prevAtx.Id(), prevAtx.Id(), prevAtx.PubLayerIdx.Add(b.layersPerEpoch))
	challengeHash, err := challenge.Hash()
	r.NoError(err)
	newNipst := nipst.NewNIPSTWithChallenge(challengeHash, poetRef)

	expectedAtxBytes, err := types.AtxAsBytes(newAtx(challenge, defaultActiveSetSize, defaultView, newNipst))
	r.NoError(err)
	r.Equal(expectedAtxBytes, net.lastTransmission)

	// assert that the challenge hash was set in the nipst builder
	r.Equal(challengeHash, nipstBuilder.Challenge)

	// cleanup
	activesetCache.Purge()
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)

	challenge := newChallenge(otherNodeId, 1, prevAtxId, prevAtxId, 5)
	posAtx := newAtx(challenge, 5, []types.BlockID{1, 2, 3}, npst)
	err := activationDb.StoreAtx(2, posAtx)
	r.NoError(err)

	// create and publish ATX
	_, err = b.PublishActivationTx(2) // non genesis epoch
	r.NoError(err)

	// assert that last transmission matches the ATX we expect
	challenge = newChallenge(nodeId, 0, *types.EmptyAtxId, posAtx.Id(), posAtx.PubLayerIdx.Add(b.layersPerEpoch))
	challengeHash, err := challenge.Hash()
	r.NoError(err)
	newNipst := nipst.NewNIPSTWithChallenge(challengeHash, poetRef)

	expectedAtxBytes, err := types.AtxAsBytes(newAtx(challenge, defaultActiveSetSize, defaultView, newNipst))
	r.NoError(err)
	r.Equal(expectedAtxBytes, net.lastTransmission)
}

func TestBuilder_PublishActivationTx_FailsWhenNoPosAtx(t *testing.T) {
	b := newBuilder(newActivationDb())

	_, err := b.PublishActivationTx(2)
	assert.EqualError(t, err, "cannot find pos atx: cannot find pos atx id: leveldb: not found")
}

func TestBuilder_PublishActivationTx_DoesNotPublish2AtxsInSameEpoch(t *testing.T) {
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	atx := types.NewActivationTx(nodeId, coinbase, 1, prevAtxId, 5, 1, prevAtxId, 5, []types.BlockID{1, 2, 3}, npst)
	err := activationDb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	setActivesetSizeInCache(t, 10)

	published, err := b.PublishActivationTx(1)
	assert.NoError(t, err)
	assert.True(t, published)

	published, err = b.PublishActivationTx(1)
	assert.NoError(t, err)
	assert.False(t, published)
}

func TestBuilder_PublishActivationTx_FailsWhenNipstBuilderFails(t *testing.T) {
	activationDb := newActivationDb()
	nipstBuilder := &NipstErrBuilderMock{} // mock that returns error from BuildNipst()
	b := NewBuilder(nodeId, coinbase, activationDb, net, &ActiveSetProviderMock{}, meshProvider, layersPerEpoch, nipstBuilder, nil, isSynced(true), lg.WithName("atxBuilder"))

	_, err := b.PublishActivationTx(0)
	assert.EqualError(t, err, "cannot create nipst: nipst builder error")
}

func TestBuilder_PublishActivationTx_ActiveSetSizeMismatchPanics(t *testing.T) {
	r := require.New(t)
	// setup
	asProvider := &ActiveSetProviderMock{}
	asProvider.setActiveSetSize(0)

	atxDb := &ATXDBMock{}
	atxDb.CalcActiveSetFromViewFunc = func(a *types.ActivationTx) (u uint32, e error) { return 30, nil }

	meshProvider.LatestLayerFunc = func() types.LayerID { return 0 }

	b := NewBuilder(nodeId, coinbase, atxDb, net, asProvider, meshProvider, layersPerEpoch, nipstBuilder, nil, isSynced(true), lg.WithName("atxBuilder"))
	b.challenge = &types.NIPSTChallenge{PubLayerIdx: types.LayerID(1)}
	b.nipst = npst

	_, err := b.PublishActivationTx(types.EpochId(0))
	r.NoError(err)

	// assert that the active set size of the published ATX is zero
	atx := lastTransmittedAtx(t)
	r.Zero(atx.ActiveSetSize)

	lastGenesisLayer := types.LayerID(layersPerEpoch * 2)
	b.posLayerID = lastGenesisLayer

	meshProvider.LatestLayerFunc = func() types.LayerID {
		return lastGenesisLayer.Add(1)
	}

	b.nipst = npst
	b.challenge = &types.NIPSTChallenge{
		PubLayerIdx: lastGenesisLayer.Add(2),
	}

	// not genesis, should panic
	r.PanicsWithValue("active set size mismatch! size based on view: 30, size reported: 0", func() {
		_, _ = b.PublishActivationTx(2)
	})
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	atx := types.NewActivationTx(nodeId, coinbase, 1, prevAtxId, 5, 1, prevAtxId, 5, []types.BlockID{1, 2, 3}, npst)

	err := activationDb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
	assert.NoError(t, err)

	assert.NoError(t, err)

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
	t.Skip("proves https://github.com/spacemeshos/go-spacemesh/issues/1166")
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	for i := 3; i < 6; i++ {
		atx := types.NewActivationTx(nodeId, coinbase, 1, prevAtxId, types.LayerID(i), 1, prevAtxId, 5, []types.BlockID{1, 2, 3}, npst)
		err := activationDb.StoreAtx(atx.PubLayerIdx.GetEpoch(layersPerEpoch), atx)
		assert.NoError(t, err)
	}

	prevATX := types.NewActivationTx(nodeId, coinbase, 1, prevAtxId, types.LayerID(6), 1, prevAtxId, 5, []types.BlockID{1, 2, 3}, npst)
	b.prevATX = prevATX

	setActivesetSizeInCache(t, 10)

	_, err := b.PublishActivationTx(0)
	assert.NoError(t, err)

	newAtx, err := types.BytesAsAtx(net.lastTransmission)
	assert.NoError(t, err)
	posAtx, err := activationDb.GetAtx(newAtx.PositioningAtx)
	assert.NoError(t, err)
	assert.Equal(t, prevATX.Id(), newAtx.PrevATXId)

	// check atx.PubLayerId - posAtx.PubLayerId = number of layers per epoch
	assert.Equal(t, types.LayerID(layersPerEpoch), newAtx.PubLayerIdx-posAtx.PubLayerIdx)

	// check pos & prev has the same PubLayerIdx
	assert.Equal(t, prevATX.PubLayerIdx, posAtx.PubLayerIdx)
}
