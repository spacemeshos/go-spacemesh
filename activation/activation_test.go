package activation

import (
	"bytes"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sort"
	"sync"
	"testing"
	"time"
)

// ========== Vars / Consts ==========

const (
	defaultActiveSetSize  = uint32(10)
	layersPerEpoch        = 10
	postGenesisEpoch      = 2
	postGenesisEpochLayer = 22
	defaultMeshLayer      = 12
)

func init() {
	types.SetLayersPerEpoch(layersPerEpoch)
}

var (
	pub, _, _   = ed25519.GenerateKey(nil)
	nodeID      = types.NodeID{Key: util.Bytes2Hex(pub), VRFPublicKey: []byte("22222")}
	otherNodeID = types.NodeID{Key: "00000", VRFPublicKey: []byte("00000")}
	coinbase    = types.HexToAddress("33333")
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	chlng       = types.HexToHash32("55555")
	poetRef     = []byte("66666")
	block1      = types.NewExistingBlock(0, []byte("11111"))
	block2      = types.NewExistingBlock(0, []byte("22222"))
	block3      = types.NewExistingBlock(0, []byte("33333"))

	defaultView       = []types.BlockID{block1.ID(), block2.ID(), block3.ID()}
	net               = &NetMock{}
	layerClockMock    = &LayerClockMock{}
	meshProviderMock  = &MeshProviderMock{latestLayer: 12}
	nipostBuilderMock = &NIPoSTBuilderMock{}
	nipost            = NewNIPoSTWithChallenge(&chlng, poetRef)
	initialPoST       = &types.PoST{
		Nonce:   0,
		Indices: make([]byte, 10),
	}
	lg = log.NewDefault(nodeID.Key[:5])
)

// ========== Mocks ==========

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
	atxDb            atxDBProvider
}

func (n *NetMock) Broadcast(_ string, d []byte) error {
	n.lastTransmission = d
	go n.hookToAtxPool(d)
	return nil
}

func (n *NetMock) hookToAtxPool(transmission []byte) {
	if atx, err := types.BytesToAtx(transmission); err == nil {
		atx.CalcAndSetID()

		if n.atxDb != nil {
			if atxDb, ok := n.atxDb.(*DB); ok {
				err := atxDb.StoreAtx(atx.PubLayerID.GetEpoch(), atx)
				if err != nil {
					panic(err)
				}
			}
		}
	}
}

type MockSigning struct {
}

func (ms *MockSigning) Sign(m []byte) []byte {
	return m
}

type NIPoSTBuilderMock struct {
	poetRef         []byte
	buildNipostFunc func(challenge *types.Hash32) (*types.NIPoST, error)
	initPostFunc    func(logicalDrive string, commitmentSize uint64) (*types.PoST, error)
	SleepTime       int
}

func (np *NIPoSTBuilderMock) BuildNIPoST(challenge *types.Hash32, _ chan struct{}, _ chan struct{}) (*types.NIPoST, error) {
	if np.buildNipostFunc != nil {
		return np.buildNipostFunc(challenge)
	}
	return NewNIPoSTWithChallenge(challenge, np.poetRef), nil
}

type NIPoSTErrBuilderMock struct{}

func (np *NIPoSTErrBuilderMock) BuildNIPoST(*types.Hash32, chan struct{}, chan struct{}) (*types.NIPoST, error) {
	return nil, fmt.Errorf("nipost builder error")
}

type MockIDStore struct{}

func (*MockIDStore) StoreNodeIdentity(types.NodeID) error {
	return nil
}

func (*MockIDStore) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(signing.PublicKey, *types.NIPoST, types.Hash32) error {
	return nil
}

func (*ValidatorMock) ValidatePoST(id []byte, PoST *types.PoST, PoSTMetadata *types.PoSTMetadata) error {
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
	m.mp[util.Bytes2Hex(key)] = val
	return nil
}

func (m *MockDB) Get(key []byte) ([]byte, error) {
	return m.mp[util.Bytes2Hex(key)], nil
}

type FaultyNetMock struct {
	bt     []byte
	retErr bool
}

func (n *FaultyNetMock) Broadcast(_ string, d []byte) error {
	n.bt = d
	if n.retErr {
		return fmt.Errorf("faulty")
	}
	// not calling `go hookToAtxPool(d)`
	return nil
}

// ========== Helper functions ==========

func newActivationDb() *DB {
	return NewDB(database.NewMemDatabase(), &MockIDStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
}

func newChallenge(nodeID types.NodeID, sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID) types.NIPoSTChallenge {
	challenge := types.NIPoSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
	}
	return challenge
}

func newAtx(challenge types.NIPoSTChallenge, nipost *types.NIPoST) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: &types.ActivationTxHeader{
				NIPoSTChallenge: challenge,
				Coinbase:        coinbase,
			},
			NIPoST: nipost,
		},
	}
	activationTx.CalcAndSetID()
	return activationTx
}

type LayerClockMock struct {
	currentLayer types.LayerID
}

func (l *LayerClockMock) GetCurrentLayer() types.LayerID {
	return l.currentLayer
}

func (l *LayerClockMock) AwaitLayer(types.LayerID) chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(1 * time.Millisecond)
		close(ch)
	}()
	return ch
}

type mockSyncer struct{}

func (m *mockSyncer) Await() chan struct{} { return closedChan }

func newBuilder(activationDb atxDBProvider) *Builder {
	net.atxDb = activationDb
	b := NewBuilder(nodeID, &MockSigning{}, activationDb, net, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	b.initialPoST = initialPoST
	return b
}

func setActivesetSizeInCache(t *testing.T, activesetSize uint32) {
	view, err := meshProviderMock.GetOrphanBlocksBefore(meshProviderMock.LatestLayer())
	assert.NoError(t, err)
	sort.Slice(view, func(i, j int) bool {
		return bytes.Compare(view[i].Bytes(), view[j].Bytes()) < 0
	})
	h := types.CalcBlocksHash12(view)
	activesetCache.Add(h, activesetSize)
}

func lastTransmittedAtx(t *testing.T) types.ActivationTx {
	var signedAtx types.ActivationTx
	err := types.BytesToInterface(net.lastTransmission, &signedAtx)
	require.NoError(t, err)
	return signedAtx
}

func assertLastAtx(r *require.Assertions, posAtx, prevAtx *types.ActivationTxHeader, layersPerEpoch uint16) {
	sigAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)

	atx := sigAtx
	r.Equal(nodeID, atx.NodeID)
	if prevAtx != nil {
		r.Equal(prevAtx.Sequence+1, atx.Sequence)
		r.Equal(prevAtx.ID(), atx.PrevATXID)
		r.Nil(atx.InitialPoST)
		r.Nil(atx.InitialPoSTIndices)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(*types.EmptyATXID, atx.PrevATXID)
		r.NotNil(atx.InitialPoST)
		r.NotNil(atx.InitialPoSTIndices)
	}
	r.Equal(posAtx.ID(), atx.PositioningATX)
	r.Equal(posAtx.PubLayerID.Add(layersPerEpoch), atx.PubLayerID)
	r.Equal(poetRef, atx.GetPoetProofRef())
}

func storeAtx(r *require.Assertions, activationDb *DB, atx *types.ActivationTx, lg log.Log) {
	epoch := atx.PubLayerID.GetEpoch()
	lg.Info("stored ATX in epoch %v", epoch)
	err := activationDb.StoreAtx(epoch, atx)
	r.NoError(err)
}

func publishAtx(b *Builder, meshLayer types.LayerID, clockEpoch types.EpochID, buildNipostLayerDuration uint16) (published, builtNipost bool, err error) {
	net.lastTransmission = nil
	meshProviderMock.latestLayer = meshLayer
	nipostBuilderMock.buildNipostFunc = func(challenge *types.Hash32) (*types.NIPoST, error) {
		builtNipost = true
		meshProviderMock.latestLayer = meshLayer.Add(buildNipostLayerDuration)
		layerClockMock.currentLayer = layerClockMock.currentLayer.Add(buildNipostLayerDuration)
		return NewNIPoSTWithChallenge(challenge, poetRef), nil
	}
	layerClockMock.currentLayer = clockEpoch.FirstLayer() + 3
	err = b.PublishActivationTx()
	nipostBuilderMock.buildNipostFunc = nil
	return net.lastTransmission != nil, builtNipost, err
}

// ========== Tests ==========

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, prevAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)

	// create and publish another ATX
	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	publishedAtx.CalcAndSetID()
	published, _, err = publishAtx(b, postGenesisEpochLayer+layersPerEpoch+1, postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, publishedAtx.ActivationTxHeader, publishedAtx.ActivationTxHeader, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	r := require.New(t)

	// setup
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()
	activationDb := newActivationDb()
	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(nodeID, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to broadcast ATX: faulty")
	r.False(published)

	// create and attempt to publish ATX
	faultyNet.retErr = false
	b = NewBuilder(nodeID, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, builtNipost, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "target epoch has passed")
	r.False(published)
	r.True(builtNipost)

	// if the network works and we try to publish a new ATX, the timeout should result in a clean state (so a NIPoST should be built)
	b.net = net
	net.atxDb = activationDb
	posAtx := newAtx(newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer+layersPerEpoch+1), nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))
	published, builtNipost, err = publishAtx(b, postGenesisEpochLayer+layersPerEpoch+2, postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNipost)
}

func TestBuilder_PublishActivationTx_RebuildNIPoSTWhenTargetEpochPassed(t *testing.T) {
	r := require.New(t)

	// setup
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()
	activationDb := newActivationDb()
	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(nodeID, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, builtNIPoST, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to broadcast ATX: faulty")
	r.False(published)
	r.True(builtNIPoST)

	// We started building the NIPoST in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the target epoch (4) has passed, so we'll start the ATX builder in epoch 5 and ensure it builds a
	// new NIPoST.

	// if the network works - the ATX should be published
	b.net = net
	net.atxDb = activationDb
	posAtx := newAtx(newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer+(3*layersPerEpoch)), nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))
	published, builtNIPoST, err = publishAtx(b, postGenesisEpochLayer+(3*layersPerEpoch)+1, postGenesisEpoch+3, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNIPoST)
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_PrevATXWithoutPrevATX(t *testing.T) {
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	challenge = newChallenge(nodeID /*👀*/, 0, *types.EmptyATXID, posAtx.ID(), postGenesisEpochLayer)
	challenge.InitialPoSTIndices = initialPoST.Indices
	prevAtx := newAtx(challenge, nipost)
	prevAtx.InitialPoST = initialPoST
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_TargetsEpochBasedOnPosAtx(t *testing.T) {
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer-layersPerEpoch /*👀*/)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	// create and publish ATX based on the best available posAtx, as long as the node is synced
	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_DoesNotPublish2AtxsInSameEpoch(t *testing.T) {
	types.SetLayersPerEpoch(int32(defaultActiveSetSize))
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, prevAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)

	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	publishedAtx.CalcAndSetID()

	// assert that the next ATX is in the next epoch
	published, _, err = publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch) // 👀
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, publishedAtx.ActivationTxHeader, publishedAtx.ActivationTxHeader, layersPerEpoch)

	publishedAtx2, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)

	r.Equal(publishedAtx.PubLayerID+layersPerEpoch, publishedAtx2.PubLayerID)
}

func TestBuilder_PublishActivationTx_FailsWhenNIPoSTBuilderFails(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	nipostBuilder := &NIPoSTErrBuilderMock{} // 👀 mock that returns error from BuildNIPoST()
	b := NewBuilder(nodeID, &MockSigning{}, activationDb, net, meshProviderMock, layersPerEpoch, nipostBuilder, &postProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	b.initialPoST = initialPoST

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to build nipost: nipost builder error")
	r.False(published)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	atx := newActivationTx(nodeID, 1, prevAtxID, 5, 1, prevAtxID, coinbase, defaultActiveSetSize, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, nipost)
	storeAtx(r, activationDb, atx, log.NewDefault("storeAtx"))

	view, err := b.mesh.GetOrphanBlocksBefore(meshProviderMock.LatestLayer())
	assert.NoError(t, err)
	act := newActivationTx(b.nodeID, 2, atx.ID(), atx.PubLayerID+10, 0, atx.ID(), coinbase, defaultActiveSetSize, view, nipost)

	bt, err := types.InterfaceToBytes(act)
	assert.NoError(t, err)
	a, err := types.BytesToAtx(bt)
	assert.NoError(t, err)
	bt2, err := types.InterfaceToBytes(a)
	assert.Equal(t, bt, bt2)
}

func TestBuilder_PublishActivationTx_PosAtxOnSameLayerAsPrevAtx(t *testing.T) {
	r := require.New(t)

	types.SetLayersPerEpoch(int32(defaultActiveSetSize))
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, 10)
	defer activesetCache.Purge()

	lg := log.NewDefault("storeAtx")
	for i := postGenesisEpochLayer; i < postGenesisEpochLayer+3; i++ {
		challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, types.LayerID(i))
		atx := newAtx(challenge, nipost)
		storeAtx(r, activationDb, atx, lg)
	}

	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer+3)
	prevATX := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevATX, lg)

	published, _, err := publishAtx(b, postGenesisEpochLayer+4, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)

	newAtx := lastTransmittedAtx(t)
	r.Equal(prevATX.ID(), newAtx.PrevATXID)

	posAtx, err := activationDb.GetAtxHeader(newAtx.PositioningATX)
	r.NoError(err)

	assertLastAtx(r, posAtx, prevATX.ActivationTxHeader, layersPerEpoch)

	t.Skip("proves https://github.com/spacemeshos/go-spacemesh/issues/1166")
	// check pos & prev has the same PubLayerID
	r.Equal(prevATX.PubLayerID, posAtx.PubLayerID)
}

func TestBuilder_SignAtx(t *testing.T) {
	ed := signing.NewEdSigner()
	nodeID := types.NodeID{Key: ed.PublicKey().String(), VRFPublicKey: []byte("bbbbb")}
	activationDb := NewDB(database.NewMemDatabase(), &MockIDStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(nodeID, ed, activationDb, net, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	atx := newActivationTx(nodeID, 1, prevAtx, 15, 1, prevAtx, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, nipost)
	atxBytes, err := types.InterfaceToBytes(atx.InnerActivationTx)
	assert.NoError(t, err)
	err = b.SignAtx(atx)
	assert.NoError(t, err)

	pubkey, err := ed25519.ExtractPublicKey(atxBytes, atx.Sig)
	assert.NoError(t, err)
	assert.Equal(t, ed.PublicKey().Bytes(), []byte(pubkey))

	ok := signing.Verify(signing.NewPublicKey(util.Hex2Bytes(atx.NodeID.Key)), atxBytes, atx.Sig)
	assert.True(t, ok)

}

func TestBuilder_NIPoSTPublishRecovery(t *testing.T) {
	id := types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("bbbbb")}
	coinbase := types.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipostBuilder := &NIPoSTBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	db := NewMockDB()
	sig := &MockSigning{}
	activationDb := NewDB(database.NewMemDatabase(), &MockIDStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	net.atxDb = activationDb
	b := NewBuilder(id, sig, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipostBuilder, &postProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipostBuilder.poetRef = poetRef
	npst := NewNIPoSTWithChallenge(&chlng, poetRef)

	atx := newActivationTx(types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("bbbbb")}, 1, prevAtx, 15, 1, prevAtx, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, npst)

	err := activationDb.StoreAtx(atx.PubLayerID.GetEpoch(), atx)
	assert.NoError(t, err)

	challenge := types.NIPoSTChallenge{
		NodeID:         b.nodeID,
		Sequence:       2,
		PrevATXID:      atx.ID(),
		PubLayerID:     atx.PubLayerID.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        b.tickProvider.NumOfTicks(), // todo: add tick provider (#827)
		PositioningATX: atx.ID(),
	}

	challengeHash, err := challenge.Hash()
	assert.NoError(t, err)
	npst2 := NewNIPoSTWithChallenge(challengeHash, poetRef)

	setActivesetSizeInCache(t, defaultActiveSetSize)

	layerClockMock.currentLayer = types.EpochID(1).FirstLayer() + 3
	err = b.PublishActivationTx()
	assert.EqualError(t, err, "target epoch has passed")

	// test load in correct epoch
	b = NewBuilder(id, &MockSigning{}, activationDb, net, layers, layersPerEpoch, nipostBuilder, &postProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layers.latestLayer = 22
	err = b.PublishActivationTx()
	assert.NoError(t, err)
	act := newActivationTx(b.nodeID, 2, atx.ID(), atx.PubLayerID+10, 0, atx.ID(), coinbase, 0, defaultView, npst2)
	err = b.SignAtx(act)
	assert.NoError(t, err)
	//bts, err := types.InterfaceToBytes(act) TODO(moshababo): encoded atx comparison fail, although decoded atxs are equal.
	//assert.NoError(t, err)
	//assert.Equal(t, bts, net.lastTransmission)

	b = NewBuilder(id, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipostBuilder, &postProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.buildNIPoSTChallenge(0)
	assert.NoError(t, err)
	db.hadNone = false
	// test load challenge in later epoch - NIPoST should be truncated
	b = NewBuilder(id, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipostBuilder, &postProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layerClockMock.currentLayer = types.EpochID(4).FirstLayer() + 3
	err = b.PublishActivationTx()
	// This 👇 ensures that handing of the challenge succeeded and the code moved on to the next part
	assert.EqualError(t, err, "target epoch has passed")
	assert.True(t, db.hadNone)
}

func genView() []types.BlockID {
	l := rand.Int() % 100
	var v []types.BlockID
	for i := 0; i < l; i++ {
		v = append(v, block2.ID())
	}

	return v
}

func TestActivationDb_CalcActiveSetFromViewHighConcurrency(t *testing.T) {
	activesetCache = NewActivesetCache(10) // small cache for collisions
	atxdb, layers, _ := getAtxDb("t6")

	id1 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, 12, 0, *types.EmptyATXID, coinbase1, 0, []types.BlockID{}, &types.NIPoST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, 300, 0, *types.EmptyATXID, coinbase2, 0, []types.BlockID{}, &types.NIPoST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, 435, 0, *types.EmptyATXID, coinbase3, 0, []types.BlockID{}, &types.NIPoST{}),
	}

	poetRef := []byte{0xba, 0xb0}
	for _, atx := range atxs {
		hash, err := atx.NIPoSTChallenge.Hash()
		assert.NoError(t, err)
		atx.NIPoST = NewNIPoSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	mck := &ATXDBMock{}
	atx := newActivationTx(id1, 1, atxs[0].ID(), 1000, 0, atxs[0].ID(), coinbase1, 3, blocks, &types.NIPoST{})

	atxdb.calcActiveSetFunc = mck.CalcActiveSetSize
	wg := sync.WaitGroup{}
	ff := 5000
	wg.Add(ff)
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < ff; i++ {
		go func() {
			num, err := atxdb.CalcActiveSetFromView(genView(), atx.PubLayerID.GetEpoch())
			assert.NoError(t, err)
			assert.Equal(t, 3, int(num))
			assert.NoError(t, err)
			wg.Done()
		}()
	}

	wg.Wait()
}

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, activeSetSize uint32, view []types.BlockID,
	nipost *types.NIPoST) *types.ActivationTx {

	nipostChallenge := types.NIPoSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipostChallenge, coinbase, nipost, nil)
}
