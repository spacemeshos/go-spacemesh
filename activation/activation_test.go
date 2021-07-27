package activation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	//	"github.com/spacemeshos/post/config"
)

// ========== Vars / Consts ==========

const (
	layersPerEpoch   = 10
	postGenesisEpoch = 2
)

func init() {
	types.SetLayersPerEpoch(layersPerEpoch)
}

var (
	pub, _, _   = ed25519.GenerateKey(nil)
	nodeID      = types.NodeID{Key: util.Bytes2Hex(pub), VRFPublicKey: []byte("22222")}
	otherNodeID = types.NodeID{Key: "00000", VRFPublicKey: []byte("00000")}
	coinbase    = types.HexToAddress("33333")
	goldenATXID = types.ATXID(types.HexToHash32("77777"))
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	chlng       = types.HexToHash32("55555")
	poetRef     = types.BytesToHash([]byte("66666"))
	poetBytes   = []byte("66666")
	block1      = types.NewExistingBlock(types.LayerID{}, []byte("11111"), nil)
	block2      = types.NewExistingBlock(types.LayerID{}, []byte("22222"), nil)
	block3      = types.NewExistingBlock(types.LayerID{}, []byte("33333"), nil)

	postGenesisEpochLayer = types.NewLayerID(22)
	defaultMeshLayer      = types.NewLayerID(12)

	defaultView       = []types.BlockID{block1.ID(), block2.ID(), block3.ID()}
	net               = &NetMock{}
	layerClockMock    = &LayerClockMock{}
	meshProviderMock  = &MeshProviderMock{latestLayer: types.NewLayerID(12)}
	nipostBuilderMock = &NIPostBuilderMock{}
	nipost            = NewNIPostWithChallenge(&chlng, poetBytes)
	initialPost       = &types.Post{
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

func (n *NetMock) Broadcast(_ context.Context, _ string, d []byte) error {
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

type NIPostBuilderMock struct {
	updatedPoETs    []PoetProvingServiceClient
	poetRef         []byte
	buildNIPostFunc func(challenge *types.Hash32) (*types.NIPost, error)
	initPostFunc    func(logicalDrive string, commitmentSize uint64) (*types.Post, error)
	SleepTime       int
}

func (np NIPostBuilderMock) updatePoETProver(PoetProvingServiceClient) {}

func (np *NIPostBuilderMock) BuildNIPost(_ context.Context, challenge *types.Hash32, _ chan struct{}) (*types.NIPost, error) {
	if np.buildNIPostFunc != nil {
		return np.buildNIPostFunc(challenge)
	}
	return NewNIPostWithChallenge(challenge, np.poetRef), nil
}

type NIPostErrBuilderMock struct{}

func (np *NIPostErrBuilderMock) updatePoETProver(PoetProvingServiceClient) {}

func (np *NIPostErrBuilderMock) BuildNIPost(context.Context, *types.Hash32, chan struct{}) (*types.NIPost, error) {
	return nil, fmt.Errorf("NIPost builder error")
}

type MockIDStore struct{}

func (*MockIDStore) StoreNodeIdentity(types.NodeID) error {
	return nil
}

func (*MockIDStore) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{}, nil
}

type ValidatorMock struct{}

// A compile time check to ensure that ValidatorMock fully implements the nipostValidator interface.
var _ nipostValidator = (*ValidatorMock)(nil)

func (*ValidatorMock) Validate(id signing.PublicKey, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint) error {
	return nil
}

func (*ValidatorMock) ValidatePost(id []byte, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint) error {
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

func (n *FaultyNetMock) Broadcast(_ context.Context, _ string, d []byte) error {
	n.bt = d
	if n.retErr {
		return fmt.Errorf("faulty")
	}
	// not calling `go hookToAtxPool(d)`
	return nil
}

// ========== Helper functions ==========

func newActivationDb() *DB {
	return NewDB(database.NewMemDatabase(), &MockIDStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, goldenATXID, &ValidatorMock{}, lg.WithName("atxDB"))
}

func newChallenge(nodeID types.NodeID, sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID) types.NIPostChallenge {
	challenge := types.NIPostChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		StartTick:      100 * sequence,
		EndTick:        100 * (sequence + 1),
		PositioningATX: posAtxID,
	}
	return challenge
}

func newAtx(challenge types.NIPostChallenge, nipost *types.NIPost) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: &types.ActivationTxHeader{
				NIPostChallenge: challenge,
				NumUnits:        2,
				Coinbase:        coinbase,
			},
			NIPost: nipost,
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
		time.Sleep(500 * time.Millisecond)
		close(ch)
	}()
	return ch
}

type mockSyncer struct{}

func (m *mockSyncer) RegisterChForSynced(_ context.Context, ch chan struct{}) {
	close(ch)
}

func newBuilder(activationDb atxDBProvider) *Builder {
	net.atxDb = activationDb
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	b := NewBuilder(cfg, nodeID, &MockSigning{}, activationDb, net, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	b.initialPost = initialPost
	return b
}

func lastTransmittedAtx(t *testing.T) types.ActivationTx {
	var signedAtx types.ActivationTx
	err := types.BytesToInterface(net.lastTransmission, &signedAtx)
	require.NoError(t, err)
	return signedAtx
}

func assertLastAtx(r *require.Assertions, posAtx, prevAtx *types.ActivationTxHeader, layersPerEpoch uint32) {
	sigAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)

	atx := sigAtx
	r.Equal(nodeID, atx.NodeID)
	if prevAtx != nil {
		r.Equal(prevAtx.Sequence+1, atx.Sequence)
		r.Equal(prevAtx.ID(), atx.PrevATXID)
		r.Nil(atx.InitialPost)
		r.Nil(atx.InitialPostIndices)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(*types.EmptyATXID, atx.PrevATXID)
		r.NotNil(atx.InitialPost)
		r.NotNil(atx.InitialPostIndices)
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

func publishAtx(b *Builder, meshLayer types.LayerID, clockEpoch types.EpochID, buildNIPostLayerDuration uint32) (published, builtNIPost bool, err error) {
	net.lastTransmission = nil
	meshProviderMock.latestLayer = meshLayer
	nipostBuilderMock.buildNIPostFunc = func(challenge *types.Hash32) (*types.NIPost, error) {
		builtNIPost = true
		meshProviderMock.latestLayer = meshLayer.Add(buildNIPostLayerDuration)
		layerClockMock.currentLayer = layerClockMock.currentLayer.Add(buildNIPostLayerDuration)
		return NewNIPostWithChallenge(challenge, poetBytes), nil
	}
	layerClockMock.currentLayer = clockEpoch.FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	nipostBuilderMock.buildNIPostFunc = nil
	return net.lastTransmission != nil, builtNIPost, err
}

// ========== Tests ==========

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, prevAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)

	// create and publish another ATX
	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	publishedAtx.CalcAndSetID()
	published, _, err = publishAtx(b, postGenesisEpochLayer.Add(layersPerEpoch+1), postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, publishedAtx.ActivationTxHeader, publishedAtx.ActivationTxHeader, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(cfg, nodeID, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to broadcast ATX: faulty")
	r.False(published)

	// create and attempt to publish ATX
	faultyNet.retErr = false
	b = NewBuilder(cfg, nodeID, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, builtNipost, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.ErrorIs(err, ErrATXChallengeExpired)
	r.False(published)
	r.True(builtNipost)

	// if the network works and we try to publish a new ATX, the timeout should result in a clean state (so a NIPost should be built)
	b.net = net
	net.atxDb = activationDb
	posAtx := newAtx(newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(layersPerEpoch+1)), nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))
	published, builtNipost, err = publishAtx(b, postGenesisEpochLayer.Add(layersPerEpoch+2), postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNipost)
}

func TestBuilder_PublishActivationTx_RebuildNIPostWhenTargetEpochPassed(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(cfg, nodeID, &MockSigning{}, activationDb, faultyNet, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	published, builtNIPost, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to broadcast ATX: faulty")
	r.False(published)
	r.True(builtNIPost)

	// We started building the NIPost in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the target epoch (4) has passed, so we'll start the ATX builder in epoch 5 and ensure it builds a
	// new NIPost.

	// if the network works - the ATX should be published
	b.net = net
	net.atxDb = activationDb
	posAtx := newAtx(newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(3*layersPerEpoch)), nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))
	published, builtNIPost, err = publishAtx(b, postGenesisEpochLayer.Add(3*layersPerEpoch).Add(1), postGenesisEpoch+3, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNIPost)
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_PrevATXWithoutPrevATX(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	challenge = newChallenge(nodeID /*👀*/, 0, *types.EmptyATXID, posAtx.ID(), postGenesisEpochLayer)
	challenge.InitialPostIndices = initialPost.Indices
	prevAtx := newAtx(challenge, nipost)
	prevAtx.InitialPost = initialPost
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_TargetsEpochBasedOnPosAtx(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer.Sub(layersPerEpoch) /*👀*/)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	// create and publish ATX based on the best available posAtx, as long as the node is synced
	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_DoesNotPublish2AtxsInSameEpoch(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, prevAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)

	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	publishedAtx.CalcAndSetID()

	// assert that the next ATX is in the next epoch
	published, _, err = publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch) // 👀
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, publishedAtx.ActivationTxHeader, publishedAtx.ActivationTxHeader, layersPerEpoch)

	publishedAtx2, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)

	r.Equal(publishedAtx.PubLayerID.Add(layersPerEpoch), publishedAtx2.PubLayerID)
}

func TestBuilder_PublishActivationTx_FailsWhenNIPostBuilderFails(t *testing.T) {
	r := require.New(t)

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	activationDb := newActivationDb()
	nipostBuilder := &NIPostErrBuilderMock{} // 👀 mock that returns error from BuildNIPost()
	b := NewBuilder(cfg, nodeID, &MockSigning{}, activationDb, net, meshProviderMock, layersPerEpoch, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))
	b.initialPost = initialPost

	challenge := newChallenge(otherNodeID /*👀*/, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(1), postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "failed to build NIPost: NIPost builder error")
	r.False(published)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	atx := newActivationTx(nodeID, 1, prevAtxID, prevAtxID, types.NewLayerID(5), 1, 100, coinbase, 100, nipost)
	storeAtx(r, activationDb, atx, log.NewDefault("storeAtx"))

	act := newActivationTx(b.nodeID, 2, atx.ID(), atx.ID(), atx.PubLayerID.Add(10), 0, 100, coinbase, 100, nipost)

	bt, err := types.InterfaceToBytes(act)
	assert.NoError(t, err)
	a, err := types.BytesToAtx(bt)
	assert.NoError(t, err)
	bt2, err := types.InterfaceToBytes(a)
	assert.Equal(t, bt, bt2)
}

func TestBuilder_PublishActivationTx_PosAtxOnSameLayerAsPrevAtx(t *testing.T) {
	r := require.New(t)

	types.SetLayersPerEpoch(layersPerEpoch)
	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	lg := log.NewDefault("storeAtx")
	for i := postGenesisEpochLayer; i.Before(postGenesisEpochLayer.Add(3)); i = i.Add(1) {
		challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, types.LayerID(i))
		atx := newAtx(challenge, nipost)
		storeAtx(r, activationDb, atx, lg)
	}

	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(3))
	prevATX := newAtx(challenge, nipost)
	storeAtx(r, activationDb, prevATX, lg)

	published, _, err := publishAtx(b, postGenesisEpochLayer.Add(4), postGenesisEpoch, layersPerEpoch)
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
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	ed := signing.NewEdSigner()
	nodeID := types.NodeID{Key: ed.PublicKey().String(), VRFPublicKey: []byte("bbbbb")}
	activationDb := NewDB(database.NewMemDatabase(), &MockIDStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, goldenATXID, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(cfg, nodeID, ed, activationDb, net, meshProviderMock, layersPerEpoch, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"))

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	atx := newActivationTx(nodeID, 1, prevAtx, prevAtx, types.NewLayerID(15), 1, 100, coinbase, 100, nipost)
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

func TestBuilder_NIPostPublishRecovery(t *testing.T) {
	id := types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("bbbbb")}
	coinbase := types.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipostBuilder := &NIPostBuilderMock{}
	layersPerEpoch := uint32(10)
	lg := log.NewDefault(id.Key[:5])
	db := NewMockDB()
	sig := &MockSigning{}
	activationDb := NewDB(database.NewMemDatabase(), &MockIDStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, goldenATXID, &ValidatorMock{}, lg.WithName("atxDB1"))
	net.atxDb = activationDb

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	b := NewBuilder(cfg, id, sig, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipostBuilder.poetRef = poetRef
	npst := NewNIPostWithChallenge(&chlng, poetRef)

	atx := newActivationTx(types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("bbbbb")}, 1, prevAtx, prevAtx, types.NewLayerID(15), 1, 100, coinbase, 100, npst)

	err := activationDb.StoreAtx(atx.PubLayerID.GetEpoch(), atx)
	assert.NoError(t, err)

	challenge := types.NIPostChallenge{
		NodeID:         b.nodeID,
		Sequence:       2,
		PrevATXID:      atx.ID(),
		PubLayerID:     atx.PubLayerID.Add(b.layersPerEpoch),
		StartTick:      atx.EndTick,
		EndTick:        atx.EndTick + b.tickProvider.NumOfTicks(), // todo: add tick provider (#827)
		PositioningATX: atx.ID(),
	}

	challengeHash, err := challenge.Hash()
	assert.NoError(t, err)
	npst2 := NewNIPostWithChallenge(challengeHash, poetRef)
	layerClockMock.currentLayer = types.EpochID(1).FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	assert.ErrorIs(t, err, ErrATXChallengeExpired)

	// test load in correct epoch
	b = NewBuilder(cfg, id, &MockSigning{}, activationDb, net, layers, layersPerEpoch, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layers.latestLayer = types.NewLayerID(22)
	err = b.PublishActivationTx(context.TODO())
	assert.NoError(t, err)
	act := newActivationTx(b.nodeID, 2, atx.ID(), atx.ID(), atx.PubLayerID.Add(10), 101, 1, coinbase, 0, npst2)
	err = b.SignAtx(act)
	assert.NoError(t, err)
	// TODO(moshababo): encoded atx comparison fail, although decoded atxs are equal.
	//bts, err := types.InterfaceToBytes(act)
	//assert.NoError(t, err)
	//assert.Equal(t, bts, net.lastTransmission)

	b = NewBuilder(cfg, id, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.buildNIPostChallenge(context.TODO())
	assert.NoError(t, err)
	db.hadNone = false
	// test load challenge in later epoch - NIPost should be truncated
	b = NewBuilder(cfg, id, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layerClockMock.currentLayer = types.EpochID(4).FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	// This 👇 ensures that handing of the challenge succeeded and the code moved on to the next part
	assert.ErrorIs(t, err, ErrATXChallengeExpired)
	assert.True(t, db.hadNone)
}

func TestBuilder_RetryPublishActivationTx(t *testing.T) {
	r := require.New(t)
	bc := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	retryInterval := 10 * time.Microsecond
	expectedTries := 3

	activationDb := newActivationDb()
	nipostBuilder := &NIPostBuilderMock{}
	b := NewBuilder(bc, nodeID, &MockSigning{}, activationDb, net, meshProviderMock,
		layersPerEpoch, nipostBuilder, &postSetupProviderMock{}, layerClockMock,
		&mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"),
		WithPoetRetryInterval(retryInterval),
	)
	b.initialPost = initialPost

	challenge := newChallenge(otherNodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(challenge, nipost)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	net.lastTransmission = nil
	meshProviderMock.latestLayer = postGenesisEpochLayer.Add(1)
	tries := 0
	builderConfirmation := make(chan struct{})
	// TODO(dshulyak) maybe measure time difference between attempts. It should be no less than retryInterval
	nipostBuilder.buildNIPostFunc = func(challenge *types.Hash32) (*types.NIPost, error) {
		tries++
		if tries == expectedTries {
			close(builderConfirmation)
		} else if tries < expectedTries {
			return nil, ErrPoetServiceUnstable
		}
		return NewNIPostWithChallenge(challenge, poetBytes), nil
	}
	layerClockMock.currentLayer = types.EpochID(postGenesisEpoch).FirstLayer().Add(3)
	ctx, cancel := context.WithCancel(context.Background())
	runnerExit := make(chan struct{})
	go func() {
		b.run(ctx)
		close(runnerExit)
	}()
	t.Cleanup(func() {
		cancel()
		<-runnerExit
	})

	select {
	case <-builderConfirmation:
	case <-time.After(time.Second):
		require.FailNow(t, "failed waiting for required number of tries to occur")
	}
}

/*
func TestBuilder_UpdatePoETProver(t *testing.T) {
	// we test that poet client is not replaced in between PoetServiceID and Submit calls.
	// but after Submit call fails with error it is replaced and called

	bc := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	activationDb := newActivationDb()
	poetProver, controller := newPoetServiceMock(t)
	defer controller.Finish()

	poetProver2, controller2 := newPoetServiceMock(t)
	defer controller2.Finish()

	mockInitializer := func(target string) PoetProvingServiceClient {
		return poetProver2
	}
	poetDb := &poetDbMock{}
	nb := NewNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	b := NewBuilder(bc, nodeID, 0, &MockSigning{}, activationDb, net, meshProviderMock,
		nb, postProver, layerClockMock,
		&mockSyncer{}, NewMockDB(), lg.WithName("atxBuilder"),
		WithPoetRetryInterval(time.Millisecond),
		WithPoETClientInitializer(mockInitializer),
	)
	b.commitment = commitment
	syncPoint := make(chan struct{}, 1)

	poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Do(func(context.Context) {
		syncPoint <- struct{}{}
	}).Return([]byte{}, nil)
	poetProver.EXPECT().Submit(gomock.Any(), gomock.Any()).Do(func(context.Context, types.Hash32) {
		<-syncPoint
	}).Return(nil, errors.New("test"))

	ctx, cancel := context.WithCancel(context.Background())
	closed := make(chan struct{})
	go func() {
		b.run(ctx)
		close(closed)
	}()
	t.Cleanup(func() {
		cancel()
		<-closed
	})

	select {
	case <-syncPoint:
	case <-time.After(time.Second):
		require.FailNow(t, "timedout waiting for PoetServiceID to be called")
	}
	poetProver2.EXPECT().PoetServiceID(gomock.Any()).Return([]byte{}, nil)
	require.NoError(t, b.UpdatePoETServer(context.TODO(), "update"))
	poet2Called := make(chan struct{})
	poetProver2.EXPECT().PoetServiceID(gomock.Any()).Do(func(context.Context) {
		cancel()
		close(poet2Called)
	}).Return([]byte{}, errors.New("update"))
	syncPoint <- struct{}{} // allow submit to run
	select {
	case <-poet2Called:
	case <-time.After(time.Second):
		require.FailNow(t, "timedout waiting for update poet to be called")
	}
}
*/

func genView() []types.BlockID {
	l := rand.Int() % 100
	var v []types.BlockID
	for i := 0; i < l; i++ {
		v = append(v, block2.ID())
	}

	return v
}

// Check that we're not trying to sync an ATX that references the golden ATX or an empty ATX (i.e. not adding it to the sync queue).
func TestActivationDB_FetchAtxReferences(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	r := require.New(t)

	activationDb := newActivationDb()
	challenge := newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	fetcher := newFetchMock()

	atx1 := newAtx(challenge, nipost)
	atx1.PositioningATX = prevAtxID // should be fetched
	atx1.PrevATXID = prevAtxID      // should be fetched

	atx2 := newAtx(challenge, nipost)
	atx2.PositioningATX = goldenATXID // should *NOT* be fetched
	atx2.PrevATXID = prevAtxID        // should be fetched

	atx3 := newAtx(challenge, nipost)
	atx3.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx3.PrevATXID = prevAtxID              // should be fetched

	atx4 := newAtx(challenge, nipost)
	atx4.PositioningATX = prevAtxID    // should be fetched
	atx4.PrevATXID = *types.EmptyATXID // should *NOT* be fetched

	atx5 := newAtx(challenge, nipost)
	atx5.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx5.PrevATXID = *types.EmptyATXID      // should *NOT* be fetched

	atxList := []*types.ActivationTx{atx1, atx2, atx3, atx4, atx5}

	for _, atx := range atxList {
		r.NoError(activationDb.FetchAtxReferences(context.TODO(), atx, fetcher))
	}

	expected := map[types.ATXID]int{
		prevAtxID: 5,
	}

	r.EqualValues(expected, fetcher.atxCalled)
}

func newActivationTx(
	nodeID types.NodeID,
	sequence uint64,
	prevATX types.ATXID,
	positioningATX types.ATXID,
	pubLayerID types.LayerID,
	startTick, numTicks uint64,
	coinbase types.Address,
	numUnints uint,
	nipost *types.NIPost,
) *types.ActivationTx {
	nipostChallenge := types.NIPostChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		EndTick:        startTick + numTicks,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipostChallenge, coinbase, nipost, numUnints, nil)
}
