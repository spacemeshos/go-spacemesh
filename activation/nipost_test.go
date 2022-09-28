package activation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	minerID       = types.NodeID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	postCfg       types.PostConfig
	postSetupOpts types.PostSetupOpts
)

func init() {
	postCfg = DefaultPostConfig()

	postSetupOpts = DefaultPostSetupOpts()
	postSetupOpts.DataDir, _ = os.MkdirTemp("", "post-test")
	postSetupOpts.NumUnits = postCfg.MinNumUnits
	postSetupOpts.ComputeProviderID = initialization.CPUProviderID()
}

type postSetupProviderMock struct {
	sessionChan chan struct{}
	called      int
	setError    bool
}

// A compile time check to ensure that postSetupProviderMock fully implements the PostProvider interface.
var _ PostSetupProvider = (*postSetupProviderMock)(nil)

func (p *postSetupProviderMock) Status() *types.PostSetupStatus {
	status := new(types.PostSetupStatus)
	status.State = postSetupStateComplete
	status.LastOpts = p.LastOpts()
	status.LastError = p.LastError()
	return status
}

func (p *postSetupProviderMock) StatusChan() <-chan *types.PostSetupStatus {
	return nil
}

func (p *postSetupProviderMock) ComputeProviders() []initialization.ComputeProvider {
	return nil
}

func (p *postSetupProviderMock) Benchmark(initialization.ComputeProvider) (int, error) {
	return 0, nil
}

func (p *postSetupProviderMock) StartSession(opts types.PostSetupOpts, commitmentAtx types.ATXID) (chan struct{}, error) {
	return p.sessionChan, nil
}

func (p *postSetupProviderMock) StopSession(deleteFiles bool) error {
	return nil
}

func (p *postSetupProviderMock) GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error) {
	p.called++
	if p.setError {
		return nil, nil, fmt.Errorf("error")
	}
	return &types.Post{}, &types.PostMetadata{}, nil
}

func (p *postSetupProviderMock) LastError() error {
	return nil
}

func (p *postSetupProviderMock) LastOpts() *types.PostSetupOpts {
	return &postSetupOpts
}

func (p *postSetupProviderMock) Config() types.PostConfig {
	return postCfg
}

func defaultPoetServiceMock(tb testing.TB) (*mocks.MockPoetProvingServiceClient, *gomock.Controller) {
	poetClient, controller := newPoetServiceMock(tb)
	poetClient.EXPECT().Submit(gomock.Any(), gomock.Any()).AnyTimes().Return(&types.PoetRound{}, nil)
	poetClient.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return([]byte{}, nil)
	return poetClient, controller
}

func newPoetServiceMock(tb testing.TB) (*mocks.MockPoetProvingServiceClient, *gomock.Controller) {
	tb.Helper()
	controller := gomock.NewController(tb)
	poetClient := mocks.NewMockPoetProvingServiceClient(controller)
	return poetClient, controller
}

type poetDbMock struct {
	errOn        bool
	unsubscribed bool
}

// A compile time check to ensure that poetDbMock fully implements poetDbAPI.
var _ poetDbAPI = (*poetDbMock)(nil)

func (*poetDbMock) SubscribeToProofRef(poetID []byte, roundID string) chan types.PoetProofRef {
	ch := make(chan types.PoetProofRef)
	go func() {
		ch <- []byte("hello there")
	}()
	return ch
}

func (p *poetDbMock) UnsubscribeFromProofRef(poetID []byte, roundID string) { p.unsubscribed = true }

func (p *poetDbMock) GetMembershipMap(poetRoot types.PoetProofRef) (map[types.Hash32]bool, error) {
	if p.errOn {
		return map[types.Hash32]bool{}, nil
	}
	hash := types.BytesToHash([]byte("anton"))
	hash2 := types.BytesToHash([]byte("anton1"))
	return map[types.Hash32]bool{hash: true, hash2: true}, nil
}

func (p *poetDbMock) GetProof(poetRef types.PoetProofRef) (*types.PoetProof, error) {
	hash := types.BytesToHash([]byte("anton"))
	hash2 := types.BytesToHash([]byte("anton1"))
	return &types.PoetProof{Members: [][]byte{hash.Bytes(), hash2.Bytes()}}, nil
}

func TestNIPostBuilderWithMocks(t *testing.T) {
	assert := require.New(t)

	postProvider := &postSetupProviderMock{}
	poetProvider, controller := defaultPoetServiceMock(t)
	defer controller.Finish()

	poetDb := &poetDbMock{}

	nb := NewNIPostBuilder(minerID, postProvider, poetProvider,
		poetDb, sql.InMemory(), logtest.New(t))
	hash := types.BytesToHash([]byte("anton"))
	nipost, err := nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
	assert.NoError(err)
	assert.NotNil(nipost)
}

func TestPostSetup(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	postSetupProvider, err := NewPostSetupManager(minerID, postCfg, logtest.New(t), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postSetupProvider)

	poetProvider, controller := defaultPoetServiceMock(t)
	defer controller.Finish()

	poetDb := &poetDbMock{}

	nb := NewNIPostBuilder(minerID, postSetupProvider, poetProvider, poetDb, sql.InMemory(), logtest.New(t))

	done, err := postSetupProvider.StartSession(postSetupOpts, goldenATXID)
	r.NoError(err)
	<-done
	defer func() {
		r.NoError(postSetupProvider.LastError())
		r.NoError(postSetupProvider.StopSession(true))
	}()

	hash := types.BytesToHash([]byte("anton"))
	nipost, err := nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
	r.NoError(err)
	r.NotNil(nipost)
}

func TestNIPostBuilderWithClients(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	poetDb := &poetDbMock{}

	nipostChallenge := types.BytesToHash([]byte("anton"))
	nipost := buildNIPost(t, r, postCfg, nipostChallenge, poetDb)

	err := validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, postCfg, postSetupOpts.NumUnits)
	r.NoError(err)
}

func buildNIPost(tb testing.TB, r *require.Assertions, postCfg types.PostConfig, nipostChallenge types.Hash32, poetDb poetDbAPI) *types.NIPost {
	poetProver, err := NewHTTPPoetHarness(true)
	r.NoError(err)
	r.NotNil(poetProver)
	tb.Cleanup(func() {
		err := poetProver.Teardown(true)
		if assert.NoError(tb, err, "failed to tear down harness") {
			tb.Log("harness torn down")
		}
	})

	cdb := newCachedDB(tb)
	postProvider, err := NewPostSetupManager(minerID, postCfg, logtest.New(tb), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postProvider)
	tb.Cleanup(func() {
		r.NoError(postProvider.LastError())
		r.NoError(postProvider.StopSession(true))
	})

	done, err := postProvider.StartSession(postSetupOpts, goldenATXID)
	r.NoError(err)
	<-done

	nb := NewNIPostBuilder(minerID, postProvider, poetProver,
		poetDb, sql.InMemory(), logtest.New(tb))

	nipost, err := nb.BuildNIPost(context.TODO(), &nipostChallenge, goldenATXID, nil)
	r.NoError(err)
	return nipost
}

func TestNewNIPostBuilderNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	minerIDNotInitialized := types.BytesToNodeID([]byte("not initialized"))
	nipostChallenge := types.BytesToHash([]byte("anton"))

	cdb := newCachedDB(t)
	postProvider, err := NewPostSetupManager(minerIDNotInitialized, postCfg, logtest.New(t), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postProvider)
	defer func() {
		r.NoError(postProvider.LastError())
		r.NoError(postProvider.StopSession(true))
	}()

	poetProver, err := NewHTTPPoetHarness(true)
	r.NoError(err)
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.Teardown(true)
		r.NoError(err)
	}()
	poetDb := &poetDbMock{}
	nb := NewNIPostBuilder(minerIDNotInitialized, postProvider, poetProver,
		poetDb, sql.InMemory(), logtest.New(t))

	nipost, err := nb.BuildNIPost(context.TODO(), &nipostChallenge, goldenATXID, nil)
	r.EqualError(err, "post setup not complete")
	r.Nil(nipost)

	done, err := postProvider.StartSession(postSetupOpts, goldenATXID)
	r.NoError(err)
	<-done

	nipost, err = nb.BuildNIPost(context.TODO(), &nipostChallenge, goldenATXID, nil)
	r.NoError(err)
	r.NotNil(nipost)

	err = validateNIPost(minerIDNotInitialized, goldenATXID, nipost, nipostChallenge, poetDb, postCfg, postSetupOpts.NumUnits)
	r.NoError(err)
}

func TestNIPostBuilder_BuildNIPost(t *testing.T) {
	assert := require.New(t)

	postProvider := &postSetupProviderMock{}

	poetProvider, controller := newPoetServiceMock(t)
	poetProvider.EXPECT().PoetServiceID(gomock.Any()).Times(2).Return([]byte{}, nil)
	poetProvider.EXPECT().Submit(gomock.Any(), gomock.Any()).Times(2).Return(&types.PoetRound{}, nil)
	defer controller.Finish()

	poetDb := &poetDbMock{errOn: false}

	nb := NewNIPostBuilder(minerID, postProvider, poetProvider,
		poetDb, sql.InMemory(), logtest.New(t))
	hash := types.BytesToHash([]byte("anton"))
	nipost, err := nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
	assert.NoError(err)
	assert.NotNil(nipost)
	db := sql.InMemory()
	assert.Equal(types.NIPostBuilderState{NIPost: &types.NIPost{}}, *nb.state)

	// fail after getting proof ref
	nb = NewNIPostBuilder(minerID, postProvider, poetProvider, poetDb, db, logtest.New(t))
	poetDb.errOn = true
	nipost, err = nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
	assert.Nil(nipost)
	assert.Error(err)

	// check that proof ref is not called again
	nb = NewNIPostBuilder(minerID, postProvider, poetProvider, poetDb, db, logtest.New(t))
	nipost, err = nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
	assert.Nil(nipost)
	assert.Error(err)

	// fail post exec
	nb = NewNIPostBuilder(minerID, postProvider, poetProvider, poetDb, db, logtest.New(t))
	poetDb.errOn = false
	postProvider.setError = true
	// check that proof ref is not called again
	nipost, err = nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
	assert.Nil(nipost)
	assert.Error(err)

	// fail post exec
	nb = NewNIPostBuilder(minerID, postProvider, poetProvider, poetDb, db, logtest.New(t))
	poetDb.errOn = false
	postProvider.setError = false
	// check that proof ref is not called again
	nipost, err = nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
	assert.NotNil(nipost)
	assert.NoError(err)

	assert.Equal(3, postProvider.called)
	// test state not loading if other challenge provided
	poetProvider.EXPECT().PoetServiceID(gomock.Any()).Return([]byte{}, nil)
	poetProvider.EXPECT().Submit(gomock.Any(), gomock.Any()).Return(&types.PoetRound{}, nil)
	hash2 := types.BytesToHash([]byte("anton1"))
	nipost, err = nb.BuildNIPost(context.TODO(), &hash2, goldenATXID, nil)
	assert.Equal(4, postProvider.called)

	assert.NotNil(nipost)
	assert.NoError(err)
}

func TestValidator_Validate(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	poetDb := &poetDbMock{}
	nipostChallenge := types.BytesToHash([]byte("anton"))

	nipost := buildNIPost(t, r, postCfg, nipostChallenge, poetDb)

	err := validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, postCfg, postSetupOpts.NumUnits)
	r.NoError(err)

	err = validateNIPost(minerID, goldenATXID, nipost, types.BytesToHash([]byte("lerner")), poetDb, postCfg, postSetupOpts.NumUnits)
	r.Contains(err.Error(), "invalid `Challenge`")

	newNIPost := *nipost
	newNIPost.Post = &types.Post{}
	err = validateNIPost(minerID, goldenATXID, &newNIPost, nipostChallenge, poetDb, postCfg, postSetupOpts.NumUnits)
	r.Contains(err.Error(), "invalid Post")

	newPostCfg := postCfg
	newPostCfg.MinNumUnits = postSetupOpts.NumUnits + 1
	err = validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `numUnits`; expected: >=%d, given: %d", newPostCfg.MinNumUnits, postSetupOpts.NumUnits))

	newPostCfg = postCfg
	newPostCfg.MaxNumUnits = postSetupOpts.NumUnits - 1
	err = validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `numUnits`; expected: <=%d, given: %d", newPostCfg.MaxNumUnits, postSetupOpts.NumUnits))

	newPostCfg = postCfg
	newPostCfg.LabelsPerUnit = uint(nipost.PostMetadata.LabelsPerUnit) + 1
	err = validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", newPostCfg.LabelsPerUnit, nipost.PostMetadata.LabelsPerUnit))

	newPostCfg = postCfg
	newPostCfg.BitsPerLabel = uint(nipost.PostMetadata.BitsPerLabel) + 1
	err = validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `BitsPerLabel`; expected: >=%d, given: %d", newPostCfg.BitsPerLabel, nipost.PostMetadata.BitsPerLabel))

	newPostCfg = postCfg
	newPostCfg.K1 = uint(nipost.PostMetadata.K2) - 1
	err = validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `K1`; expected: <=%d, given: %d", newPostCfg.K1, nipost.PostMetadata.K1))

	newPostCfg = postCfg
	newPostCfg.K2 = uint(nipost.PostMetadata.K2) + 1
	err = validateNIPost(minerID, goldenATXID, nipost, nipostChallenge, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `K2`; expected: >=%d, given: %d", newPostCfg.K2, nipost.PostMetadata.K2))
}

func validateNIPost(minerID types.NodeID, commitmentAtx types.ATXID, nipost *types.NIPost, challenge types.Hash32, poetDb poetDbAPI, postCfg types.PostConfig, numUnits uint) error {
	v := &Validator{poetDb, postCfg}
	commitment := GetCommitmentBytes(minerID, commitmentAtx)
	_, err := v.Validate(commitment[:], nipost, challenge, numUnits)
	return err
}

func TestNIPostBuilder_TimeoutUnsubscribe(t *testing.T) {
	r := require.New(t)

	postProvider := &postSetupProviderMock{}
	poetProvider, controller := defaultPoetServiceMock(t)
	defer controller.Finish()

	poetDb := &poetDbMock{}

	nb := NewNIPostBuilder(minerID, postProvider, poetProvider,
		poetDb, sql.InMemory(), logtest.New(t))
	hash := types.BytesToHash([]byte("anton"))
	poetDb.unsubscribed = false
	nipost, err := nb.BuildNIPost(context.TODO(), &hash, goldenATXID, closedChan) // closedChan will timeout immediately
	r.ErrorIs(err, ErrATXChallengeExpired)
	r.Nil(nipost)
	r.True(poetDb.unsubscribed)
}

func TestNIPostBuilder_Close(t *testing.T) {
	r := require.New(t)

	postProvider := &postSetupProviderMock{}
	poetProvider, controller := defaultPoetServiceMock(t)
	defer controller.Finish()

	poetDb := &poetDbMock{}

	nb := NewNIPostBuilder(minerID, postProvider, poetProvider,
		poetDb, sql.InMemory(), logtest.New(t))
	hash := types.BytesToHash([]byte("anton"))
	ctx, close := context.WithCancel(context.Background())
	close()
	nipost, err := nb.BuildNIPost(ctx, &hash, goldenATXID, nil)
	r.ErrorIs(err, ErrStopRequested)
	r.Nil(nipost)
}

func TestNIPSTBuilder_PoetUnstable(t *testing.T) {
	postProver := &postSetupProviderMock{}
	poetProver, controller := newPoetServiceMock(t)
	defer controller.Finish()

	poetDb := &poetDbMock{}

	nb := NewNIPostBuilder(minerID, postProver, poetProver,
		poetDb, sql.InMemory(), logtest.New(t))

	t.Run("PoetServiceID", func(t *testing.T) {
		poetProver.EXPECT().PoetServiceID(gomock.Any()).Return(nil, errors.New("test"))
		hash := types.BytesToHash([]byte("test"))
		nipst, err := nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})

	t.Run("Submit", func(t *testing.T) {
		poetProver.EXPECT().PoetServiceID(gomock.Any()).Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any()).Return(nil, errors.New("test"))
		hash := types.BytesToHash([]byte("test"))
		nipst, err := nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})

	t.Run("NotIncluded", func(t *testing.T) {
		poetProver.EXPECT().PoetServiceID(gomock.Any()).Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any()).Return(&types.PoetRound{}, nil)
		hash := types.BytesToHash([]byte("test")) // see poetDbMock for included challenges
		nipst, err := nb.BuildNIPost(context.TODO(), &hash, goldenATXID, nil)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
}

func FuzzBuilderStateConsistency(f *testing.F) {
	tester.FuzzConsistency[types.NIPostBuilderState](f)
}

func FuzzBuilderStateSafety(f *testing.F) {
	tester.FuzzSafety[types.NIPostBuilderState](f)
}
