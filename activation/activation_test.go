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
	"github.com/spacemeshos/post/config"
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

	defaultView      = []types.BlockID{block1.ID(), block2.ID(), block3.ID()}
	net              = &NetMock{}
	layerClockMock   = &LayerClockMock{}
	meshProviderMock = &MeshProviderMock{latestLayer: 12}
	nipstBuilderMock = &NipstBuilderMock{}
	postProver       = &postProverClientMock{}
	npst             = NewNIPSTWithChallenge(&chlng, poetRef)
	commitment       = &types.PostProof{
		Challenge:    []byte(nil),
		MerkleRoot:   []byte("1"),
		ProofNodes:   [][]byte(nil),
		ProvenLeaves: [][]byte(nil),
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

// A compile time check to ensure that postProverClientMock fully implements PostProverClient.
var _ PostProverClient = (*postProverClientMock)(nil)

type NipstBuilderMock struct {
	poetRef        []byte
	buildNipstFunc func(challenge *types.Hash32) (*types.NIPST, error)
	initPostFunc   func(logicalDrive string, commitmentSize uint64) (*types.PostProof, error)
	SleepTime      int
}

func (np *NipstBuilderMock) BuildNIPST(challenge *types.Hash32, _ chan struct{}, _ chan struct{}) (*types.NIPST, error) {
	if np.buildNipstFunc != nil {
		return np.buildNipstFunc(challenge)
	}
	return NewNIPSTWithChallenge(challenge, np.poetRef), nil
}

type NipstErrBuilderMock struct{}

func (np *NipstErrBuilderMock) BuildNIPST(*types.Hash32, chan struct{}, chan struct{}) (*types.NIPST, error) {
	return nil, fmt.Errorf("nipst builder error")
}

type MockIDStore struct{}

func (*MockIDStore) StoreNodeIdentity(types.NodeID) error {
	return nil
}

func (*MockIDStore) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(signing.PublicKey, *types.NIPST, types.Hash32) error {
	return nil
}

func (*ValidatorMock) VerifyPost(signing.PublicKey, *types.PostProof, uint64) error {
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
	return NewDB(database.NewMemDatabase(), &MockIDStore{}, nil, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB"))
}

func newChallenge(nodeID types.NodeID, sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID) types.NIPSTChallenge {
	challenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
	}
	return challenge
}

func newAtx(challenge types.NIPSTChallenge, View []types.BlockID, nipst *types.NIPST) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: &types.ActivationTxHeader{
				NIPSTChallenge: challenge,
				Coinbase:       coinbase,
			},
			Nipst: nipst,
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
	b := NewBuilder(nodeID, coinbase, &MockSigning{}, activationDb, net, meshProviderMock, layersPerEpoch, nipstBuilderMock, postProver, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	b.commitment = commitment
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
		r.Nil(atx.Commitment)
		r.Nil(atx.CommitmentMerkleRoot)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(*types.EmptyATXID, atx.PrevATXID)
		r.NotNil(atx.Commitment)
		r.NotNil(atx.CommitmentMerkleRoot)
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

func publishAtx(b *Builder, meshLayer types.LayerID, clockEpoch types.EpochID, buildNipstLayerDuration uint16) (published, builtNipst bool, err error) {
	net.lastTransmission = nil
	meshProviderMock.latestLayer = meshLayer
	nipstBuilderMock.buildNipstFunc = func(challenge *types.Hash32) (*types.NIPST, error) {
		builtNipst = true
		meshProviderMock.latestLayer = meshLayer.Add(buildNipstLayerDuration)
		layerClockMock.currentLayer = layerClockMock.currentLayer.Add(buildNipstLayerDuration)
		return NewNIPSTWithChallenge(challenge, poetRef), nil
	}
	layerClockMock.currentLayer = clockEpoch.FirstLayer(layersPerEpoch) + 3
	err = b.PublishActivationTx()
	nipstBuilderMock.buildNipstFunc = nil
	return net.lastTransmission != nil, builtNipst, err
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
	prevAtx := newAtx(challenge, defaultView, npst)
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
	prevAtx := newAtx(challenge, defaultView, npst)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(nodeID, coinbase, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipstBuilderMock, postProver, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to broadcast ATX: faulty")
	r.False(published)

	// create and attempt to publish ATX
	faultyNet.retErr = false
	b = NewBuilder(nodeID, coinbase, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipstBuilderMock, postProver, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, builtNipst, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "target epoch has passed")
	r.False(published)
	r.True(builtNipst)

	// if the network works and we try to publish a new ATX, the timeout should result in a clean state (so a NIPST should be built)
	b.net = net
	net.atxDb = activationDb
	posAtx := newAtx(newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer+layersPerEpoch+1), defaultView, npst)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))
	published, builtNipst, err = publishAtx(b, postGenesisEpochLayer+layersPerEpoch+2, postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNipst)
}

func TestBuilder_PublishActivationTx_RebuildNipstWhenTargetEpochPassed(t *testing.T) {
	r := require.New(t)

	// setup
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()
	activationDb := newActivationDb()
	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, defaultView, npst)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(nodeID, coinbase, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipstBuilderMock, postProver, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, builtNipst, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to broadcast ATX: faulty")
	r.False(published)
	r.True(builtNipst)

	// We started building the NIPST in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the target epoch (4) has passed, so we'll start the ATX builder in epoch 5 and ensure it builds a
	// new NIPST.

	// if the network works - the ATX should be published
	b.net = net
	net.atxDb = activationDb
	posAtx := newAtx(newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer+(3*layersPerEpoch)), defaultView, npst)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))
	published, builtNipst, err = publishAtx(b, postGenesisEpochLayer+(3*layersPerEpoch)+1, postGenesisEpoch+3, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNipst)
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
	posAtx := newAtx(challenge, defaultView, npst)
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
	posAtx := newAtx(challenge, defaultView, npst)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	challenge = newChallenge(nodeID /*👀*/, 0, *types.EmptyATXID, posAtx.ID(), postGenesisEpochLayer)
	challenge.CommitmentMerkleRoot = commitment.MerkleRoot
	prevAtx := newAtx(challenge, defaultView, npst)
	prevAtx.Commitment = commitment
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
	posAtx := newAtx(challenge, defaultView, npst)
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
	prevAtx := newAtx(challenge, defaultView, npst)
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

func TestBuilder_PublishActivationTx_FailsWhenNipstBuilderFails(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	nipstBuilder := &NipstErrBuilderMock{} // 👀 mock that returns error from BuildNipst()
	b := NewBuilder(nodeID, coinbase, &MockSigning{}, activationDb, net, meshProviderMock, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	b.commitment = commitment

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, defaultView, npst)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	published, _, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to build nipst: nipst builder error")
	r.False(published)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	atx := newActivationTx(nodeID, 1, prevAtxID, 5, 1, prevAtxID, coinbase, defaultActiveSetSize, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, npst)
	storeAtx(r, activationDb, atx, log.NewDefault("storeAtx"))

	view, err := b.mesh.GetOrphanBlocksBefore(meshProviderMock.LatestLayer())
	assert.NoError(t, err)
	act := newActivationTx(b.nodeID, 2, atx.ID(), atx.PubLayerID+10, 0, atx.ID(), coinbase, defaultActiveSetSize, view, npst)

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
		atx := newAtx(challenge, defaultView, npst)
		storeAtx(r, activationDb, atx, lg)
	}

	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer+3)
	prevATX := newAtx(challenge, defaultView, npst)
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
	activationDb := NewDB(database.NewMemDatabase(), &MockIDStore{}, nil, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(nodeID, coinbase, ed, activationDb, net, meshProviderMock, layersPerEpoch, nipstBuilderMock, postProver, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	atx := newActivationTx(nodeID, 1, prevAtx, 15, 1, prevAtx, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, npst)
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

func TestBuilder_NipstPublishRecovery(t *testing.T) {
	id := types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("bbbbb")}
	coinbase := types.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	db := NewMockDB()
	sig := &MockSigning{}
	activationDb := NewDB(database.NewMemDatabase(), &MockIDStore{}, nil, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	net.atxDb = activationDb
	b := NewBuilder(id, coinbase, sig, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipstBuilder.poetRef = poetRef
	npst := NewNIPSTWithChallenge(&chlng, poetRef)

	atx := newActivationTx(types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("bbbbb")}, 1, prevAtx, 15, 1, prevAtx, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, npst)

	err := activationDb.StoreAtx(atx.PubLayerID.GetEpoch(), atx)
	assert.NoError(t, err)

	challenge := types.NIPSTChallenge{
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
	npst2 := NewNIPSTWithChallenge(challengeHash, poetRef)

	setActivesetSizeInCache(t, defaultActiveSetSize)

	layerClockMock.currentLayer = types.EpochID(1).FirstLayer(layersPerEpoch) + 3
	err = b.PublishActivationTx()
	assert.EqualError(t, err, "target epoch has passed")

	// test load in correct epoch
	b = NewBuilder(id, coinbase, &MockSigning{}, activationDb, net, layers, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layers.latestLayer = 22
	err = b.PublishActivationTx()
	assert.NoError(t, err)
	act := newActivationTx(b.nodeID, 2, atx.ID(), atx.PubLayerID+10, 0, atx.ID(), coinbase, 0, defaultView, npst2)
	err = b.SignAtx(act)
	assert.NoError(t, err)
	bts, err := types.InterfaceToBytes(act)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.lastTransmission)

	b = NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.buildNipstChallenge()
	assert.NoError(t, err)
	db.hadNone = false
	// test load challenge in later epoch - Nipst should be truncated
	b = NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layerClockMock.currentLayer = types.EpochID(4).FirstLayer(layersPerEpoch) + 3
	err = b.PublishActivationTx()
	// This 👇 ensures that handing of the challenge succeeded and the code moved on to the next part
	assert.EqualError(t, err, "target epoch has passed")
	assert.True(t, db.hadNone)
}

func TestStartPost(t *testing.T) {
	id := types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("bbbbb")}
	coinbase := types.HexToAddress("0xaaa")
	layers := &MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])

	drive := "/tmp/anton"
	coinbase2 := types.HexToAddress("0xabb")
	db := NewMockDB()

	postCfg := *config.DefaultConfig()
	postCfg.Difficulty = 5
	postCfg.NumProvenLabels = 10
	postCfg.SpacePerUnit = 1 << 10 // 1KB.
	postCfg.NumFiles = 1

	postProver, err := NewPostClient(&postCfg, util.Hex2Bytes(id.Key))
	assert.NoError(t, err)
	assert.NotNil(t, postProver)
	defer func() {
		assert.NoError(t, postProver.Reset())
	}()

	activationDb := NewDB(database.NewMemDatabase(), &MockIDStore{}, nil, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	builder := NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))

	// Attempt to initialize with invalid space.
	// This test verifies that the params are being set in the post client.
	assert.Nil(t, builder.commitment)
	err = builder.StartPost(coinbase2, drive, 1000)
	assert.EqualError(t, err, "space (1000) must be a power of 2")
	assert.Nil(t, builder.commitment)
	assert.Equal(t, postProver.Cfg().SpacePerUnit, uint64(1000))

	// Attempt to initialize again
	assert.Nil(t, builder.commitment)
	err = builder.StartPost(coinbase2, drive, 1024)
	assert.EqualError(t, err, "already started")
	assert.Nil(t, builder.commitment)

	// Reinitialize.
	builder = NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder2"))
	assert.Nil(t, builder.commitment)
	err = builder.StartPost(coinbase2, drive, 1024)
	assert.NoError(t, err)
	time.Sleep(100 * time.Millisecond) // Introducing a small delay since the procedure is async.
	assert.NotNil(t, builder.commitment)
	assert.Equal(t, postProver.Cfg().SpacePerUnit, uint64(1024))

	// Attempt to initialize again.
	err = builder.StartPost(coinbase2, drive, 1024)
	assert.EqualError(t, err, "already initialized")
	assert.NotNil(t, builder.commitment)

	// Instantiate a new builder and call StartPost on the same datadir, which is already initialized,
	// and so will result in running the execution phase instead of the initialization phase.
	// This test verifies that a call to StartPost with a different space param will return an error.
	execBuilder := NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = execBuilder.StartPost(coinbase2, drive, 2048)
	assert.EqualError(t, err, "config mismatch")

	// Call StartPost with the correct space param.
	assert.Nil(t, execBuilder.commitment)
	err = execBuilder.StartPost(coinbase2, drive, 1024)
	time.Sleep(100 * time.Millisecond)
	assert.NoError(t, err)
	assert.NotNil(t, execBuilder.commitment)

	// Verify both builders produced the same commitment proof - one from the initialization phase,
	// the other from a zero-challenge execution phase.
	assert.Equal(t, builder.commitment, execBuilder.commitment)
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
		newActivationTx(id1, 0, *types.EmptyATXID, 12, 0, *types.EmptyATXID, coinbase1, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, 300, 0, *types.EmptyATXID, coinbase2, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, 435, 0, *types.EmptyATXID, coinbase3, 0, []types.BlockID{}, &types.NIPST{}),
	}

	poetRef := []byte{0xba, 0xb0}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	mck := &ATXDBMock{}
	atx := newActivationTx(id1, 1, atxs[0].ID(), 1000, 0, atxs[0].ID(), coinbase1, 3, blocks, &types.NIPST{})

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
	nipst *types.NIPST) *types.ActivationTx {

	nipstChallenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipstChallenge, coinbase, activeSetSize, nipst, nil)
}
