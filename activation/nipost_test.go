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
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	minerID       = types.NodeID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	postCfg       PostConfig
	postSetupOpts PostSetupOpts
)

func init() {
	postCfg = DefaultPostConfig()

	postSetupOpts = DefaultPostSetupOpts()
	postSetupOpts.DataDir, _ = os.MkdirTemp("", "post-test")
	postSetupOpts.NumUnits = postCfg.MinNumUnits
	postSetupOpts.ComputeProviderID = int(initialization.CPUProviderID())
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

func (p *postSetupProviderMock) StatusChan() <-chan *PostSetupStatus {
	return nil
}

func (p *postSetupProviderMock) ComputeProviders() []PostSetupComputeProvider {
	return nil
}

func (p *postSetupProviderMock) Benchmark(PostSetupComputeProvider) (int, error) {
	return 0, nil
}

func (p *postSetupProviderMock) StartSession(context context.Context, opts PostSetupOpts, commitmentAtx types.ATXID) error {
	return nil
}

func (p *postSetupProviderMock) Reset() error {
	return nil
}

func (p *postSetupProviderMock) GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error) {
	p.called++
	if p.setError {
		return nil, nil, fmt.Errorf("error")
	}
	return &types.Post{}, &types.PostMetadata{
		Challenge: challenge,
	}, nil
}

func (p *postSetupProviderMock) LastError() error {
	return nil
}

func (p *postSetupProviderMock) LastOpts() *PostSetupOpts {
	return &postSetupOpts
}

func (p *postSetupProviderMock) Config() PostConfig {
	return postCfg
}

func defaultPoetServiceMock(tb testing.TB) (*MockPoetProvingServiceClient, *gomock.Controller) {
	poetClient, controller := newPoetServiceMock(tb)
	poetClient.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, challenge, _ []byte) (*types.PoetRound, error) {
			return &types.PoetRound{
				ChallengeHash: getSerializedChallengeHash(challenge).Bytes(),
			}, nil
		})
	poetClient.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return([]byte{}, nil)
	return poetClient, controller
}

func newPoetServiceMock(tb testing.TB) (*MockPoetProvingServiceClient, *gomock.Controller) {
	tb.Helper()
	controller := gomock.NewController(tb)
	poetClient := NewMockPoetProvingServiceClient(controller)
	return poetClient, controller
}

func defaultPoetDbMockForChallenge(t *testing.T, challenge types.PoetChallenge) *MockpoetDbAPI {
	hash, err := challenge.Hash()
	require.NoError(t, err)
	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().GetProofRef(gomock.Any(), gomock.Any()).AnyTimes().Return([]byte("ref"), nil)
	poetDb.EXPECT().GetProof(gomock.Any()).AnyTimes().Return(&types.PoetProof{Members: [][]byte{hash.Bytes()}}, nil)
	poetDb.EXPECT().GetMembershipMap(gomock.Any()).AnyTimes().Return(map[types.Hash32]bool{*hash: true}, nil)
	return poetDb
}

func TestNIPostBuilderWithMocks(t *testing.T) {
	assert := require.New(t)

	postProvider := &postSetupProviderMock{}
	poetProvider, _ := defaultPoetServiceMock(t)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	poetDb := defaultPoetDbMockForChallenge(t, challenge)

	nb := NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProvider},
		poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())
	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
	assert.NoError(err)
	assert.NotNil(nipost)
}

func TestPostSetup(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	postSetupProvider, err := NewPostSetupManager(minerID, postCfg, logtest.New(t), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postSetupProvider)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	poetProvider, _ := defaultPoetServiceMock(t)
	poetDb := defaultPoetDbMockForChallenge(t, challenge)

	nb := NewNIPostBuilder(minerID, postSetupProvider, []PoetProvingServiceClient{poetProvider},
		poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())

	r.NoError(postSetupProvider.StartSession(context.Background(), postSetupOpts, goldenATXID))
	t.Cleanup(func() { assert.NoError(t, postSetupProvider.Reset()) })

	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
	r.NoError(err)
	r.NotNil(nipost)
}

func TestNIPostBuilderWithClients(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	poetDb := defaultPoetDbMockForChallenge(t, challenge)

	challengeHash, err := challenge.Hash()
	r.NoError(err)
	nipost := buildNIPost(t, r, postCfg, challenge, poetDb)
	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, postCfg, postSetupOpts.NumUnits)
	r.NoError(err)
}

func buildNIPost(tb testing.TB, r *require.Assertions, postCfg PostConfig, nipostChallenge types.PoetChallenge, poetDb poetDbAPI) *types.NIPost {
	gtw := util.NewMockGrpcServer(tb)
	pb.RegisterGatewayServiceServer(gtw.Server, &gatewayService{})
	var eg errgroup.Group
	eg.Go(gtw.Serve)
	tb.Cleanup(func() { assert.NoError(tb, eg.Wait()) })
	tb.Cleanup(gtw.Stop)

	poetProver, err := NewHTTPPoetHarness(true, WithGateway(gtw.Target()))
	r.NoError(err)
	r.NotNil(poetProver)
	tb.Cleanup(func() { assert.NoError(tb, poetProver.Teardown(true), "failed to tear down harness") })

	cdb := newCachedDB(tb)
	postProvider, err := NewPostSetupManager(minerID, postCfg, logtest.New(tb), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postProvider)
	tb.Cleanup(func() { assert.NoError(tb, postProvider.Reset()) })

	r.NoError(postProvider.StartSession(context.Background(), postSetupOpts, goldenATXID))

	nb := NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(tb), signing.NewEdSigner())

	nipost, _, err := nb.BuildNIPost(context.Background(), &nipostChallenge, goldenATXID, time.Time{})
	r.NoError(err)
	return nipost
}

type gatewayService struct {
	pb.UnimplementedGatewayServiceServer
}

func getSerializedChallengeHash(challenge []byte) *types.Hash32 {
	challengeDecoded := types.PoetChallenge{}
	if err := codec.Decode(challenge, &challengeDecoded); err != nil {
		return nil
	}
	hash, _ := challengeDecoded.Hash()
	return hash
}

func (*gatewayService) VerifyChallenge(ctx context.Context, req *pb.VerifyChallengeRequest) (*pb.VerifyChallengeResponse, error) {
	return &pb.VerifyChallengeResponse{
		Hash: getSerializedChallengeHash(req.Challenge).Bytes(),
	}, nil
}

func TestNewNIPostBuilderNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	minerIDNotInitialized := types.BytesToNodeID([]byte("not initialized"))
	nipostChallenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	challengeHash, err := nipostChallenge.Hash()
	r.NoError(err)

	cdb := newCachedDB(t)
	postProvider, err := NewPostSetupManager(minerIDNotInitialized, postCfg, logtest.New(t), cdb, goldenATXID)
	r.NoError(err)
	r.NotNil(postProvider)
	t.Cleanup(func() { assert.NoError(t, postProvider.Reset()) })

	gtw := util.NewMockGrpcServer(t)
	pb.RegisterGatewayServiceServer(gtw.Server, &gatewayService{})
	var eg errgroup.Group
	eg.Go(gtw.Serve)
	t.Cleanup(func() { assert.NoError(t, eg.Wait()) })
	t.Cleanup(gtw.Stop)

	poetProver, err := NewHTTPPoetHarness(true, WithGateway(gtw.Target()))
	r.NoError(err)
	r.NotNil(poetProver)
	t.Cleanup(func() {
		assert.NoError(t, poetProver.Teardown(true), "failed to tear down harness")
	})

	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().GetMembershipMap(gomock.Any()).AnyTimes().Return(map[types.Hash32]bool{*challengeHash: true}, nil)
	poetDb.EXPECT().GetProofRef(gomock.Any(), gomock.Any()).AnyTimes().Return([]byte("ref"), nil)
	poetDb.EXPECT().GetProof(gomock.Any()).AnyTimes().Return(&types.PoetProof{Members: [][]byte{challengeHash.Bytes()}}, nil)

	nb := NewNIPostBuilder(minerIDNotInitialized, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())

	nipost, _, err := nb.BuildNIPost(context.Background(), &nipostChallenge, goldenATXID, time.Time{})
	r.EqualError(err, "post setup not complete")
	r.Nil(nipost)

	r.NoError(postProvider.StartSession(context.Background(), postSetupOpts, goldenATXID))

	nipost, _, err = nb.BuildNIPost(context.Background(), &nipostChallenge, goldenATXID, time.Time{})
	r.NoError(err)
	r.NotNil(nipost)

	r.NoError(validateNIPost(minerIDNotInitialized, goldenATXID, nipost, *challengeHash, poetDb, postCfg, postSetupOpts.NumUnits))
}

func TestNIPostBuilder_BuildNIPost(t *testing.T) {
	assert := require.New(t)

	postProvider := &postSetupProviderMock{}

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	challengeHash, err := challenge.Hash()
	assert.NoError(err)

	poetProver, _ := newPoetServiceMock(t)
	poetProver.EXPECT().PoetServiceID(gomock.Any()).Times(3).Return([]byte{}, nil)
	poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).Times(3).DoAndReturn(
		func(_ context.Context, challenge, _ []byte) (*types.PoetRound, error) {
			return &types.PoetRound{
				ChallengeHash: getSerializedChallengeHash(challenge).Bytes(),
			}, nil
		})

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().GetMembershipMap(gomock.Any()).AnyTimes().Return(map[types.Hash32]bool{*challengeHash: true}, nil)
	poetDb.EXPECT().GetProofRef(gomock.Any(), gomock.Any()).AnyTimes().Return([]byte("ref"), nil)
	poetDb.EXPECT().GetProof(gomock.Any()).AnyTimes().Return(&types.PoetProof{Members: [][]byte{challengeHash.Bytes()}}, nil)

	nb := NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())

	nipost, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
	assert.NoError(err)
	assert.NotNil(nipost)
	db := sql.InMemory()
	assert.Equal(types.NIPostBuilderState{NIPost: &types.NIPost{}}, *nb.state)

	// fail after getting proof ref
	poetDb = NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().GetProofRef(gomock.Any(), gomock.Any()).AnyTimes().Return([]byte("ref"), nil)
	poetDb.EXPECT().GetMembershipMap(gomock.Any()).AnyTimes().Return(map[types.Hash32]bool{}, nil)

	nb = NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProver}, poetDb, db, logtest.New(t), signing.NewEdSigner())
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
	assert.Nil(nipost)
	assert.Error(err)

	// check that proof ref is not called again
	nb = NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProver}, poetDb, db, logtest.New(t), signing.NewEdSigner())
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
	assert.Nil(nipost)
	assert.Error(err)

	poetDb = NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().GetMembershipMap(gomock.Any()).Return(map[types.Hash32]bool{*challengeHash: true}, nil)
	poetDb.EXPECT().GetProofRef(gomock.Any(), gomock.Any()).AnyTimes().Return([]byte("ref"), nil)
	poetDb.EXPECT().GetProof(gomock.Any()).AnyTimes().Return(&types.PoetProof{Members: [][]byte{challengeHash.Bytes()}}, nil)

	// fail post exec
	nb = NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProver}, poetDb, db, logtest.New(t), signing.NewEdSigner())
	postProvider.setError = true
	// check that proof ref is not called again
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
	assert.Nil(nipost)
	assert.Error(err)

	// fail post exec
	challenge2 := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{Sequence: 1}}
	challenge2Hash, err := challenge2.Hash()
	assert.NoError(err)

	poetDb = NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().GetMembershipMap(gomock.Any()).AnyTimes().Return(map[types.Hash32]bool{*challengeHash: true, *challenge2Hash: true}, nil)
	poetDb.EXPECT().GetProofRef(gomock.Any(), gomock.Any()).AnyTimes().Return([]byte("ref"), nil)
	poetDb.EXPECT().GetProof(gomock.Any()).AnyTimes().Return(&types.PoetProof{Members: [][]byte{challengeHash.Bytes(), challenge2Hash.Bytes()}}, nil)
	nb = NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProver}, poetDb, db, logtest.New(t), signing.NewEdSigner())
	// poetDb.errOn = false
	postProvider.setError = false
	// check that proof ref is not called again
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
	assert.NotNil(nipost)
	assert.NoError(err)

	assert.Equal(3, postProvider.called)
	// test state not loading if other challenge provided
	nipost, _, err = nb.BuildNIPost(context.Background(), &challenge2, goldenATXID, time.Time{})
	assert.NoError(err)
	assert.NotNil(nipost)
	assert.Equal(4, postProvider.called)
}

func createMockPoetService(t *testing.T, id []byte) PoetProvingServiceClient {
	t.Helper()
	poet := NewMockPoetProvingServiceClient(gomock.NewController(t))
	poet.EXPECT().PoetServiceID(gomock.Any()).Times(1).Return(id, nil)
	poet.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, challenge, _ []byte) (*types.PoetRound, error) {
			return &types.PoetRound{
				ChallengeHash: getSerializedChallengeHash(challenge).Bytes(),
			}, nil
		})
	return poet
}

func TestNIPostBuilder_ManyPoETs_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	require := require.New(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))
	poets := make([]PoetProvingServiceClient, 0, 2)
	poets = append(poets, createMockPoetService(t, []byte("poet0")))
	poets = append(poets, createMockPoetService(t, []byte("poet1")))
	nb := NewNIPostBuilder(minerID, &postSetupProviderMock{}, poets, poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())

	resultChan := make(chan *types.NIPost)
	deadline := time.Now().Add(time.Millisecond * 10)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	challengeHash, err := challenge.Hash()
	require.NoError(err)

	go func() error {
		nipost, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, deadline)
		assert.NoError(t, err)
		resultChan <- nipost
		return nil
	}()

	// Act
	// Store proofMsg only from poet1
	proofMsg := types.PoetProofMessage{
		PoetServiceID: []byte("poet1"),
		PoetProof: types.PoetProof{
			Members:   [][]byte{challengeHash[:]},
			LeafCount: 1,
		},
	}
	ref, err := proofMsg.Ref()
	require.NoError(err)
	poetDb.StoreProof(context.Background(), ref, &proofMsg)

	time.Sleep(time.Until(deadline))

	// Verify
	result := <-resultChan
	require.Equal(challengeHash, result.Challenge)
	proof, err := poetDb.GetProof(result.PostMetadata.Challenge)
	require.NoError(err)
	require.EqualValues(proof.LeafCount, 1)
}

func TestNIPostBuilder_ManyPoETs_AllFinished(t *testing.T) {
	t.Parallel()
	// Arrange
	assert := require.New(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))
	poets := make([]PoetProvingServiceClient, 0, 2)
	poets = append(poets, createMockPoetService(t, []byte("poet0")))
	poets = append(poets, createMockPoetService(t, []byte("poet1")))
	nb := NewNIPostBuilder(minerID, &postSetupProviderMock{}, poets, poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	challengeHash, err := challenge.Hash()
	require.NoError(t, err)

	resultChan := make(chan *types.NIPost)
	deadline := time.Now().Add(time.Millisecond * 10)

	go func() error {
		nipost, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, deadline)
		assert.NoError(err)
		resultChan <- nipost
		return nil
	}()

	// Act
	// Store proofs from both poets
	proofMsg := types.PoetProofMessage{
		PoetServiceID: []byte("poet0"),
		PoetProof: types.PoetProof{
			Members:   [][]byte{challengeHash[:]},
			LeafCount: 4,
		},
	}
	ref, err := proofMsg.Ref()
	assert.NoError(err)
	poetDb.StoreProof(context.Background(), ref, &proofMsg)

	proofMsg = types.PoetProofMessage{
		PoetServiceID: []byte("poet1"),
		PoetProof: types.PoetProof{
			Members:   [][]byte{challengeHash[:]},
			LeafCount: 1,
		},
	}
	ref, err = proofMsg.Ref()
	assert.NoError(err)
	poetDb.StoreProof(context.Background(), ref, &proofMsg)

	// Verify
	result := <-resultChan
	assert.Equal(challengeHash, result.Challenge)
	proof, err := poetDb.GetProof(result.PostMetadata.Challenge)
	assert.NoError(err)
	assert.EqualValues(proof.LeafCount, 4)
}

func TestValidator_Validate(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	challengeHash, err := challenge.Hash()
	r.NoError(err)
	poetDb := defaultPoetDbMockForChallenge(t, challenge)

	nipost := buildNIPost(t, r, postCfg, challenge, poetDb)

	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, postCfg, postSetupOpts.NumUnits)
	r.NoError(err)

	err = validateNIPost(minerID, goldenATXID, nipost, types.BytesToHash([]byte("lerner")), poetDb, postCfg, postSetupOpts.NumUnits)
	r.Contains(err.Error(), "invalid `Challenge`")

	newNIPost := *nipost
	newNIPost.Post = &types.Post{}
	err = validateNIPost(minerID, goldenATXID, &newNIPost, *challengeHash, poetDb, postCfg, postSetupOpts.NumUnits)
	r.Contains(err.Error(), "invalid Post")

	newPostCfg := postCfg
	newPostCfg.MinNumUnits = postSetupOpts.NumUnits + 1
	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `numUnits`; expected: >=%d, given: %d", newPostCfg.MinNumUnits, postSetupOpts.NumUnits))

	newPostCfg = postCfg
	newPostCfg.MaxNumUnits = postSetupOpts.NumUnits - 1
	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `numUnits`; expected: <=%d, given: %d", newPostCfg.MaxNumUnits, postSetupOpts.NumUnits))

	newPostCfg = postCfg
	newPostCfg.LabelsPerUnit = nipost.PostMetadata.LabelsPerUnit + 1
	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", newPostCfg.LabelsPerUnit, nipost.PostMetadata.LabelsPerUnit))

	newPostCfg = postCfg
	newPostCfg.BitsPerLabel = nipost.PostMetadata.BitsPerLabel + 1
	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `BitsPerLabel`; expected: >=%d, given: %d", newPostCfg.BitsPerLabel, nipost.PostMetadata.BitsPerLabel))

	newPostCfg = postCfg
	newPostCfg.K1 = nipost.PostMetadata.K2 - 1
	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `K1`; expected: <=%d, given: %d", newPostCfg.K1, nipost.PostMetadata.K1))

	newPostCfg = postCfg
	newPostCfg.K2 = nipost.PostMetadata.K2 + 1
	err = validateNIPost(minerID, goldenATXID, nipost, *challengeHash, poetDb, newPostCfg, postSetupOpts.NumUnits)
	r.EqualError(err, fmt.Sprintf("invalid `K2`; expected: >=%d, given: %d", newPostCfg.K2, nipost.PostMetadata.K2))
}

func validateNIPost(minerID types.NodeID, commitmentAtx types.ATXID, nipost *types.NIPost, challenge types.Hash32, poetDb poetDbAPI, postCfg PostConfig, numUnits uint32) error {
	v := &Validator{poetDb, postCfg}
	_, err := v.Validate(minerID, commitmentAtx, nipost, challenge, numUnits)
	return err
}

func TestNIPostBuilder_Close(t *testing.T) {
	r := require.New(t)

	postProvider := &postSetupProviderMock{}
	poetProver, _ := defaultPoetServiceMock(t)
	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	poetDb := defaultPoetDbMockForChallenge(t, challenge)

	nb := NewNIPostBuilder(minerID, postProvider, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())

	ctx, close := context.WithCancel(context.Background())
	close()
	nipost, _, err := nb.BuildNIPost(ctx, &challenge, goldenATXID, time.Time{})
	r.ErrorIs(err, context.Canceled)
	r.Nil(nipost)
}

func TestNIPSTBuilder_PoetUnstable(t *testing.T) {
	t.Parallel()
	postProver := &postSetupProviderMock{}
	poetProver, controller := newPoetServiceMock(t)
	defer controller.Finish()

	challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{}}
	poetDb := defaultPoetDbMockForChallenge(t, challenge)

	nb := NewNIPostBuilder(minerID, postProver, []PoetProvingServiceClient{poetProver},
		poetDb, sql.InMemory(), logtest.New(t), signing.NewEdSigner())

	t.Run("PoetServiceID", func(t *testing.T) {
		poetProver.EXPECT().PoetServiceID(gomock.Any()).Return(nil, errors.New("test"))
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit", func(t *testing.T) {
		poetProver.EXPECT().PoetServiceID(gomock.Any()).Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("test"))
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit returns invalid hash", func(t *testing.T) {
		poetProver.EXPECT().PoetServiceID(gomock.Any()).Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).Return(&types.PoetRound{}, nil)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Challenge is not included in proof members", func(t *testing.T) {
		// The proof in poetDB will not include this challenge
		challenge := types.PoetChallenge{NIPostChallenge: &types.NIPostChallenge{Sequence: 1}}
		hash, err := challenge.Hash()
		require.NoError(t, err)
		poetProver.EXPECT().PoetServiceID(gomock.Any()).Return([]byte{}, nil)
		poetProver.EXPECT().Submit(gomock.Any(), gomock.Any(), gomock.Any()).Return(&types.PoetRound{ChallengeHash: hash.Bytes()}, nil)
		nipst, _, err := nb.BuildNIPost(context.Background(), &challenge, goldenATXID, time.Time{})
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
