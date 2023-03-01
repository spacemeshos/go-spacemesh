package activation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func getPostSetupOpts(tb testing.TB) PostSetupOpts {
	postSetupOpts := DefaultPostSetupOpts()
	postSetupOpts.DataDir = tb.TempDir()
	postSetupOpts.NumUnits = DefaultPostConfig().MinNumUnits
	postSetupOpts.ComputeProviderID = int(initialization.CPUProviderID())
	return postSetupOpts
}

type postSetupProviderMock struct {
	called   int
	setError bool
}

func (p *postSetupProviderMock) Status() *PostSetupStatus {
	status := new(PostSetupStatus)
	status.State = PostSetupStateComplete
	status.LastOpts = p.LastOpts()
	return status
}

func (p *postSetupProviderMock) ComputeProviders() []PostSetupComputeProvider {
	return nil
}

func (p *postSetupProviderMock) Benchmark(PostSetupComputeProvider) (int, error) {
	return 0, nil
}

func (p *postSetupProviderMock) StartSession(_ context.Context, _ PostSetupOpts, _ types.ATXID) error {
	return nil
}

func (p *postSetupProviderMock) Reset() error {
	return nil
}

func (p *postSetupProviderMock) GenerateProof(ctx context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error) {
	p.called++
	if p.setError {
		return nil, nil, fmt.Errorf("error")
	}
	return &types.Post{}, &types.PostMetadata{
		Challenge: challenge,
	}, nil
}

func (p *postSetupProviderMock) VRFNonce() (*types.VRFPostIndex, error) {
	nonce := types.VRFPostIndex(5)
	return &nonce, nil
}

func (p *postSetupProviderMock) LastError() error {
	return nil
}

func (p *postSetupProviderMock) LastOpts() *PostSetupOpts {
	postSetupOpts := DefaultPostSetupOpts()
	postSetupOpts.DataDir, _ = os.MkdirTemp("", "post-test")
	postSetupOpts.NumUnits = DefaultPostConfig().MinNumUnits
	postSetupOpts.ComputeProviderID = int(initialization.CPUProviderID())
	return &postSetupOpts
}

func (p *postSetupProviderMock) Config() PostConfig {
	return DefaultPostConfig()
}

func defaultPoetServiceMock(tb testing.TB, id []byte) *MockPoetProvingServiceClient {
	tb.Helper()
	poetClient := NewMockPoetProvingServiceClient(gomock.NewController(tb))
	poetClient.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, challenge, _ []byte) (*types.PoetRound, error) {
			hash, err := getSerializedChallengeHash(challenge)
			require.NoError(tb, err)
			return &types.PoetRound{
				ChallengeHash: hash,
			}, nil
		})
	poetClient.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(id, nil)
	return poetClient
}

func defaultLayerClockMock(tb testing.TB) *MocklayerClock {
	mclock := NewMocklayerClock(gomock.NewController(tb))
	mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(postGenesisEpoch.FirstLayer().Value) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got.Value))
		}).AnyTimes()
	return mclock
}

func TestNIPostBuilderWithMocks(t *testing.T) {
	t.Parallel()

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}}
	hash := challenge.Hash()

	postProvider := &postSetupProviderMock{}
	poetProvider := defaultPoetServiceMock(t, []byte("poet"))
	poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
		PoetProof: types.PoetProof{Members: [][]byte{hash.Bytes()}},
	}, nil)

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nb := NewNIPostBuilder(types.NodeID{1}, postProvider, []PoetProvingServiceClient{poetProvider},
		poetDb, sql.InMemory(), logtest.New(t), sig, PoetConfig{}, mclock)
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	require.NoError(t, err)
	require.NotNil(t, nipost)
}

func TestPostSetup(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	nodeID := types.NodeID{1}
	goldenATXID := types.ATXID{2, 3, 4}
	postSetupProvider, err := NewPostSetupManager(nodeID, DefaultPostConfig(), logtest.New(t), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postSetupProvider)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}}
	hash := challenge.Hash()

	poetProvider := defaultPoetServiceMock(t, []byte("poet"))
	poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
		PoetProof: types.PoetProof{Members: [][]byte{hash.Bytes()}},
	}, nil)

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nb := NewNIPostBuilder(nodeID, postSetupProvider, []PoetProvingServiceClient{poetProvider},
		poetDb, sql.InMemory(), logtest.New(t), sig, PoetConfig{}, mclock)

	r.NoError(postSetupProvider.StartSession(context.Background(), getPostSetupOpts(t), goldenATXID))
	t.Cleanup(func() { assert.NoError(t, postSetupProvider.Reset()) })

	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	r.NoError(err)
	r.NotNil(nipost)
}

func TestNIPostBuilderWithClients(t *testing.T) {
	logtest.SetupGlobal(t)
	r := require.New(t)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}}
	hash := challenge.Hash()

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().GetProof(gomock.Any()).Return(&types.PoetProof{Members: [][]byte{hash.Bytes()}}, nil)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)

	challengeHash := challenge.Hash()
	postCfg := DefaultPostConfig()
	nipost := buildNIPost(t, r, postCfg, challenge, poetDb)
	err := validateNIPost(types.NodeID{1}, types.ATXID{2, 3, 4}, nipost, challengeHash, poetDb, postCfg, getPostSetupOpts(t).NumUnits)
	r.NoError(err)
}

func spawnMockGateway(tb testing.TB) string {
	tb.Helper()
	gtw := util.NewMockGrpcServer(tb)
	pb.RegisterGatewayServiceServer(gtw.Server, &gatewayService{})
	var eg errgroup.Group
	eg.Go(gtw.Serve)
	tb.Cleanup(func() { assert.NoError(tb, eg.Wait()) })
	tb.Cleanup(gtw.Stop)
	return gtw.Target()
}

func spawnPoet(tb testing.TB, opts ...HTTPPoetOpt) *HTTPPoetClient {
	tb.Helper()
	poetProver, err := NewHTTPPoetTestHarness(context.Background(), tb.TempDir(), opts...)
	require.NoError(tb, err)
	require.NotNil(tb, poetProver)

	var eg errgroup.Group
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(func() {
		cancel()
		assert.NoError(tb, eg.Wait())
	})
	eg.Go(func() error {
		return poetProver.Service.Start(ctx)
	})
	_, err = poetProver.HTTPPoetClient.PoetServiceID(context.Background())
	require.NoError(tb, err)
	return poetProver.HTTPPoetClient
}

func buildNIPost(tb testing.TB, r *require.Assertions, postCfg PostConfig, nipostChallenge types.PoetChallenge, poetDb poetDbAPI) *types.NIPost {
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
	nodeID := types.NodeID{1}
	goldenATXID := types.ATXID{2, 3, 4}
	postProvider, err := NewPostSetupManager(nodeID, postCfg, logtest.New(tb), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postProvider)

	r.NoError(postProvider.StartSession(context.Background(), getPostSetupOpts(tb), goldenATXID))
	mclock := defaultLayerClockMock(tb)

	epoch := layersPerEpoch * layerDuration
	poetCfg := PoetConfig{
		PhaseShift: epoch / 5,
		CycleGap:   epoch / 10,
	}
	gtw := spawnMockGateway(tb)
	poetProver := spawnPoet(tb, WithGateway(gtw), WithGenesis(time.Now()), WithEpochDuration(epoch), WithPhaseShift(poetCfg.PhaseShift), WithCycleGap(poetCfg.CycleGap))

	signer, err := signing.NewEdSigner()
	r.NoError(err)
	nb := NewNIPostBuilder(nodeID, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(tb), signer, poetCfg, mclock)

	nipost, _, err := nb.BuildNIPost(context.Background(), &nipostChallenge)
	r.NoError(err)
	return nipost
}

type gatewayService struct {
	pb.UnimplementedGatewayServiceServer
}

func getSerializedChallengeHash(challenge []byte) (types.Hash32, error) {
	challengeDecoded := types.PoetChallenge{}
	if err := codec.Decode(challenge, &challengeDecoded); err != nil {
		return types.Hash32{}, err
	}
	return challengeDecoded.Hash(), nil
}

func (*gatewayService) VerifyChallenge(_ context.Context, req *pb.VerifyChallengeRequest) (*pb.VerifyChallengeResponse, error) {
	hash, err := getSerializedChallengeHash(req.Challenge)
	if err != nil {
		return nil, err
	}
	return &pb.VerifyChallengeResponse{
		Hash: hash.Bytes(),
	}, nil
}

func TestNewNIPostBuilderNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	minerIDNotInitialized := types.BytesToNodeID([]byte("not initialized"))
	nipostChallenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}}
	challengeHash := nipostChallenge.Hash()

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	postCfg := DefaultPostConfig()
	goldenATXID := types.ATXID{2, 3, 4}
	postProvider, err := NewPostSetupManager(minerIDNotInitialized, postCfg, logtest.New(t), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postProvider)

	gtw := spawnMockGateway(t)
	poetProver := spawnPoet(t, WithGateway(gtw), WithGenesis(time.Now()), WithEpochDuration(time.Second))

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().GetProof(gomock.Any()).Return(&types.PoetProof{Members: [][]byte{challengeHash.Bytes()}}, nil)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	signer, err := signing.NewEdSigner()
	r.NoError(err)

	nb := NewNIPostBuilder(minerIDNotInitialized, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), signer, PoetConfig{}, mclock)

	nipost, _, err := nb.BuildNIPost(context.Background(), &nipostChallenge)
	r.EqualError(err, "post setup not complete")
	r.Nil(nipost)

	r.NoError(postProvider.StartSession(context.Background(), getPostSetupOpts(t), goldenATXID))

	nipost, _, err = nb.BuildNIPost(context.Background(), &nipostChallenge)
	r.NoError(err)
	r.NotNil(nipost)

	r.NoError(validateNIPost(minerIDNotInitialized, goldenATXID, nipost, challengeHash, poetDb, postCfg, getPostSetupOpts(t).NumUnits))
}

func TestNIPostBuilder_BuildNIPost(t *testing.T) {
	t.Parallel()
	req := require.New(t)

	postProvider := &postSetupProviderMock{}

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}}
	challengeHash := challenge.Hash()
	challenge2 := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
		Sequence:   1,
	}}
	challenge2Hash := challenge2.Hash()

	poetProver := defaultPoetServiceMock(t, []byte("poet"))
	poetProver.EXPECT().Proof(gomock.Any(), "").AnyTimes().Return(&types.PoetProofMessage{
		PoetProof: types.PoetProof{Members: [][]byte{challengeHash.Bytes(), challenge2Hash.Bytes()}},
	}, nil)

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	nodeID := types.NodeID{1}
	nb := NewNIPostBuilder(nodeID, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), sig, PoetConfig{}, mclock)

	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)
	req.NotNil(nipost)
	db := sql.InMemory()
	req.Equal(types.NIPostBuilderState{NIPost: &types.NIPost{}}, *nb.state)

	poetDb = NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)

	// fail post exec
	sig, err = signing.NewEdSigner()
	req.NoError(err)
	nb = NewNIPostBuilder(nodeID, postProvider, []PoetProvingServiceClient{poetProver}, poetDb, db, logtest.New(t), sig, PoetConfig{}, mclock)
	postProvider.setError = true
	// check that proof ref is not called again
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge)
	req.Nil(nipost)
	req.Error(err)

	// fail post exec
	poetDb = NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)

	sig, err = signing.NewEdSigner()
	req.NoError(err)
	nb = NewNIPostBuilder(nodeID, postProvider, []PoetProvingServiceClient{poetProver}, poetDb, db, logtest.New(t), sig, PoetConfig{}, mclock)
	postProvider.setError = false
	// check that proof ref is not called again
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge)
	req.NotNil(nipost)
	req.NoError(err)

	req.Equal(3, postProvider.called)
	// test state not loading if other challenge provided
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge2)
	req.NoError(err)
	req.NotNil(nipost)
	req.Equal(4, postProvider.called)
}

func TestNIPostBuilder_ManyPoETs_SubmittingChallenge_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)
	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 1).FirstLayer(),
	}}
	challengeHash := challenge.Hash()

	proof := types.PoetProofMessage{PoetProof: types.PoetProof{Members: [][]byte{challengeHash.Bytes()}}}
	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	poets := make([]PoetProvingServiceClient, 0, 2)
	{
		poet := NewMockPoetProvingServiceClient(ctrl)
		poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return([]byte("poet0"), nil)
		poet.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _, _ []byte) (*types.PoetRound, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
		poets = append(poets, poet)
	}
	{
		poet := NewMockPoetProvingServiceClient(ctrl)
		poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return([]byte("poet1"), nil)
		poet.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, challenge, _ []byte) (*types.PoetRound, error) {
				return &types.PoetRound{
					ChallengeHash: challengeHash,
				}, nil
			})
		poet.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.PoetProofMessage{
			PoetProof: types.PoetProof{Members: [][]byte{challengeHash.Bytes()}},
		}, nil)
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	poetCfg := PoetConfig{
		PhaseShift: layerDuration * layersPerEpoch / 2,
	}
	nb := NewNIPostBuilder(types.NodeID{1}, &postSetupProviderMock{}, poets, poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)

	// Act
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	req.Equal(challengeHash, *nipost.Challenge)
	ref, _ := proof.Ref()
	req.EqualValues(ref, nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_ManyPoETs_WaitingForProof_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)
	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 1).FirstLayer(),
	}}
	challengeHash := challenge.Hash()

	proof := types.PoetProofMessage{PoetProof: types.PoetProof{Members: [][]byte{challengeHash.Bytes()}}}
	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	poets := make([]PoetProvingServiceClient, 0, 2)
	{
		poet := defaultPoetServiceMock(t, []byte("poet0"))
		poet.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&proof, nil)
		poets = append(poets, poet)
	}
	{
		poet := defaultPoetServiceMock(t, []byte("poet1"))
		poet.EXPECT().Proof(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ string) (*types.PoetProofMessage, error) {
			// Hang up after the context expired
			<-ctx.Done()
			return nil, ctx.Err()
		})
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	poetCfg := PoetConfig{
		PhaseShift:  layerDuration * layersPerEpoch / 2,
		GracePeriod: layerDuration * layersPerEpoch / 2,
	}
	nb := NewNIPostBuilder(types.NodeID{1}, &postSetupProviderMock{}, poets, poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)

	// Act
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	req.Equal(challengeHash, *nipost.Challenge)
	ref, _ := proof.Ref()
	req.EqualValues(ref, nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_ManyPoETs_AllFinished(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}}
	challengeHash := challenge.Hash()

	proofWorse := types.PoetProofMessage{
		PoetProof: types.PoetProof{Members: [][]byte{challengeHash.Bytes()}, LeafCount: 111},
	}
	proofBetter := types.PoetProofMessage{
		PoetProof: types.PoetProof{Members: [][]byte{challengeHash.Bytes()}, LeafCount: 999},
	}

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Times(2).Return(nil)
	mclock := defaultLayerClockMock(t)

	poets := make([]PoetProvingServiceClient, 0, 2)
	{
		poet := defaultPoetServiceMock(t, []byte("poet0"))
		poet.EXPECT().Proof(gomock.Any(), "").Return(&proofWorse, nil)
		poets = append(poets, poet)
	}
	{
		poet := defaultPoetServiceMock(t, []byte("poet1"))
		poet.EXPECT().Proof(gomock.Any(), "").Return(&proofBetter, nil)
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	nb := NewNIPostBuilder(types.NodeID{1}, &postSetupProviderMock{}, poets, poetDb, sql.InMemory(), logtest.New(t), sig, PoetConfig{}, mclock)

	// Act
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	req.Equal(challengeHash, *nipost.Challenge)
	ref, _ := proofBetter.Ref()
	req.EqualValues(ref, nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_Close(t *testing.T) {
	r := require.New(t)

	postProvider := &postSetupProviderMock{}
	gtw := spawnMockGateway(t)
	poetProver := spawnPoet(t, WithGateway(gtw), WithGenesis(time.Now()), WithEpochDuration(time.Second))
	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}}
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nb := NewNIPostBuilder(types.NodeID{1}, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), sig, PoetConfig{}, mclock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	nipost, _, err := nb.BuildNIPost(ctx, &challenge)
	r.ErrorIs(err, context.Canceled)
	r.Nil(nipost)
}

func TestNIPSTBuilder_PoetUnstable(t *testing.T) {
	t.Parallel()
	postProver := &postSetupProviderMock{}
	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 1).FirstLayer(),
	}}
	poetCfg := PoetConfig{PhaseShift: layerDuration}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID := types.NodeID{1}

	t.Run("PoetServiceID fails", func(t *testing.T) {
		t.Parallel()
		poetDb := NewMockpoetDbAPI(gomock.NewController(t))
		mclock := defaultLayerClockMock(t)
		poetProver := NewMockPoetProvingServiceClient(gomock.NewController(t))
		poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(nil, errors.New("test"))

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit fails", func(t *testing.T) {
		t.Parallel()
		poetDb := NewMockpoetDbAPI(gomock.NewController(t))
		mclock := defaultLayerClockMock(t)
		poetProver := NewMockPoetProvingServiceClient(gomock.NewController(t))
		poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("test"))

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit hangs", func(t *testing.T) {
		t.Parallel()
		poetDb := NewMockpoetDbAPI(gomock.NewController(t))
		mclock := defaultLayerClockMock(t)
		poetProver := NewMockPoetProvingServiceClient(gomock.NewController(t))
		poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _, _ any) (*types.PoetRound, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit returns invalid hash", func(t *testing.T) {
		t.Parallel()
		poetDb := NewMockpoetDbAPI(gomock.NewController(t))
		mclock := defaultLayerClockMock(t)
		poetProver := NewMockPoetProvingServiceClient(gomock.NewController(t))
		poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).Return(&types.PoetRound{}, nil)

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("GetProof fails", func(t *testing.T) {
		t.Parallel()
		poetDb := NewMockpoetDbAPI(gomock.NewController(t))
		mclock := defaultLayerClockMock(t)
		poetProver := defaultPoetServiceMock(t, []byte("poet"))
		poetProver.EXPECT().Proof(gomock.Any(), "").Return(nil, errors.New("failed"))

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
	t.Run("Challenge is not included in proof members", func(t *testing.T) {
		t.Parallel()
		poetDb := NewMockpoetDbAPI(gomock.NewController(t))
		poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
		mclock := defaultLayerClockMock(t)
		poetProver := defaultPoetServiceMock(t, []byte("poet"))
		poetProver.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{PoetProof: types.PoetProof{}}, nil)

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
}

func FuzzBuilderStateConsistency(f *testing.F) {
	tester.FuzzConsistency[types.NIPostBuilderState](f)
}

func FuzzBuilderStateSafety(f *testing.F) {
	tester.FuzzSafety[types.NIPostBuilderState](f)
}
