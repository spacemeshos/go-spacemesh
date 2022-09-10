package activation

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation/mocks"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/niposts"
)

// ========== Vars / Consts ==========

const (
	layersPerEpoch   = 10
	postGenesisEpoch = 2

	testTickSize = 1
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

var (
	sig         = NewMockSigner()
	otherSig    = NewMockSigner()
	coinbase    = types.GenerateAddress([]byte("33333"))
	goldenATXID = types.ATXID(types.HexToHash32("77777"))
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	chlng       = types.HexToHash32("55555")
	poetRef     = types.BytesToHash([]byte("66666"))
	poetBytes   = []byte("66666")

	postGenesisEpochLayer = types.NewLayerID(22)

	net               = &NetMock{}
	layerClockMock    = &LayerClockMock{}
	nipostBuilderMock = &NIPostBuilderMock{}
	nipost            = newNIPostWithChallenge(&chlng, poetBytes)
	initialPost       = &types.Post{
		Nonce:   0,
		Indices: make([]byte, 10),
	}
)

// ========== Mocks ==========

type NetMock struct {
	lastTransmission []byte
	atxHdlr          atxHandler
}

func (n *NetMock) Publish(_ context.Context, _ string, d []byte) error {
	n.lastTransmission = d
	go n.hookToAtxPool(d)
	return nil
}

func (n *NetMock) hookToAtxPool(transmission []byte) {
	if atx, err := types.BytesToAtx(transmission); err == nil {
		if err := atx.CalcAndSetID(); err != nil {
			panic(err)
		}
		if err := atx.CalcAndSetNodeID(); err != nil {
			panic(err)
		}

		if n.atxHdlr != nil {
			if atxDb, ok := n.atxHdlr.(*Handler); ok {
				vAtx, err := atx.Verify(0, 1)
				if err != nil {
					panic(err)
				}
				if err := atxDb.StoreAtx(context.TODO(), vAtx); err != nil {
					panic(err)
				}
			}
		}
	}
}

func NewMockSigner() *MockSigning {
	return &MockSigning{signing.NewEdSigner()}
}

// TODO(mafa): replace this mock with the generated mock from "github.com/spacemeshos/go-spacemesh/signing/mocks".
type MockSigning struct {
	signer *signing.EdSigner
}

func (ms *MockSigning) NodeID() types.NodeID {
	return types.BytesToNodeID(ms.signer.PublicKey().Bytes())
}

func (ms *MockSigning) Sign(m []byte) []byte {
	return ms.signer.Sign(m)
}

type NIPostBuilderMock struct {
	poetRef         []byte
	buildNIPostFunc func(challenge *types.Hash32) (*types.NIPost, error)
	SleepTime       int
}

func (np NIPostBuilderMock) updatePoETProver(PoetProvingServiceClient) {}

func (np *NIPostBuilderMock) BuildNIPost(_ context.Context, challenge *types.Hash32, _ chan struct{}) (*types.NIPost, error) {
	if np.buildNIPostFunc != nil {
		return np.buildNIPostFunc(challenge)
	}
	return newNIPostWithChallenge(challenge, np.poetRef), nil
}

type NIPostErrBuilderMock struct{}

func (np *NIPostErrBuilderMock) updatePoETProver(PoetProvingServiceClient) {}

func (np *NIPostErrBuilderMock) BuildNIPost(context.Context, *types.Hash32, chan struct{}) (*types.NIPost, error) {
	return nil, fmt.Errorf("NIPost builder error")
}

type ValidatorMock struct{}

// A compile time check to ensure that ValidatorMock fully implements the nipostValidator interface.
var _ nipostValidator = (*ValidatorMock)(nil)

func (*ValidatorMock) Validate(signing.PublicKey, *types.NIPost, types.Hash32, uint) (uint64, error) {
	return 1, nil
}

func (*ValidatorMock) ValidatePost([]byte, *types.Post, *types.PostMetadata, uint) error {
	return nil
}

type FaultyNetMock struct {
	bt     []byte
	retErr bool
}

func (n *FaultyNetMock) Publish(_ context.Context, _ string, d []byte) error {
	n.bt = d
	if n.retErr {
		return fmt.Errorf("faulty")
	}
	// not calling `go hookToAtxPool(d)`
	return nil
}

// ========== Helper functions ==========

func newCachedDB(tb testing.TB) *datastore.CachedDB {
	return datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
}

func newAtxHandler(tb testing.TB, cdb *datastore.CachedDB) *Handler {
	return NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, &ValidatorMock{}, logtest.New(tb).WithName("atxHandler"))
}

func newChallenge(sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID) types.NIPostChallenge {
	return types.NIPostChallenge{
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
	}
}

func newAtx(t testing.TB, challenge types.NIPostChallenge, sig *MockSigning, nipost *types.NIPost, numUnits uint, coinbase types.Address) *types.ActivationTx {
	atx := types.NewActivationTx(challenge, coinbase, nipost, numUnits, nil)
	require.NoError(t, SignAtx(sig, atx))
	require.NoError(t, atx.CalcAndSetID())
	require.NoError(t, atx.CalcAndSetNodeID())
	return atx
}

func newActivationTx(
	t testing.TB,
	sig *MockSigning,
	sequence uint64,
	prevATX types.ATXID,
	positioningATX types.ATXID,
	pubLayerID types.LayerID,
	startTick, numTicks uint64,
	coinbase types.Address,
	numUnits uint,
	nipost *types.NIPost,
) *types.VerifiedActivationTx {
	challenge := newChallenge(sequence, prevATX, positioningATX, pubLayerID)
	atx := newAtx(t, challenge, sig, nipost, numUnits, coinbase)
	vAtx, err := atx.Verify(startTick, numTicks)
	require.NoError(t, err)
	return vAtx
}

type LayerClockMock struct {
	currentLayer types.LayerID
}

func (l *LayerClockMock) LayerToTime(types.LayerID) time.Time {
	return time.Time{}
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

func newBuilder(tb testing.TB, cdb *datastore.CachedDB, hdlr atxHandler) *Builder {
	net.atxHdlr = hdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, hdlr, net, nipostBuilderMock, &postSetupProviderMock{},
		layerClockMock, &mockSyncer{}, logtest.New(tb).WithName("atxBuilder"))
	b.initialPost = initialPost
	return b
}

func lastTransmittedAtx(t *testing.T) types.ActivationTx {
	var signedAtx types.ActivationTx
	err := codec.Decode(net.lastTransmission, &signedAtx)
	require.NoError(t, err)
	return signedAtx
}

func assertLastAtx(r *require.Assertions, posAtx, prevAtx *types.VerifiedActivationTx, layersPerEpoch uint32) {
	sigAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	r.NoError(sigAtx.CalcAndSetNodeID())

	atx := sigAtx
	r.Equal(sig.NodeID(), atx.NodeID())
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

func storeAtx(r *require.Assertions, atxHdlr *Handler, atx *types.VerifiedActivationTx, lg log.Log) {
	err := atxHdlr.StoreAtx(context.TODO(), atx)
	r.NoError(err)
}

func publishAtx(b *Builder, clockEpoch types.EpochID, buildNIPostLayerDuration uint32) (published, builtNIPost bool, err error) {
	net.lastTransmission = nil
	nipostBuilderMock.buildNIPostFunc = func(challenge *types.Hash32) (*types.NIPost, error) {
		builtNIPost = true
		layerClockMock.currentLayer = layerClockMock.currentLayer.Add(buildNIPostLayerDuration)
		return newNIPostWithChallenge(challenge, poetBytes), nil
	}
	layerClockMock.currentLayer = clockEpoch.FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	nipostBuilderMock.buildNIPostFunc = nil
	return net.lastTransmission != nil, builtNIPost, err
}

// ========== Tests ==========

func addPrevAtx(t *testing.T, db sql.Executor, epoch types.EpochID) {
	prevAtx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer(),
			},
		},
	}
	require.NoError(t, SignAtx(sig, prevAtx))
	vAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, vAtx, time.Now()))
}

func TestBuilder_waitForFirstATX(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	poetCfg := PoetConfig{
		PhaseShift:  5 * time.Millisecond,
		CycleGap:    2 * time.Millisecond,
		GracePeriod: time.Millisecond,
	}
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, &postSetupProviderMock{},
		mClock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"),
		WithPoetConfig(poetCfg))
	b.initialPost = initialPost

	ch := make(chan struct{}, 1)
	close(ch)
	current := types.NewLayerID(layersPerEpoch * 2) // first layer of epoch 2
	addPrevAtx(t, cdb, current.GetEpoch()-1)
	mClock.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
	mClock.EXPECT().LayerToTime(current).Return(time.Now().Add(100 * time.Millisecond))
	require.True(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_waitForFirstATX_nextEpoch(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	poetCfg := PoetConfig{
		PhaseShift:  5 * time.Millisecond,
		CycleGap:    2 * time.Millisecond,
		GracePeriod: time.Millisecond,
	}
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, &postSetupProviderMock{},
		mClock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"),
		WithPoetConfig(poetCfg))
	b.initialPost = initialPost

	ch := make(chan struct{}, 1)
	close(ch)
	current := types.NewLayerID(layersPerEpoch * 2) // first layer of epoch 2
	addPrevAtx(t, cdb, current.GetEpoch()-1)
	mClock.EXPECT().GetCurrentLayer().Return(current)
	mClock.EXPECT().LayerToTime(current).Return(time.Now().Add(-5 * time.Millisecond))
	mClock.EXPECT().AwaitLayer(current.Add(layersPerEpoch)).Return(ch)
	mClock.EXPECT().GetCurrentLayer().Return(current.Add(layersPerEpoch)).AnyTimes()
	require.True(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_waitForFirstATX_Genesis(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b.layerClock = mClock

	current := types.NewLayerID(0)
	mClock.EXPECT().GetCurrentLayer().Return(current)
	require.False(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_waitForFirstATX_NoWait(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b.layerClock = mClock

	current := types.NewLayerID(layersPerEpoch)
	addPrevAtx(t, cdb, current.GetEpoch())
	mClock.EXPECT().GetCurrentLayer().Return(current)
	require.False(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_StartSmeshingCoinbase(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)

	coinbase := types.Address{1, 1, 1}
	require.NoError(t, builder.StartSmeshing(coinbase, PostSetupOpts{}))
	t.Cleanup(func() { builder.StopSmeshing(true) })
	require.Equal(t, coinbase, builder.Coinbase())
}

func TestBuilder_RestartSmeshing(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	sessionChan := make(chan struct{})
	close(sessionChan)
	builder := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock,
		&postSetupProviderMock{sessionChan: sessionChan},
		layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	builder.initialPost = initialPost

	for i := 0; i < 100; i++ {
		require.NoError(t, builder.StartSmeshing(types.Address{}, PostSetupOpts{}))
		// NOTE(dshulyak) this is a poor way to test that smeshing started and didn't exit immediately,
		// but proper test requires adding quite a lot of additional mocking and general refactoring.
		time.Sleep(400 * time.Microsecond)
		require.True(t, builder.Smeshing())
		require.NoError(t, builder.StopSmeshing(true))
		require.False(t, builder.Smeshing())
	}
}

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPrevAtx, logtest.New(t).WithName("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPrevAtx, vPrevAtx, layersPerEpoch)

	// create and publish another ATX
	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	r.NoError(prevAtx.CalcAndSetID())
	vPublishedAtx, err := publishedAtx.Verify(0, 1)
	r.NoError(err)
	published, _, err = publishAtx(b, postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPublishedAtx, vPublishedAtx, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vAtx, logtest.New(t).WithName("storeAtx"))

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, faultyNet, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "broadcast: failed to broadcast ATX: faulty")
	r.False(published)

	// create and attempt to publish ATX
	faultyNet.retErr = false
	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, faultyNet, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	published, builtNipost, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.ErrorIs(err, ErrATXChallengeExpired)
	r.False(published)
	r.True(builtNipost)

	// if the network works and we try to publish a new ATX, the timeout should result in a clean state (so a NIPost should be built)
	b.publisher = net
	net.atxHdlr = atxHdlr
	challenge = newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(layersPerEpoch+1))
	posAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPosAtx, logtest.New(t).WithName("storeAtx"))
	published, builtNipost, err = publishAtx(b, postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNipost)
}

func TestBuilder_PublishActivationTx_RebuildNIPostWhenTargetEpochPassed(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vAtx, logtest.New(t).WithName("storeAtx"))

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, faultyNet, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	published, builtNIPost, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "broadcast: failed to broadcast ATX: faulty")
	r.False(published)
	r.True(builtNIPost)

	// We started building the NIPost in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the target epoch (4) has passed, so we'll start the ATX builder in epoch 5 and ensure it builds a
	// new NIPost.

	// if the network works - the ATX should be published
	b.publisher = net
	net.atxHdlr = atxHdlr
	challenge = newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(3*layersPerEpoch))
	posAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPosAtx, logtest.New(t).WithName("storeAtx"))
	published, builtNIPost, err = publishAtx(b, postGenesisEpoch+3, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNIPost)
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPosAtx, logtest.New(t).WithName("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPosAtx, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_PrevATXWithoutPrevATX(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)
	lid := types.NewLayerID(1)
	challenge := newChallenge(1, prevAtxID, prevAtxID, lid.Add(layersPerEpoch))
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPosAtx, logtest.New(t).WithName("storeAtx"))

	challenge = newChallenge(0, *types.EmptyATXID, posAtx.ID(), lid)
	challenge.InitialPostIndices = initialPost.Indices
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	prevAtx.InitialPost = initialPost
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPrevAtx, logtest.New(t).WithName("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPosAtx, vPrevAtx, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_TargetsEpochBasedOnPosAtx(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Sub(layersPerEpoch))
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPosAtx, logtest.New(t).WithName("storeAtx"))

	// create and publish ATX based on the best available posAtx, as long as the node is synced
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPosAtx, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_DoesNotPublish2AtxsInSameEpoch(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPrevAtx, logtest.New(t).WithName("storeAtx"))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPrevAtx, vPrevAtx, layersPerEpoch)

	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	vAtx, err := publishedAtx.Verify(0, 1)
	r.NoError(err)

	// assert that the next ATX is in the next epoch
	published, _, err = publishAtx(b, postGenesisEpoch, layersPerEpoch) // 👀
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vAtx, vAtx, layersPerEpoch)

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

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	nipostBuilder := &NIPostErrBuilderMock{} // 👀 mock that returns error from BuildNIPost()
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.initialPost = initialPost

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPosAtx, logtest.New(t).WithName("storeAtx"))

	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "create ATX: failed to build NIPost: NIPost builder error")
	r.False(published)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)

	atx := newActivationTx(t, sig, 1, prevAtxID, prevAtxID, types.NewLayerID(5), 1, 100, coinbase, 100, nipost)
	storeAtx(r, atxHdlr, atx, logtest.New(t).WithName("storeAtx"))

	act := newActivationTx(t, sig, 2, atx.ID(), atx.ID(), atx.PubLayerID.Add(10), 0, 100, coinbase, 100, nipost)

	bt, err := codec.Encode(act)
	assert.NoError(t, err)

	a, err := types.BytesToAtx(bt)
	assert.NoError(t, err)

	bt2, err := codec.Encode(a)
	assert.NoError(t, err)

	assert.Equal(t, bt, bt2)
}

func TestBuilder_PublishActivationTx_PosAtxOnSameLayerAsPrevAtx(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	lg := logtest.New(t).WithName("storeAtx")
	for i := postGenesisEpochLayer; i.Before(postGenesisEpochLayer.Add(3)); i = i.Add(1) {
		challenge := newChallenge(1, prevAtxID, prevAtxID, i.Mul(layersPerEpoch))
		atx := newAtx(t, challenge, sig, nipost, 2, coinbase)
		vAtx, err := atx.Verify(0, 1)
		r.NoError(err)
		storeAtx(r, atxHdlr, vAtx, lg)
	}

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(3).Mul(layersPerEpoch))
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPrevAtx, lg)

	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)

	newAtx := lastTransmittedAtx(t)
	r.Equal(prevAtx.ID(), newAtx.PrevATXID)

	posAtx, err := cdb.GetFullAtx(newAtx.PositioningATX)
	r.NoError(err)

	assertLastAtx(r, posAtx, vPrevAtx, layersPerEpoch)

	t.Skip("proves https://github.com/spacemeshos/go-spacemesh/issues/1166")
	// check pos & prev has the same PubLayerID
	r.Equal(prevAtx.PubLayerID, posAtx.PubLayerID)
}

func TestBuilder_SignAtx(t *testing.T) {
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	sig := NewMockSigner()
	cdb := newCachedDB(t)
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, &ValidatorMock{}, logtest.New(t).WithName("atxDB1"))
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	challenge := newChallenge(1, prevAtx, prevAtx, types.NewLayerID(15))
	atx := newAtx(t, challenge, sig, nipost, 100, coinbase)
	atxBytes, err := codec.Encode(&atx.InnerActivationTx)
	assert.NoError(t, err)
	err = b.SignAtx(atx)
	assert.NoError(t, err)

	pubkey, err := ed25519.ExtractPublicKey(atxBytes, atx.Sig)
	assert.NoError(t, err)
	assert.Equal(t, sig.NodeID().ToBytes(), []byte(pubkey))

	ok := signing.Verify(signing.NewPublicKey(atx.NodeID().ToBytes()), atxBytes, atx.Sig)
	assert.True(t, ok)
}

func TestBuilder_NIPostPublishRecovery(t *testing.T) {
	coinbase := types.GenerateAddress([]byte("0xaaa"))
	net := &NetMock{}
	nipostBuilder := &NIPostBuilderMock{}
	layersPerEpoch := uint32(10)
	sig := NewMockSigner()
	cdb := newCachedDB(t)
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, &ValidatorMock{}, logtest.New(t).WithName("atxDB1"))
	net.atxHdlr = atxHdlr

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, &FaultyNetMock{}, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipostBuilder.poetRef = poetRef
	npst := newNIPostWithChallenge(&chlng, poetRef)

	atx := newActivationTx(t, sig, 1, prevAtx, prevAtx, types.NewLayerID(15), 1, 100, coinbase, 100, npst)

	err := atxHdlr.StoreAtx(context.TODO(), atx)
	assert.NoError(t, err)

	challenge := types.NIPostChallenge{
		Sequence:       2,
		PrevATXID:      atx.ID(),
		PubLayerID:     atx.PubLayerID.Add(b.layersPerEpoch),
		PositioningATX: atx.ID(),
	}

	challengeHash, err := challenge.Hash()
	assert.NoError(t, err)
	npst2 := newNIPostWithChallenge(challengeHash, poetRef)
	layerClockMock.currentLayer = types.EpochID(1).FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	assert.ErrorIs(t, err, ErrATXChallengeExpired)

	// test load in correct epoch
	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	err = b.PublishActivationTx(context.TODO())
	assert.NoError(t, err)
	challenge = newChallenge(2, atx.ID(), atx.ID(), atx.PubLayerID.Add(10))
	act := newAtx(t, challenge, sig, npst2, 0, coinbase)
	err = b.SignAtx(act)
	assert.NoError(t, err)

	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, &FaultyNetMock{}, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	err = b.buildNIPostChallenge(context.TODO())
	assert.NoError(t, err)
	got, err := niposts.Get(cdb, getNIPostKey())
	require.NoError(t, err)
	require.NotEmpty(t, got)

	// test load challenge in later epoch - NIPost should be truncated
	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, &FaultyNetMock{}, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	err = b.loadChallenge()
	assert.NoError(t, err)
	layerClockMock.currentLayer = types.EpochID(4).FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	// This 👇 ensures that handing of the challenge succeeded and the code moved on to the next part
	assert.ErrorIs(t, err, ErrATXChallengeExpired)
	got, err = niposts.Get(cdb, getNIPostKey())
	require.NoError(t, err)
	require.Empty(t, got)
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

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	nipostBuilder := &NIPostBuilderMock{}
	b := NewBuilder(bc, sig.NodeID(), sig, cdb, atxHdlr, net,
		nipostBuilder, &postSetupProviderMock{}, layerClockMock,
		&mockSyncer{}, logtest.New(t).WithName("atxBuilder"),
		WithPoetRetryInterval(retryInterval),
	)
	b.initialPost = initialPost

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPosAtx, logtest.New(t).WithName("storeAtx"))

	net.lastTransmission = nil
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
		return newNIPostWithChallenge(challenge, poetBytes), nil
	}
	layerClockMock.currentLayer = types.EpochID(postGenesisEpoch).FirstLayer().Add(3)
	ctx, cancel := context.WithCancel(context.Background())
	runnerExit := make(chan struct{})
	go func() {
		b.loop(ctx)
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

func TestBuilder_InitialProofGeneratedOnce(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)

	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	postSetupProvider := &postSetupProviderMock{}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, postSetupProvider,
		layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))

	require.NoError(t, b.generateProof())
	require.Equal(t, 1, postSetupProvider.called)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	storeAtx(r, atxHdlr, vPrevAtx, logtest.New(t).WithName("storeAtx"))

	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPrevAtx, vPrevAtx, layersPerEpoch)

	require.NoError(t, b.generateProof())
	require.Equal(t, 1, postSetupProvider.called)
}
