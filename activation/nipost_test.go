package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"testing"
)

var minerID = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
var postCfg config.Config

func init() {
	postCfg = *config.DefaultConfig()
	postCfg.DataDir, _ = ioutil.TempDir("", "post-test")
	postCfg.NumLabels = 1 << 10
	postCfg.LabelSize = 8
	postCfg.NumFiles = 1
}

type postProviderMock struct {
	called   int
	setError bool
}

// A compile time check to ensure that postProviderMock fully implements the PostProvider interface.
var _ PostProvider = (*postProviderMock)(nil)

func (p *postProviderMock) PostStatus() (*PostStatus, error) {
	return nil, nil
}

func (p *postProviderMock) PostComputeProviders() []initialization.ComputeProvider {
	return nil
}

func (p *postProviderMock) CreatePostData(options *PostOptions) (chan struct{}, error) {
	return nil, nil
}

func (p *postProviderMock) StopPostDataCreationSession(deleteFiles bool) error {
	return nil
}

func (p *postProviderMock) PostDataCreationProgressStream() <-chan *PostStatus {
	return nil
}

func (p *postProviderMock) InitCompleted() (chan struct{}, bool) {
	return nil, true
}

func (p *postProviderMock) GenerateProof(challenge []byte) (*types.PoST, *types.PoSTMetadata, error) {
	p.called++
	if p.setError {
		return nil, nil, fmt.Errorf("error")
	}
	return &types.PoST{}, &types.PoSTMetadata{}, nil
}

func (p *postProviderMock) Cfg() config.Config {
	return config.Config{}
}

func (p *postProviderMock) SetLogger(logger log.Log) {
}

type poetProvingServiceClientMock struct {
	called int
}

// A compile time check to ensure that poetProvingServiceClientMock fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*poetProvingServiceClientMock)(nil)

func (p *poetProvingServiceClientMock) Submit(challenge types.Hash32) (*types.PoetRound, error) {
	p.called++
	return &types.PoetRound{}, nil
}

func (p *poetProvingServiceClientMock) PoetServiceID() ([]byte, error) {
	p.called++
	return []byte{}, nil
}

type poetDbMock struct {
	errOn        bool
	unsubscribed bool
}

// A compile time check to ensure that poetDbMock fully implements poetDbAPI.
var _ poetDbAPI = (*poetDbMock)(nil)

func (*poetDbMock) SubscribeToProofRef(poetID []byte, roundID string) chan []byte {
	ch := make(chan []byte)
	go func() {
		ch <- []byte("hello there")
	}()
	return ch
}

func (p *poetDbMock) UnsubscribeFromProofRef(poetID []byte, roundID string) { p.unsubscribed = true }

func (p *poetDbMock) GetMembershipMap(poetRoot []byte) (map[types.Hash32]bool, error) {
	if p.errOn {
		return map[types.Hash32]bool{}, nil
	}
	hash := types.BytesToHash([]byte("anton"))
	hash2 := types.BytesToHash([]byte("anton1"))
	return map[types.Hash32]bool{hash: true, hash2: true}, nil
}

func TestNIPSTBuilderWithMocks(t *testing.T) {
	assert := require.New(t)

	postProvider := &postProviderMock{}
	poetProvider := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{}

	nb := NewNIPoSTBuilder(minerID, postProvider, poetProvider,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	nipost, err := nb.BuildNIPoST(&hash, nil, nil)
	assert.NoError(err)
	assert.NotNil(nipost)
}

func TestInitializePost(t *testing.T) {
	assert := require.New(t)

	var postProvider PostProvider
	var err error
	postProvider, err = NewPostManager(minerID, *cfg, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	assert.NoError(err)
	assert.NotNil(postProvider)

	poetProvider := &poetProvingServiceClientMock{}
	poetDb := &poetDbMock{}

	nb := NewNIPoSTBuilder(minerID, postProvider, poetProvider,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	datadir, _ := ioutil.TempDir("", "post-test")

	options := &PostOptions{
		DataDir:           datadir,
		DataSize:          1 << 15,
		ComputeProviderID: int(initialization.CPUProviderID()),
	}
	done, err := postProvider.CreatePostData(options)
	assert.NoError(err)
	<-done
	defer func() {
		assert.NoError(postProvider.StopPostDataCreationSession(true))
	}()

	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPoST(&hash, nil, nil)
	assert.NoError(err)
	assert.NotNil(npst)
}

func TestNIPoSTBuilderWithClients(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	poetDb := &poetDbMock{}

	nipostChallenge := types.BytesToHash([]byte("anton"))
	nipost := buildNIPoST(r, postCfg, nipostChallenge, poetDb)

	err := validateNIPoST(minerID, nipost, nipostChallenge, poetDb, postCfg)
	r.NoError(err)
}

func buildNIPoST(r *require.Assertions, postCfg config.Config, nipostChallenge types.Hash32, poetDb poetDbAPI) *types.NIPoST {
	poetProver, err := NewHTTPPoetHarness(true)
	r.NoError(err)
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.Teardown(true)
		r.NoError(err)
	}()

	var postProvider PostProvider
	postProvider, err = NewPostManager(minerID, postCfg, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	r.NoError(err)
	r.NotNil(postProvider)
	defer func() {
		err := postProvider.StopPostDataCreationSession(true)
		r.NoError(err)
	}()

	options := &PostOptions{
		DataDir:           postCfg.DataDir,
		DataSize:          shared.DataSize(postCfg.NumLabels, postCfg.LabelSize),
		ComputeProviderID: int(initialization.CPUProviderID()),
	}
	done, err := postProvider.CreatePostData(options)
	r.NoError(err)
	<-done

	nb := NewNIPoSTBuilder(minerID, postProvider, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))

	nipost, err := nb.BuildNIPoST(&nipostChallenge, nil, nil)
	r.NoError(err)
	return nipost
}

func TestNewNIPoSTBuilderNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	minerIDNotInitialized := make([]byte, 32)
	copy(minerIDNotInitialized, []byte("not initialized"))
	nipostChallenge := types.BytesToHash([]byte("anton"))

	cfg := *cfg
	cfg.DataDir, _ = ioutil.TempDir("", "post-test")
	store := NewMockDB()

	postProvider, err := NewPostManager(minerIDNotInitialized, cfg, store, postLog)
	r.NoError(err)

	poetProver, err := NewHTTPPoetHarness(true)
	r.NoError(err)
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.Teardown(true)
		r.NoError(err)
	}()
	poetDb := &poetDbMock{}
	nb := NewNIPoSTBuilder(minerIDNotInitialized, postProvider, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))

	npst, err := nb.BuildNIPoST(&nipostChallenge, nil, nil)
	r.EqualError(err, "PoST init not completed")
	r.Nil(npst)

	options := &PostOptions{
		DataDir:           cfg.DataDir,
		DataSize:          1 << 15,
		ComputeProviderID: int(initialization.CPUProviderID()),
	}
	done, err := postProvider.CreatePostData(options)
	defer func() {
		err := postProvider.StopPostDataCreationSession(true)
		r.NoError(err)
	}()
	r.NoError(err)

	<-done
	npst, err = nb.BuildNIPoST(&nipostChallenge, nil, nil)
	r.NoError(err)
	r.NotNil(npst)

	err = validateNIPoST(minerIDNotInitialized, npst, nipostChallenge, poetDb, postCfg)
	r.NoError(err)
}

func TestNIPoSTBuilder_BuildNIPoST(t *testing.T) {
	assert := require.New(t)

	postProvider := &postProviderMock{}
	poetProvider := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{errOn: false}

	nb := NewNIPoSTBuilder(minerID, postProvider, poetProvider,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPoST(&hash, nil, nil)
	assert.NoError(err)
	assert.NotNil(npst)
	db := database.NewMemDatabase()
	assert.Equal(builderState{NIPoST: &types.NIPoST{}}, *nb.state)

	//fail after getting proof ref
	nb = NewNIPoSTBuilder(minerID, postProvider, poetProvider, poetDb, db, log.NewDefault(string(minerID)))
	poetDb.errOn = true
	npst, err = nb.BuildNIPoST(&hash, nil, nil)
	assert.Nil(npst)
	assert.Error(err)

	//check that proof ref is not called again
	nb = NewNIPoSTBuilder(minerID, postProvider, poetProvider, poetDb, db, log.NewDefault(string(minerID)))
	npst, err = nb.BuildNIPoST(&hash, nil, nil)
	assert.Equal(4, poetProvider.called)
	assert.Nil(npst)
	assert.Error(err)

	//fail post exec
	nb = NewNIPoSTBuilder(minerID, postProvider, poetProvider, poetDb, db, log.NewDefault(string(minerID)))
	poetDb.errOn = false
	postProvider.setError = true
	//check that proof ref is not called again
	npst, err = nb.BuildNIPoST(&hash, nil, nil)
	assert.Equal(4, poetProvider.called)
	assert.Nil(npst)
	assert.Error(err)

	//fail post exec
	nb = NewNIPoSTBuilder(minerID, postProvider, poetProvider, poetDb, db, log.NewDefault(string(minerID)))
	poetDb.errOn = false
	postProvider.setError = false
	//check that proof ref is not called again
	npst, err = nb.BuildNIPoST(&hash, nil, nil)
	assert.Equal(4, poetProvider.called)
	assert.NotNil(npst)
	assert.NoError(err)

	assert.Equal(3, postProvider.called)
	//test state not loading if other challenge provided
	hash2 := types.BytesToHash([]byte("anton1"))
	npst, err = nb.BuildNIPoST(&hash2, nil, nil)
	assert.Equal(6, poetProvider.called)
	assert.Equal(4, postProvider.called)

	assert.NotNil(npst)
	assert.NoError(err)

}

func TestValidator_Validate(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	poetDb := &poetDbMock{}
	nipostChallenge := types.BytesToHash([]byte("anton"))

	nipost := buildNIPoST(r, postCfg, nipostChallenge, poetDb)

	err := validateNIPoST(minerID, nipost, nipostChallenge, poetDb, postCfg)
	r.NoError(err)

	err = validateNIPoST(minerID, nipost, types.BytesToHash([]byte("lerner")), poetDb, postCfg)
	r.EqualError(err, "nipost challenge is not equal to expected challenge")

	newNIPoST := *nipost
	newNIPoST.PoST = &types.PoST{}
	err = validateNIPoST(minerID, &newNIPoST, nipostChallenge, poetDb, postCfg)
	r.Contains(err.Error(), "invalid PoST")

	newPostCfg := postCfg
	newPostCfg.NumLabels = nipost.PoSTMetadata.NumLabels + 1
	err = validateNIPoST(minerID, nipost, nipostChallenge, poetDb, newPostCfg)
	r.EqualError(err, fmt.Sprintf("invalid NumLabels; expected: >=%d, given: %d", newPostCfg.NumLabels, nipost.PoSTMetadata.NumLabels))

	newPostCfg = postCfg
	newPostCfg.LabelSize = nipost.PoSTMetadata.LabelSize + 1
	err = validateNIPoST(minerID, nipost, nipostChallenge, poetDb, newPostCfg)
	r.EqualError(err, fmt.Sprintf("invalid LabelSize; expected: >=%d, given: %d", newPostCfg.LabelSize, nipost.PoSTMetadata.LabelSize))

	newPostCfg = postCfg
	newPostCfg.K1 = nipost.PoSTMetadata.K2 - 1
	err = validateNIPoST(minerID, nipost, nipostChallenge, poetDb, newPostCfg)
	r.EqualError(err, fmt.Sprintf("invalid K1; expected: <=%d, given: %d", newPostCfg.K1, nipost.PoSTMetadata.K1))

	newPostCfg = postCfg
	newPostCfg.K2 = nipost.PoSTMetadata.K2 + 1
	err = validateNIPoST(minerID, nipost, nipostChallenge, poetDb, newPostCfg)
	r.EqualError(err, fmt.Sprintf("invalid K2; expected: >=%d, given: %d", newPostCfg.K2, nipost.PoSTMetadata.K2))
}

func validateNIPoST(minerID []byte, nipost *types.NIPoST, challenge types.Hash32, poetDb poetDbAPI, postCfg config.Config) error {
	v := &Validator{poetDb, postCfg}
	// MERGE-2 FIX -- space param
	return v.Validate(*signing.NewPublicKey(minerID), nipost, 100, challenge)
}

func TestNIPoSTBuilder_TimeoutUnsubscribe(t *testing.T) {
	r := require.New(t)

	postProvider := &postProviderMock{}
	poetProvider := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{}

	nb := NewNIPoSTBuilder(minerID, postProvider, poetProvider,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	poetDb.unsubscribed = false
	npst, err := nb.BuildNIPoST(&hash, closedChan, nil) // closedChan will timeout immediately
	r.EqualError(err, "atx expired while waiting for poet proof, target epoch ended")
	r.Nil(npst)
	r.True(poetDb.unsubscribed)
}

func TestNIPoSTBuilder_Close(t *testing.T) {
	r := require.New(t)

	postProvider := &postProviderMock{}
	poetProvider := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{}

	nb := NewNIPoSTBuilder(minerID, postProvider, poetProvider,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPoST(&hash, nil, closedChan) // closedChan will timeout immediately
	r.IsType(&StopRequestedError{}, err)
	r.Nil(npst)
}
