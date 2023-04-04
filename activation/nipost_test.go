package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-scale/tester"
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
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}

	ctrl := gomock.NewController(t)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any())

	poetProvider := defaultPoetServiceMock(t, []byte("poet"))
	poetProvider.EXPECT().Proof(gomock.Any(), "").DoAndReturn(
		func(context.Context, string) (*types.PoetProofMessage, error) {
			msg := &types.PoetProofMessage{
				PoetProof: types.PoetProof{
					Members: make([]types.Member, 1),
				},
			}
			copy(msg.PoetProof.Members[0][:], challenge.Hash().Bytes())
			return msg, nil
		},
	)

	poetDb := NewMockpoetDbAPI(ctrl)
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

	postProvider := newTestPostManager(t)

	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}

	poetProvider := defaultPoetServiceMock(t, []byte("poet"))
	poetProvider.EXPECT().Proof(gomock.Any(), "").DoAndReturn(
		func(context.Context, string) (*types.PoetProofMessage, error) {
			msg := &types.PoetProofMessage{
				PoetProof: types.PoetProof{
					Members: make([]types.Member, 1),
				},
			}
			copy(msg.PoetProof.Members[0][:], challenge.Hash().Bytes())
			return msg, nil
		},
	)

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	nb := NewNIPostBuilder(postProvider.id, postProvider, []PoetProvingServiceClient{poetProvider},
		poetDb, sql.InMemory(), logtest.New(t), postProvider.signer, PoetConfig{}, mclock)

	r.NoError(postProvider.StartSession(context.Background(), postProvider.opts))
	t.Cleanup(func() { assert.NoError(t, postProvider.Reset()) })

	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	r.NoError(err)
	r.NotNil(nipost)
}

func TestNIPostBuilderWithClients(t *testing.T) {
	t.Parallel()
	logtest.SetupGlobal(t)
	r := require.New(t)

	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().GetProof(gomock.Any()).DoAndReturn(
		func(types.PoetProofRef) (*types.PoetProof, error) {
			proof := &types.PoetProof{
				Members: make([]types.Member, 1),
			}
			copy(proof.Members[0][:], challenge.Hash().Bytes())
			return proof, nil
		},
	)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)

	postProvider := newTestPostManager(t)

	challengeHash := challenge.Hash()
	postCfg := DefaultPostConfig()
	nipost := buildNIPost(t, postProvider, postCfg, challenge, poetDb)
	err := validateNIPost(
		postProvider.id,
		postProvider.commitmentAtxId,
		nipost,
		challengeHash,
		poetDb,
		postCfg,
		postProvider.opts.NumUnits,
		verifying.WithLabelScryptParams(postProvider.opts.Scrypt),
	)
	r.NoError(err)
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

func buildNIPost(tb testing.TB, postProvider *testPostManager, postCfg PostConfig, nipostChallenge types.NIPostChallenge, poetDb poetDbAPI) *types.NIPost {
	require.NoError(tb, postProvider.StartSession(context.Background(), postProvider.opts))
	mclock := defaultLayerClockMock(tb)

	epoch := layersPerEpoch * layerDuration
	poetCfg := PoetConfig{
		PhaseShift: epoch / 5,
		CycleGap:   epoch / 10,
	}

	poetProver := spawnPoet(tb, WithGenesis(time.Now()), WithEpochDuration(epoch), WithPhaseShift(poetCfg.PhaseShift), WithCycleGap(poetCfg.CycleGap))

	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	nb := NewNIPostBuilder(postProvider.id, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(tb), signer, poetCfg, mclock)

	nipost, _, err := nb.BuildNIPost(context.Background(), &nipostChallenge)
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
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}

	postProvider := newTestPostManager(t)

	poetProver := spawnPoet(t, WithGenesis(time.Now()), WithEpochDuration(time.Second))

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().GetProof(gomock.Any()).DoAndReturn(
		func(types.PoetProofRef) (*types.PoetProof, error) {
			proof := &types.PoetProof{
				Members: make([]types.Member, 1),
			}
			copy(proof.Members[0][:], challenge.Hash().Bytes())
			return proof, nil
		},
	)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	nb := NewNIPostBuilder(postProvider.id, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), postProvider.signer, PoetConfig{}, mclock)

	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	r.EqualError(err, "post setup not complete")
	r.Nil(nipost)

	r.NoError(postProvider.StartSession(context.Background(), postProvider.opts))

	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge)
	r.NoError(err)
	r.NotNil(nipost)

	r.NoError(validateNIPost(
		postProvider.id,
		postProvider.goldenATXID,
		nipost,
		challenge.Hash(),
		poetDb,
		postProvider.cfg,
		postProvider.opts.NumUnits,
		verifying.WithLabelScryptParams(postProvider.opts.Scrypt),
	))
}

func TestNIPostBuilder_BuildNIPost(t *testing.T) {
	t.Parallel()
	req := require.New(t)

	postProvider := NewMockpostSetupProvider(gomock.NewController(t))
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete}).AnyTimes()

	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}
	challenge2 := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
		Sequence:   1,
	}

	poetProver := defaultPoetServiceMock(t, []byte("poet"))
	poetProver.EXPECT().Proof(gomock.Any(), "").AnyTimes().DoAndReturn(
		func(context.Context, string) (*types.PoetProofMessage, error) {
			msg := &types.PoetProofMessage{
				PoetProof: types.PoetProof{
					Members: make([]types.Member, 2),
				},
			}
			copy(msg.PoetProof.Members[0][:], challenge.Hash().Bytes())
			copy(msg.PoetProof.Members[1][:], challenge2.Hash().Bytes())
			return msg, nil
		},
	)

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	nodeID := types.NodeID{1}
	nb := NewNIPostBuilder(nodeID, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), sig, PoetConfig{}, mclock)

	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).Times(1)
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
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).Return(nil, nil, fmt.Errorf("error")).Times(1)
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
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).Times(1)
	// check that proof ref is not called again
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)
	req.NotNil(nipost)

	// test state not loading if other challenge provided
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).Times(1)
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge2)
	req.NoError(err)
	req.NotNil(nipost)
}

func TestNIPostBuilder_ManyPoETs_SubmittingChallenge_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)
	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 1).FirstLayer(),
	}

	proof := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			Members: make([]types.Member, 1),
		},
	}
	copy(proof.PoetProof.Members[0][:], challenge.Hash().Bytes())
	ctrl := gomock.NewController(t)
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
		poet.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(proof, nil)
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
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error) {
			return &types.Post{}, &types.PostMetadata{
				Challenge: challenge,
			}, nil
		},
	)
	nb := NewNIPostBuilder(types.NodeID{1}, postProvider, poets, poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)

	// Act
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	req.Equal(challenge.Hash(), *nipost.Challenge)
	ref, _ := proof.Ref()
	req.EqualValues(ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_ManyPoETs_WaitingForProof_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)
	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 1).FirstLayer(),
	}

	proof := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			Members: make([]types.Member, 1),
		},
	}
	copy(proof.PoetProof.Members[0][:], challenge.Hash().Bytes())
	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(t)

	poets := make([]PoetProvingServiceClient, 0, 2)
	{
		poet := defaultPoetServiceMock(t, []byte("poet0"))
		poet.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(proof, nil)
		poets = append(poets, poet)
	}
	{
		poet := defaultPoetServiceMock(t, []byte("poet1"))
		poet.EXPECT().Proof(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ string) (*types.PoetProofMessage, error) {
				// Hang up after the context expired
				<-ctx.Done()
				return nil, ctx.Err()
			},
		)
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	poetCfg := PoetConfig{
		PhaseShift:  layerDuration * layersPerEpoch / 2,
		GracePeriod: layerDuration * layersPerEpoch / 2,
	}
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error) {
			return &types.Post{}, &types.PostMetadata{
				Challenge: challenge,
			}, nil
		},
	)
	nb := NewNIPostBuilder(types.NodeID{1}, postProvider, poets, poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)

	// Act
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	req.Equal(challenge.Hash(), *nipost.Challenge)
	ref, _ := proof.Ref()
	req.EqualValues(ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_ManyPoETs_AllFinished(t *testing.T) {
	t.Parallel()
	// Arrange
	req := require.New(t)

	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}

	proofWorse := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			Members:   make([]types.Member, 1),
			LeafCount: 111,
		},
	}
	copy(proofWorse.PoetProof.Members[0][:], challenge.Hash().Bytes())
	proofBetter := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			Members:   make([]types.Member, 1),
			LeafCount: 999,
		},
	}
	copy(proofBetter.PoetProof.Members[0][:], challenge.Hash().Bytes())

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Times(2).Return(nil)
	mclock := defaultLayerClockMock(t)

	poets := make([]PoetProvingServiceClient, 0, 2)
	{
		poet := defaultPoetServiceMock(t, []byte("poet0"))
		poet.EXPECT().Proof(gomock.Any(), "").Return(proofWorse, nil)
		poets = append(poets, poet)
	}
	{
		poet := defaultPoetServiceMock(t, []byte("poet1"))
		poet.EXPECT().Proof(gomock.Any(), "").Return(proofBetter, nil)
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	postProvider.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error) {
			return &types.Post{}, &types.PostMetadata{
				Challenge: challenge,
			}, nil
		},
	)
	nb := NewNIPostBuilder(types.NodeID{1}, postProvider, poets, poetDb, sql.InMemory(), logtest.New(t), sig, PoetConfig{}, mclock)

	// Act
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge)
	req.NoError(err)

	// Verify
	req.Equal(challenge.Hash(), *nipost.Challenge)
	ref, _ := proofBetter.Ref()
	req.EqualValues(ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_Close(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctrl := gomock.NewController(t)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Status().Return(&PostSetupStatus{State: PostSetupStateComplete})
	poetProver := spawnPoet(t, WithGenesis(time.Now()), WithEpochDuration(time.Second))
	poetDb := NewMockpoetDbAPI(ctrl)
	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 2).FirstLayer(),
	}
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
	challenge := types.NIPostChallenge{
		PubLayerID: (postGenesisEpoch + 1).FirstLayer(),
	}
	poetCfg := PoetConfig{PhaseShift: layerDuration}

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

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
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

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
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

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
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
		poetProver.EXPECT().Proof(gomock.Any(), "").Return(nil, errors.New("failed"))

		nb := NewNIPostBuilder(nodeID, postProver, []PoetProvingServiceClient{poetProver},
			poetDb, sql.InMemory(), logtest.New(t), sig, poetCfg, mclock)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge)
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
