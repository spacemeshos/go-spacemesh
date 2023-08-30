package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func defaultPoetServiceMock(tb testing.TB, id []byte) *MockPoetProvingServiceClient {
	tb.Helper()
	poetClient := NewMockPoetProvingServiceClient(gomock.NewController(tb))
	poetClient.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(&types.PoetRound{}, nil)
	poetClient.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: id}, nil)
	poetClient.EXPECT().PowParams(gomock.Any()).AnyTimes().Return(&PoetPowParams{}, nil)
	return poetClient
}

func defaultLayerClockMock(tb testing.TB) *MocklayerClock {
	mclock := NewMocklayerClock(gomock.NewController(tb))
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(postGenesisEpoch.FirstLayer()) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)
	return mclock
}

func TestNIPostBuilderWithMocks(t *testing.T) {
	t.Parallel()

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	ctrl := gomock.NewController(t)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	postProvider.EXPECT().CommitmentAtx().Return(types.EmptyATXID, nil).AnyTimes()
	postProvider.EXPECT().LastOpts().Return(&PostSetupOpts{}).AnyTimes()
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any())

	nipostValidator := NewMocknipostValidator(ctrl)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	poetProvider := defaultPoetServiceMock(t, []byte("poet"))
	poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
		PoetProof: types.PoetProof{},
	}, []types.Member{types.Member(challenge.Hash())}, nil)

	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nb, err := NewNIPostBuilder(
		types.NodeID{1},
		postProvider,
		poetDb,
		[]string{},
		t.TempDir(),
		logtest.New(t),
		sig,
		PoetConfig{},
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients([]PoetProvingServiceClient{poetProvider}),
	)
	require.NoError(t, err)
	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	require.NoError(t, err)
	require.NotNil(t, nipost)
}

func TestPostSetup(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	postProvider := newTestPostManager(t)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	poetProvider := defaultPoetServiceMock(t, []byte("poet"))
	poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
		PoetProof: types.PoetProof{},
	}, []types.Member{types.Member(challenge.Hash())}, nil)

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	nipostValidator := NewMocknipostValidator(gomock.NewController(t))
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)

	nb, err := NewNIPostBuilder(
		postProvider.id,
		postProvider,
		poetDb,
		[]string{},
		t.TempDir(),
		logtest.New(t),
		postProvider.signer,
		PoetConfig{},
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients([]PoetProvingServiceClient{poetProvider}),
	)
	r.NoError(err)

	r.NoError(postProvider.PrepareInitializer(context.Background(), postProvider.opts))
	r.NoError(postProvider.StartSession(context.Background()))
	t.Cleanup(func() { assert.NoError(t, postProvider.Reset()) })

	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	r.NoError(err)
	r.NotNil(nipost)
}

func TestNIPostBuilderWithClients(t *testing.T) {
	t.Parallel()

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t).WithName("poetDb"))
	postCfg := DefaultPostConfig()
	postCfg.PowDifficulty[0] = 1
	postProvider := newTestPostManager(t, withPostConfig(postCfg))
	logger := logtest.New(t).WithName("validator")
	verifier, err := NewPostVerifier(postProvider.Config(), logger)
	require.NoError(t, err)
	defer verifier.Close()

	v := NewValidator(poetDb, postProvider.Config(), logger, verifier)
	nipost := buildNIPost(t, postProvider, challenge, poetDb, v)
	_, err = v.NIPost(
		context.Background(),
		challenge.PublishEpoch,
		postProvider.id,
		postProvider.commitmentAtxId,
		nipost,
		challenge.Hash(),
		postProvider.opts.NumUnits,
		verifying.WithLabelScryptParams(postProvider.opts.Scrypt),
	)
	require.NoError(t, err)
}

func spawnPoet(tb testing.TB, clientCfg PoetConfig, opts ...HTTPPoetOpt) *HTTPPoetClient {
	tb.Helper()
	poetProver, err := NewHTTPPoetTestHarness(context.Background(), tb.TempDir(), clientCfg, opts...)
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

func buildNIPost(tb testing.TB, postProvider *testPostManager, nipostChallenge types.NIPostChallenge, poetDb poetDbAPI, validator nipostValidator) *types.NIPost {
	require.NoError(tb, postProvider.PrepareInitializer(context.Background(), postProvider.opts))
	require.NoError(tb, postProvider.StartSession(context.Background()))
	mclock := defaultLayerClockMock(tb)

	epoch := layersPerEpoch * layerDuration
	poetCfg := PoetConfig{
		PhaseShift:        epoch / 5,
		CycleGap:          epoch / 10,
		GracePeriod:       epoch / 10,
		RequestRetryDelay: epoch / 100,
		MaxRequestRetries: 10,
	}

	poetProver := spawnPoet(tb, poetCfg, WithGenesis(time.Now()), WithEpochDuration(epoch), WithPhaseShift(poetCfg.PhaseShift), WithCycleGap(poetCfg.CycleGap))

	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	nb, err := NewNIPostBuilder(
		postProvider.id,
		postProvider,
		poetDb,
		[]string{},
		tb.TempDir(),
		logtest.New(tb),
		signer,
		poetCfg,
		mclock,
		WithNipostValidator(validator),
		withPoetClients([]PoetProvingServiceClient{poetProver}),
	)
	require.NoError(tb, err)
	nipost, err := nb.BuildNIPost(context.Background(), &nipostChallenge)
	require.NoError(tb, err)
	return nipost
}

func TestNewNIPostBuilderNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	r := require.New(t)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	challengeHash := challenge.Hash()

	postProvider := newTestPostManager(t)

	epoch := layersPerEpoch * layerDuration
	poetCfg := PoetConfig{
		PhaseShift:        epoch / 5,
		CycleGap:          epoch / 10,
		GracePeriod:       epoch / 10,
		RequestRetryDelay: epoch / 100,
		MaxRequestRetries: 10,
	}

	genesis := time.Now()
	poetProver := spawnPoet(t, poetCfg, WithGenesis(genesis), WithEpochDuration(epoch), WithPhaseShift(poetCfg.PhaseShift), WithCycleGap(poetCfg.CycleGap))

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().GetProof(gomock.Any()).Return(
		&types.PoetProof{}, &challengeHash, nil,
	)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	nipostValidator := NewMocknipostValidator(ctrl)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)

	nb, err := NewNIPostBuilder(
		postProvider.id,
		postProvider,
		poetDb,
		[]string{},
		t.TempDir(),
		logtest.New(t),
		postProvider.signer,
		poetCfg,
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients([]PoetProvingServiceClient{poetProver}),
	)
	require.NoError(t, err)

	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	r.EqualError(err, "post setup not complete")
	r.Nil(nipost)

	r.NoError(postProvider.PrepareInitializer(context.Background(), postProvider.opts))
	r.NoError(postProvider.StartSession(context.Background()))

	nipost, err = nb.BuildNIPost(context.Background(), &challenge)
	r.NoError(err)
	r.NotNil(nipost)

	logger := logtest.New(t).WithName("validator")
	verifier, err := NewPostVerifier(postProvider.cfg, logger)
	r.NoError(err)
	defer verifier.Close()
	v := NewValidator(poetDb, postProvider.cfg, logger, verifier)
	_, err = v.NIPost(
		context.Background(),
		challenge.PublishEpoch,
		postProvider.id,
		postProvider.goldenATXID,
		nipost,
		challenge.Hash(),
		postProvider.opts.NumUnits,
		verifying.WithLabelScryptParams(postProvider.opts.Scrypt),
	)
	r.NoError(err)
}

func TestNIPostBuilder_BuildNIPost(t *testing.T) {
	t.Parallel()
	req := require.New(t)
	dir := t.TempDir()

	ctrl := gomock.NewController(t)
	nipostValidator := NewMocknipostValidator(ctrl)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete}).AnyTimes()
	postProvider.EXPECT().CommitmentAtx().Return(types.EmptyATXID, nil).AnyTimes()
	postProvider.EXPECT().LastOpts().Return(&PostSetupOpts{}).AnyTimes()

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	challenge2 := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
		Sequence:     1,
	}
	challenge3 := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
		Sequence:     2,
	}

	poetProver := defaultPoetServiceMock(t, []byte("poet"))
	poetProver.EXPECT().Proof(gomock.Any(), "").AnyTimes().Return(
		&types.PoetProofMessage{
			PoetProof: types.PoetProof{},
		}, []types.Member{
			types.Member(challenge.Hash()),
			types.Member(challenge2.Hash()),
			types.Member(challenge3.Hash()),
		}, nil,
	)

	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	nodeID := types.NodeID{1}
	nb, err := NewNIPostBuilder(
		nodeID,
		postProvider,
		poetDb,
		[]string{},
		dir,
		logtest.New(t),
		sig,
		PoetConfig{},
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients([]PoetProvingServiceClient{poetProver}),
	)
	req.NoError(err)

	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)
	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)
	req.NotNil(nipost)

	poetDb = NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)

	// fail post exec
	sig, err = signing.NewEdSigner()
	req.NoError(err)
	nb, err = NewNIPostBuilder(
		nodeID,
		postProvider,
		poetDb,
		[]string{},
		dir,
		logtest.New(t),
		sig,
		PoetConfig{},
		mclock,
		withPoetClients([]PoetProvingServiceClient{poetProver}),
	)
	req.NoError(err)
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil, fmt.Errorf("error")).Times(1)
	// check that proof ref is not called again
	nipost, err = nb.BuildNIPost(context.Background(), &challenge2)
	req.Nil(nipost)
	req.Error(err)

	// successful post exec
	poetDb = NewMockpoetDbAPI(ctrl)

	sig, err = signing.NewEdSigner()
	req.NoError(err)
	nb, err = NewNIPostBuilder(
		nodeID,
		postProvider,
		poetDb,
		[]string{},
		dir,
		logtest.New(t),
		sig,
		PoetConfig{},
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients([]PoetProvingServiceClient{poetProver}),
	)
	req.NoError(err)
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)
	// check that proof ref is not called again
	nipost, err = nb.BuildNIPost(context.Background(), &challenge2)
	req.NoError(err)
	req.NotNil(nipost)

	// test state not loading if other challenge provided
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)
	nipost, err = nb.BuildNIPost(context.Background(), &challenge3)
	req.NoError(err)
	req.NotNil(nipost)
}

func TestNIPostBuilder_ManyPoETs_SubmittingChallenge_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 1,
	}

	proof := &types.PoetProofMessage{PoetProof: types.PoetProof{}}

	ctrl := gomock.NewController(t)
	nipostValidator := NewMocknipostValidator(ctrl)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	poets := make([]PoetProvingServiceClient, 0, 2)
	{
		poet := NewMockPoetProvingServiceClient(ctrl)
		poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte("poet0")}, nil)
		poet.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _, _ []byte, _ types.EdSignature, _ types.NodeID, _ PoetPoW) (*types.PoetRound, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		)
		poet.EXPECT().PowParams(gomock.Any()).Return(&PoetPowParams{}, nil)
		poets = append(poets, poet)
	}
	{
		poet := NewMockPoetProvingServiceClient(ctrl)
		poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte("poet1")}, nil)
		poet.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&types.PoetRound{}, nil)
		poet.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(proof, []types.Member{types.Member(challenge.Hash())}, nil)
		poet.EXPECT().PowParams(gomock.Any()).Return(&PoetPowParams{}, nil)
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	poetCfg := PoetConfig{
		PhaseShift: layerDuration * layersPerEpoch / 2,
	}
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	postProvider.EXPECT().CommitmentAtx().Return(types.EmptyATXID, nil).AnyTimes()
	postProvider.EXPECT().LastOpts().Return(&PostSetupOpts{}).AnyTimes()
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, challenge []byte, _ proving.OptionFunc) (*types.Post, *types.PostMetadata, error) {
			return &types.Post{}, &types.PostMetadata{
				Challenge: challenge,
			}, nil
		},
	)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)
	nb, err := NewNIPostBuilder(
		types.NodeID{1},
		postProvider,
		poetDb,
		[]string{},
		t.TempDir(),
		logtest.New(t),
		sig,
		poetCfg,
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients(poets),
	)
	req.NoError(err)

	// Act
	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	ref, _ := proof.Ref()
	req.EqualValues(ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_ManyPoETs_AllFinished(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	proofWorse := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			LeafCount: 111,
		},
	}
	proofBetter := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			LeafCount: 999,
		},
	}

	ctrl := gomock.NewController(t)
	nipostValidator := NewMocknipostValidator(ctrl)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Times(2).Return(nil)
	mclock := defaultLayerClockMock(t)

	poets := make([]PoetProvingServiceClient, 0, 2)
	{
		poet := defaultPoetServiceMock(t, []byte("poet0"))
		poet.EXPECT().Proof(gomock.Any(), "").Return(proofWorse, []types.Member{types.Member(challenge.Hash())}, nil)
		poets = append(poets, poet)
	}
	{
		poet := defaultPoetServiceMock(t, []byte("poet1"))
		poet.EXPECT().Proof(gomock.Any(), "").Return(proofBetter, []types.Member{types.Member(challenge.Hash())}, nil)
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	postProvider.EXPECT().CommitmentAtx().Return(types.EmptyATXID, nil).AnyTimes()
	postProvider.EXPECT().LastOpts().Return(&PostSetupOpts{}).AnyTimes()
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge []byte, _ proving.OptionFunc) (*types.Post, *types.PostMetadata, error) {
			return &types.Post{}, &types.PostMetadata{
				Challenge: challenge,
			}, nil
		},
	)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)
	nb, err := NewNIPostBuilder(
		types.NodeID{1},
		postProvider,
		poetDb,
		[]string{},
		t.TempDir(),
		logtest.New(t),
		sig,
		PoetConfig{},
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients(poets),
	)
	req.NoError(err)

	// Act
	nipost, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	ref, _ := proofBetter.Ref()
	req.EqualValues(ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_Close(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctrl := gomock.NewController(t)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	poetProver := spawnPoet(t, DefaultPoetConfig(), WithGenesis(time.Now()), WithEpochDuration(time.Second))
	poetDb := NewMockpoetDbAPI(ctrl)
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nb, err := NewNIPostBuilder(
		types.NodeID{1},
		postProvider,
		poetDb,
		[]string{},
		t.TempDir(),
		logtest.New(t),
		sig,
		PoetConfig{},
		mclock,
		withPoetClients([]PoetProvingServiceClient{poetProver}),
	)
	r.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	nipost, err := nb.BuildNIPost(ctx, &challenge)
	r.ErrorIs(err, context.Canceled)
	r.Nil(nipost)
}

func TestNIPSTBuilder_PoetUnstable(t *testing.T) {
	t.Parallel()
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 1,
	}
	poetCfg := PoetConfig{
		PhaseShift: layerDuration,
	}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID := types.NodeID{1}

	t.Run("PoetServiceID fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := defaultLayerClockMock(t)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		poetProver := NewMockPoetProvingServiceClient(ctrl)
		poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{}, errors.New("test"))

		nb, err := NewNIPostBuilder(
			nodeID,
			postProver,
			poetDb,
			[]string{},
			t.TempDir(),
			logtest.New(t),
			sig,
			poetCfg,
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)
		nipst, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := defaultLayerClockMock(t)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		poetProver := NewMockPoetProvingServiceClient(ctrl)
		poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte{}}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("test"))
		poetProver.EXPECT().PowParams(gomock.Any()).Return(&PoetPowParams{}, nil)

		nb, err := NewNIPostBuilder(
			nodeID,
			postProver,
			poetDb,
			[]string{},
			t.TempDir(),
			logtest.New(t),
			sig,
			poetCfg,
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)

		nipst, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit hangs", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := defaultLayerClockMock(t)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		poetProver := NewMockPoetProvingServiceClient(ctrl)
		poetProver.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte{}}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _, _ []byte, _ types.EdSignature, _ types.NodeID, _ PoetPoW) (*types.PoetRound, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		)
		poetProver.EXPECT().PowParams(gomock.Any()).Return(&PoetPowParams{}, nil)

		nb, err := NewNIPostBuilder(
			nodeID,
			postProver,
			poetDb,
			[]string{},
			t.TempDir(),
			logtest.New(t),
			sig,
			poetCfg,
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)
		nipst, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("GetProof fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := defaultLayerClockMock(t)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		poetProver := defaultPoetServiceMock(t, []byte("poet"))
		poetProver.EXPECT().Proof(gomock.Any(), "").Return(nil, nil, errors.New("failed"))

		nb, err := NewNIPostBuilder(
			nodeID,
			postProver,
			poetDb,
			[]string{},
			t.TempDir(),
			logtest.New(t),
			sig,
			poetCfg,
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)
		nipst, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
	t.Run("Challenge is not included in proof members", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
		mclock := defaultLayerClockMock(t)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		poetProver := defaultPoetServiceMock(t, []byte("poet"))
		poetProver.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{PoetProof: types.PoetProof{}}, []types.Member{}, nil)

		nb, err := NewNIPostBuilder(
			nodeID,
			postProver,
			poetDb,
			[]string{},
			t.TempDir(),
			logtest.New(t),
			sig,
			poetCfg,
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)
		nipst, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
}

// TestNIPoSTBuilder_StaleChallenge checks if
// it properly detects that the challenge is stale and the poet round has already started.
func TestNIPoSTBuilder_StaleChallenge(t *testing.T) {
	t.Parallel()

	currLayer := types.EpochID(10).FirstLayer()
	genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// Act & Verify
	t.Run("no requests, poet round started", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetProvingServiceClient(ctrl)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			}).AnyTimes()

		nb, err := NewNIPostBuilder(
			types.NodeID{1},
			postProver,
			poetDb,
			[]string{},
			t.TempDir(),
			logtest.New(t),
			sig,
			PoetConfig{},
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)

		challenge := types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch()}
		nipost, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "poet round has already started")
		require.Nil(t, nipost)
	})
	t.Run("no response before deadline", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetProvingServiceClient(ctrl)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			}).AnyTimes()

		dir := t.TempDir()
		nb, err := NewNIPostBuilder(
			types.NodeID{1},
			postProver,
			poetDb,
			[]string{},
			dir,
			logtest.New(t),
			sig,
			PoetConfig{},
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)
		challenge := types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch() - 1}
		state := types.NIPostBuilderState{
			Challenge:    challenge.Hash(),
			PoetRequests: []types.PoetRequest{{}},
		}
		require.NoError(t, saveBuilderState(dir, &state))

		nipost, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "poet proof for pub epoch")
		require.Nil(t, nipost)
	})
	t.Run("too late for proof generation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetProvingServiceClient(ctrl)
		postProver := NewMockpostSetupProvider(ctrl)
		postProver.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			}).AnyTimes()

		dir := t.TempDir()
		nb, err := NewNIPostBuilder(
			types.NodeID{1},
			postProver,
			poetDb,
			[]string{},
			dir,
			logtest.New(t),
			sig,
			PoetConfig{},
			mclock,
			withPoetClients([]PoetProvingServiceClient{poetProver}),
		)
		require.NoError(t, err)
		challenge := types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch() - 1}
		state := types.NIPostBuilderState{
			Challenge:    challenge.Hash(),
			PoetRequests: []types.PoetRequest{{}},
			PoetProofRef: [32]byte{1, 2, 3},
			NIPost:       &types.NIPost{},
		}
		require.NoError(t, saveBuilderState(dir, &state))

		nipost, err := nb.BuildNIPost(context.Background(), &challenge)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "deadline to publish ATX for pub epoch")
		require.Nil(t, nipost)
	})
}

// Test if the NIPoSTBuilder continues after being interrupted just after
// a challenge has been submitted to poet.
func TestNIPoSTBuilder_Continues_After_Interrupted(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 1,
	}

	proof := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			LeafCount: 777,
		},
	}

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	buildCtx, cancel := context.WithCancel(context.Background())

	poet := NewMockPoetProvingServiceClient(ctrl)
	poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte("poet0")}, nil)
	poet.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _, _ []byte, _ types.EdSignature, _ types.NodeID, _ PoetPoW) (*types.PoetRound, error) {
			cancel()
			return &types.PoetRound{}, context.Canceled
		},
	)
	poet.EXPECT().PowParams(gomock.Any()).Times(2).Return(&PoetPowParams{}, nil)
	poet.EXPECT().Proof(gomock.Any(), "").Return(proof, []types.Member{types.Member(challenge.Hash())}, nil)

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	poetCfg := PoetConfig{
		PhaseShift: layerDuration * layersPerEpoch / 2,
	}
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete}).Times(2)
	postProvider.EXPECT().CommitmentAtx().Return(types.EmptyATXID, nil).AnyTimes()
	postProvider.EXPECT().LastOpts().Return(&PostSetupOpts{}).AnyTimes()
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, challenge []byte, _ proving.OptionFunc) (*types.Post, *types.PostMetadata, error) {
			return &types.Post{}, &types.PostMetadata{
				Challenge: challenge,
			}, nil
		},
	)
	nipostValidator := NewMocknipostValidator(ctrl)
	nipostValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)
	nb, err := NewNIPostBuilder(
		types.NodeID{1},
		postProvider,
		poetDb,
		[]string{},
		t.TempDir(),
		logtest.New(t),
		sig,
		poetCfg,
		mclock,
		WithNipostValidator(nipostValidator),
		withPoetClients([]PoetProvingServiceClient{poet}),
	)
	req.NoError(err)

	// Act
	nipost, err := nb.BuildNIPost(buildCtx, &challenge)
	req.ErrorIs(err, context.Canceled)
	req.Nil(nipost)

	// allow to submit in the second try
	poet.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&types.PoetRound{}, nil)

	nipost, err = nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	ref, _ := proof.Ref()
	req.EqualValues(ref[:], nipost.PostMetadata.Challenge)
}

func TestConstructingMerkleProof(t *testing.T) {
	challenge := types.NIPostChallenge{}
	challengeHash := challenge.Hash()

	t.Run("members empty", func(t *testing.T) {
		_, err := constructMerkleProof(challengeHash, []types.Member{})
		require.Error(t, err)
	})
	t.Run("not a member", func(t *testing.T) {
		_, err := constructMerkleProof(challengeHash, []types.Member{{}, {}})
		require.Error(t, err)
	})

	t.Run("is odd member", func(t *testing.T) {
		otherChallenge := types.NIPostChallenge{Sequence: 1}
		members := []types.Member{
			types.Member(challengeHash),
			types.Member(otherChallenge.Hash()),
		}
		proof, err := constructMerkleProof(challengeHash, members)
		require.NoError(t, err)

		root, err := calcRoot(members)
		require.NoError(t, err)

		err = validateMerkleProof(challengeHash[:], proof, root)
		require.NoError(t, err)
	})
	t.Run("is even member", func(t *testing.T) {
		otherChallenge := types.NIPostChallenge{Sequence: 1}
		members := []types.Member{
			types.Member(otherChallenge.Hash()),
			types.Member(challengeHash),
		}
		proof, err := constructMerkleProof(challengeHash, members)
		require.NoError(t, err)

		root, err := calcRoot(members)
		require.NoError(t, err)

		err = validateMerkleProof(challengeHash[:], proof, root)
		require.NoError(t, err)
	})
}

func FuzzBuilderStateConsistency(f *testing.F) {
	tester.FuzzConsistency[types.NIPostBuilderState](f)
}

func FuzzBuilderStateSafety(f *testing.F) {
	tester.FuzzSafety[types.NIPostBuilderState](f)
}

func TestRandomDurationInRange(t *testing.T) {
	t.Parallel()
	test := func(min, max time.Duration) {
		for i := 0; i < 100; i++ {
			waittime := randomDurationInRange(min, max)
			require.LessOrEqual(t, waittime, max)
			require.GreaterOrEqual(t, waittime, min)
		}
	}
	t.Run("min = 0", func(t *testing.T) {
		t.Parallel()
		test(0, 7*time.Second)
	})
	t.Run("min != 0", func(t *testing.T) {
		t.Parallel()
		test(5*time.Second, 7*time.Second)
	})
}

func TestCalculatingGetProofWaitTime(t *testing.T) {
	t.Parallel()
	t.Run("past round end", func(t *testing.T) {
		t.Parallel()
		deadline := proofDeadline(time.Now().Add(-time.Hour), time.Hour*12)
		require.Less(t, time.Until(deadline), time.Duration(0))
	})
	t.Run("before round end", func(t *testing.T) {
		t.Parallel()
		cycleGap := 12 * time.Hour
		deadline := proofDeadline(time.Now().Add(time.Hour), cycleGap)

		require.Greater(t, time.Until(deadline), time.Hour+time.Duration(float64(cycleGap)*minPoetGetProofJitter/100))
		require.LessOrEqual(t, time.Until(deadline), time.Hour+time.Duration(float64(cycleGap)*maxPoetGetProofJitter/100))
	})
}
