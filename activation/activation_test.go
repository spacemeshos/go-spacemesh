package activation

import (
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sort"
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

var (
	pub, _, _    = ed25519.GenerateKey(nil)
	nodeId       = types.NodeId{Key: util.Bytes2Hex(pub), VRFPublicKey: []byte("22222")}
	otherNodeId  = types.NodeId{Key: "00000", VRFPublicKey: []byte("00000")}
	coinbase     = types.HexToAddress("33333")
	prevAtxId    = types.AtxId(types.HexToHash32("44444"))
	chlng        = types.HexToHash32("55555")
	poetRef      = []byte("66666")
	defaultView  = []types.BlockID{1, 2, 3}
	net          = &NetMock{}
	meshProvider = &MeshProviderMock{latestLayer: 12}
	nipstBuilder = &NipstBuilderMock{}
	postProver   = &postProverClientMock{}
	npst         = NewNIPSTWithChallenge(&chlng, poetRef)
	commitment   = &types.PostProof{
		//Identity:     []byte(nil),
		Challenge:    []byte(nil),
		MerkleRoot:   []byte("1"),
		ProofNodes:   [][]byte(nil),
		ProvenLeaves: [][]byte(nil),
	}
	lg = log.NewDefault(nodeId.Key[:5])
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
}

func (n *NetMock) Broadcast(id string, d []byte) error {
	n.lastTransmission = d
	return nil
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

func (np *NipstBuilderMock) BuildNIPST(challenge *types.Hash32) (*types.NIPST, error) {
	if np.buildNipstFunc != nil {
		return np.buildNipstFunc(challenge)
	}
	return NewNIPSTWithChallenge(challenge, np.poetRef), nil
}

type NipstErrBuilderMock struct{}

func (np *NipstErrBuilderMock) BuildNIPST(challenge *types.Hash32) (*types.NIPST, error) {
	return nil, fmt.Errorf("Nipst builder error")
}

type MockIdStore struct{}

func (*MockIdStore) StoreNodeIdentity(id types.NodeId) error {
	return nil
}

func (*MockIdStore) GetIdentity(id string) (types.NodeId, error) {
	return types.NodeId{}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(id signing.PublicKey, nipst *types.NIPST, expectedChallenge types.Hash32) error {
	return nil
}

func (*ValidatorMock) VerifyPost(id signing.PublicKey, proof *types.PostProof, space uint64) error {
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
		&types.InnerActivationTx{
			ActivationTxHeader: &types.ActivationTxHeader{
				NIPSTChallenge: challenge,
				Coinbase:       coinbase,
				ActiveSetSize:  ActiveSetSize,
			},
			Nipst: nipst,
			View:  View,
		},
		nil,
	}
	activationTx.CalcAndSetId()
	return activationTx
}

func newBuilder(activationDb ATXDBProvider) *Builder {
	b := NewBuilder(nodeId, coinbase, &MockSigning{}, activationDb, net, meshProvider, layersPerEpoch, nipstBuilder, postProver, nil, isSynced(true), NewMockDB(), lg.WithName("atxBuilder"))
	b.commitment = commitment
	return b
}

func setActivesetSizeInCache(t *testing.T, activesetSize uint32) {
	view, err := meshProvider.GetOrphanBlocksBefore(meshProvider.LatestLayer())
	assert.NoError(t, err)
	sort.Slice(view, func(i, j int) bool {
		return view[i] < view[j]
	})
	h, err := types.CalcBlocksHash12(view)
	assert.NoError(t, err)
	activesetCache.Add(h, activesetSize)
}

func lastTransmittedAtx(t *testing.T) types.ActivationTx {
	var signedAtx types.ActivationTx
	err := types.BytesToInterface(net.lastTransmission, &signedAtx)
	require.NoError(t, err)
	return signedAtx
}

func assertLastAtx(r *require.Assertions, posAtx, prevAtx *types.ActivationTxHeader, layersPerEpoch uint16) {
	sigAtx, err := types.BytesAsAtx(net.lastTransmission)
	r.NoError(err)

	atx := sigAtx
	r.Equal(nodeId, atx.NodeId)
	if prevAtx != nil {
		r.Equal(prevAtx.Sequence+1, atx.Sequence)
		r.Equal(prevAtx.Id(), atx.PrevATXId)
		r.Nil(atx.Commitment)
		r.Nil(atx.CommitmentMerkleRoot)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(*types.EmptyAtxId, atx.PrevATXId)
		r.NotNil(atx.Commitment)
		r.NotNil(atx.CommitmentMerkleRoot)
	}
	r.Equal(posAtx.Id(), atx.PositioningAtx)
	r.Equal(posAtx.PubLayerIdx.Add(layersPerEpoch), atx.PubLayerIdx)
	r.Equal(defaultActiveSetSize, atx.ActiveSetSize)
	r.Equal(defaultView, atx.View)
	r.Equal(poetRef, atx.GetPoetProofRef())
}

func storeAtx(r *require.Assertions, activationDb *ActivationDb, atx *types.ActivationTx, lg log.Log) {
	epoch := atx.PubLayerIdx.GetEpoch(layersPerEpoch)
	lg.Info("stored ATX in epoch %v", epoch)
	err := activationDb.StoreAtx(epoch, atx)
	r.NoError(err)
}

func publishAtx(b *Builder, meshLayer types.LayerID, clockEpoch types.EpochId, buildNipstLayerDuration uint16) (published bool, err error) {
	net.lastTransmission = nil
	meshProvider.latestLayer = meshLayer
	nipstBuilder.buildNipstFunc = func(challenge *types.Hash32) (*types.NIPST, error) {
		meshProvider.latestLayer = meshLayer.Add(buildNipstLayerDuration)
		return NewNIPSTWithChallenge(challenge, poetRef), nil
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
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, prevAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)
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
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_PrevATXWithoutPrevATX(t *testing.T) {
	r := require.New(t)

	// setup
	activationDb := newActivationDb()
	b := newBuilder(activationDb)
	setActivesetSizeInCache(t, defaultActiveSetSize)
	defer activesetCache.Purge()

	challenge := newChallenge(otherNodeId /*👀*/, 1, prevAtxId, prevAtxId, postGenesisEpochLayer)
	posAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	challenge = newChallenge(nodeId /*👀*/, 0, *types.EmptyAtxId, posAtx.Id(), postGenesisEpochLayer)
	challenge.CommitmentMerkleRoot = commitment.MerkleRoot
	prevAtx := newAtx(challenge, 5, defaultView, npst)
	prevAtx.Commitment = commitment
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, posAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)
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
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "cannot find pos atx in epoch 2: cannot find pos atx id: current posAtx (epoch 1) does not belong to the requested epoch (2)")
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
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "cannot find pos atx in epoch 2: cannot find pos atx id: current posAtx (epoch 1) does not belong to the requested epoch (2)")
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
	storeAtx(r, activationDb, prevAtx, log.NewDefault("storeAtx"))

	// create and publish ATX
	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, prevAtx.ActivationTxHeader, prevAtx.ActivationTxHeader, layersPerEpoch)

	// assert that another ATX cannot be published
	published, err = publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch) // 👀
	r.NoError(err)
	r.False(published)
}

func TestBuilder_PublishActivationTx_FailsWhenNipstBuilderFails(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	nipstBuilder := &NipstErrBuilderMock{} // 👀 mock that returns error from BuildNipst()
	b := NewBuilder(nodeId, coinbase, &MockSigning{}, activationDb, net, meshProvider, layersPerEpoch, nipstBuilder, postProver, nil, isSynced(true), NewMockDB(), lg.WithName("atxBuilder"))
	b.commitment = commitment

	challenge := newChallenge(otherNodeId /*👀*/, 1, prevAtxId, prevAtxId, postGenesisEpochLayer)
	posAtx := newAtx(challenge, 5, defaultView, npst)
	storeAtx(r, activationDb, posAtx, log.NewDefault("storeAtx"))

	published, err := publishAtx(b, postGenesisEpochLayer+1, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "cannot create Nipst: Nipst builder error")
	r.False(published)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	r := require.New(t)

	activationDb := newActivationDb()
	b := newBuilder(activationDb)

	atx := types.NewActivationTx(nodeId, coinbase, 1, prevAtxId, 5, 1, prevAtxId, defaultActiveSetSize, []types.BlockID{1, 2, 3}, npst)
	storeAtx(r, activationDb, atx, log.NewDefault("storeAtx"))

	view, err := b.mesh.GetOrphanBlocksBefore(meshProvider.LatestLayer())
	assert.NoError(t, err)
	act := types.NewActivationTx(b.nodeId, coinbase, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), defaultActiveSetSize, view, npst)

	bt, err := types.InterfaceToBytes(act)
	assert.NoError(t, err)
	a, err := types.BytesAsAtx(bt)
	assert.NoError(t, err)
	bt2, err := types.InterfaceToBytes(a)
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
		storeAtx(r, activationDb, atx, log.NewDefault("storeAtx"))
	}

	challenge := newChallenge(nodeId, 1, prevAtxId, prevAtxId, postGenesisEpochLayer+3)
	prevATX := newAtx(challenge, 5, defaultView, npst)
	b.prevATX = prevATX.ActivationTxHeader

	published, err := publishAtx(b, postGenesisEpochLayer+4, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)

	newAtx := lastTransmittedAtx(t)
	r.Equal(prevATX.Id(), newAtx.PrevATXId)

	posAtx, err := activationDb.GetAtxHeader(newAtx.PositioningAtx)
	r.NoError(err)

	assertLastAtx(r, posAtx, prevATX.ActivationTxHeader, layersPerEpoch)

	t.Skip("proves https://github.com/spacemeshos/go-spacemesh/issues/1166")
	// check pos & prev has the same PubLayerIdx
	r.Equal(prevATX.PubLayerIdx, posAtx.PubLayerIdx)
}

func TestBuilder_SignAtx(t *testing.T) {
	ed := signing.NewEdSigner()
	nodeId := types.NodeId{ed.PublicKey().String(), []byte("bbbbb")}
	activationDb := NewActivationDb(database.NewMemDatabase(), &MockIdStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(nodeId, coinbase, ed, activationDb, net, meshProvider, layersPerEpoch, nipstBuilder, postProver, nil, isSynced(true), NewMockDB(), lg.WithName("atxBuilder"))

	prevAtx := types.AtxId(types.HexToHash32("0x111"))
	atx := types.NewActivationTx(nodeId, coinbase, 1, prevAtx, 15, 1, prevAtx, 5, []types.BlockID{1, 2, 3}, npst)
	atxBytes, err := types.InterfaceToBytes(atx.InnerActivationTx)
	assert.NoError(t, err)
	signed, err := b.SignAtx(atx)
	assert.NoError(t, err)

	pubkey, err := ed25519.ExtractPublicKey(atxBytes, signed.Sig)
	assert.NoError(t, err)
	assert.Equal(t, ed.PublicKey().Bytes(), []byte(pubkey))

	ok := signing.Verify(signing.NewPublicKey(util.Hex2Bytes(atx.NodeId.Key)), atxBytes, signed.Sig)
	assert.True(t, ok)

}

func TestBuilder_NipstPublishRecovery(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
	coinbase := types.HexToAddress("0xaaa")
	net := &NetMock{}
	layers := &MeshProviderMock{}
	nipstBuilder := &NipstBuilderMock{}
	layersPerEpoch := uint16(10)
	lg := log.NewDefault(id.Key[:5])
	db := NewMockDB()
	sig := &MockSigning{}
	activationDb := NewActivationDb(database.NewMemDatabase(), &MockIdStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	b := NewBuilder(id, coinbase, sig, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	prevAtx := types.AtxId(types.HexToHash32("0x111"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipstBuilder.poetRef = poetRef
	npst := NewNIPSTWithChallenge(&chlng, poetRef)

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
	npst2 := NewNIPSTWithChallenge(bytes, poetRef)
	assert.NoError(t, err)

	setActivesetSizeInCache(t, defaultActiveSetSize)

	act := types.NewActivationTx(b.nodeId, coinbase, b.GetLastSequence(b.nodeId)+1, atx.Id(), atx.PubLayerIdx+10, 0, atx.Id(), defaultActiveSetSize, defaultView, npst2)
	err = b.PublishActivationTx(1)
	assert.Error(t, err)

	//test load in correct epoch
	b = NewBuilder(id, coinbase, &MockSigning{}, activationDb, net, layers, layersPerEpoch, nipstBuilder, postProver, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layers.latestLayer = 22
	err = b.PublishActivationTx(1)
	assert.NoError(t, err)
	signed, err := b.SignAtx(act)
	assert.NoError(t, err)
	bts, err := types.InterfaceToBytes(signed)
	assert.NoError(t, err)
	assert.Equal(t, bts, net.lastTransmission)

	b = NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	err = b.PublishActivationTx(1)
	assert.Error(t, err)
	db.hadNone = false
	//test load challenge in later epoch - Nipst should be truncated
	b = NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
	assert.Error(t, err)
	err = b.loadChallenge()
	assert.NoError(t, err)
	err = b.PublishActivationTx(3)
	// This 👇 ensures that handing of the challenge succeeded and the code moved on to the next part
	assert.EqualError(t, err, "cannot find pos atx in epoch 3: cannot find pos atx id: current posAtx (epoch 1) does not belong to the requested epoch (3)")
	assert.True(t, db.hadNone)
}

func TestStartPost(t *testing.T) {
	id := types.NodeId{"aaaaaa", []byte("bbbbb")}
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

	activationDb := NewActivationDb(database.NewMemDatabase(), &MockIdStore{}, mesh.NewMemMeshDB(lg.WithName("meshDB")), layersPerEpoch, &ValidatorMock{}, lg.WithName("atxDB1"))
	builder := NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))

	// Attempt to initialize with invalid space.
	// This test verifies that the params are being set in the post client.
	assert.Nil(t, builder.commitment)
	err = builder.StartPost(coinbase2, drive, 1000)
	assert.EqualError(t, err, "space (1000) must be a power of 2")
	assert.Nil(t, builder.commitment)
	assert.Equal(t, postProver.Cfg().SpacePerUnit, uint64(1000))

	// Initialize.
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
	execBuilder := NewBuilder(id, coinbase, &MockSigning{}, activationDb, &FaultyNetMock{}, layers, layersPerEpoch, nipstBuilder, postProver, nil, func() bool { return true }, db, lg.WithName("atxBuilder"))
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
