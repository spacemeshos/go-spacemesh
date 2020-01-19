package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"testing"
)

var minerID = []byte("id")
var postCfg config.Config

func init() {
	postCfg = *config.DefaultConfig()
	postCfg.Difficulty = 5
	postCfg.NumProvenLabels = 10
	postCfg.SpacePerUnit = 1 << 10 // 1KB.
	postCfg.NumFiles = 1
}

type postProverClientMock struct {
	called   int
	setError bool
}

// A compile time check to ensure that postProverClientMock fully implements PostProverClient.
var _ PostProverClient = (*postProverClientMock)(nil)

func (p *postProverClientMock) Initialize() (*types.PostProof, error) {
	//p.called++
	return &types.PostProof{}, nil
}

func (p *postProverClientMock) Execute(challenge []byte) (*types.PostProof, error) {
	p.called++
	if p.setError {
		return nil, fmt.Errorf("error")
	}
	return &types.PostProof{}, nil
}

func (p *postProverClientMock) Reset() error {
	p.called++
	return nil
}

func (p *postProverClientMock) IsInitialized() (bool, error) {
	//p.called++
	return true, nil
}

func (p *postProverClientMock) VerifyInitAllowed() error {
	p.called++
	return nil
}

func (p *postProverClientMock) SetLogger(shared.Logger) {
	p.called++
}

func (p *postProverClientMock) SetParams(datadir string, space uint64) error {
	p.called++
	return nil
}

func (p *postProverClientMock) Cfg() *config.Config {
	//p.called++
	return &config.Config{}
}

type poetProvingServiceClientMock struct {
	called int
}

// A compile time check to ensure that poetProvingServiceClientMock fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*poetProvingServiceClientMock)(nil)

func (p *poetProvingServiceClientMock) submit(challenge types.Hash32) (*types.PoetRound, error) {
	p.called++
	return &types.PoetRound{}, nil
}

func (p *poetProvingServiceClientMock) getPoetServiceId() ([]byte, error) {
	p.called++
	return []byte{}, nil
}

type poetDbMock struct {
	errOn        bool
	unsubscribed bool
}

// A compile time check to ensure that poetDbMock fully implements PoetDbApi.
var _ PoetDbApi = (*poetDbMock)(nil)

func (*poetDbMock) SubscribeToProofRef(poetId []byte, roundId string) chan []byte {
	ch := make(chan []byte)
	go func() {
		ch <- []byte("hello there")
	}()
	return ch
}

func (p *poetDbMock) UnsubscribeFromProofRef(poetId []byte, roundId string) { p.unsubscribed = true }

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

	postProver := &postProverClientMock{}
	poetProver := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash, nil, nil)
	assert.NoError(err)
	assert.NotNil(npst)
}

func TestInitializePost(t *testing.T) {
	assert := require.New(t)

	postProver, err := NewPostClient(&postCfg, minerID)
	assert.NoError(err)
	assert.NotNil(postProver)

	poetProver := &poetProvingServiceClientMock{}
	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	datadir := "/tmp/anton"
	space := uint64(2048)

	err = postProver.SetParams(datadir, space)
	assert.NoError(err)
	_, err = postProver.Initialize()
	assert.NoError(err)
	defer func() {
		assert.NoError(postProver.Reset())
	}()

	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash, nil, nil)
	assert.NoError(err)
	assert.NotNil(npst)
}

func TestNIPSTBuilderWithClients(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	poetDb := &poetDbMock{}

	nipstChallenge := types.BytesToHash([]byte("anton"))

	npst := buildNIPST(r, postCfg, nipstChallenge, poetDb)

	err := validateNIPST(npst, postCfg, nipstChallenge, poetDb, minerID)
	r.NoError(err)
}

func buildNIPST(r *require.Assertions, postCfg config.Config, nipstChallenge types.Hash32, poetDb PoetDbApi) *types.NIPST {
	poetProver, err := newRPCPoetHarnessClient()
	r.NoError(err)
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.Teardown(true)
		r.NoError(err)
	}()

	postProver, err := NewPostClient(&postCfg, minerID)
	r.NoError(err)
	r.NotNil(postProver)
	defer func() {
		err := postProver.Reset()
		r.NoError(err)
	}()

	commitment, err := postProver.Initialize()
	r.NoError(err)
	r.NotNil(commitment)

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))

	npst, err := nb.BuildNIPST(&nipstChallenge, nil, nil)
	r.NoError(err)
	return npst
}

func TestNewNIPSTBuilderNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	minerIDNotInitialized := []byte("not initialized")
	nipstChallenge := types.BytesToHash([]byte("anton"))

	postProver, err := NewPostClient(&postCfg, minerIDNotInitialized)
	r.NoError(err)
	r.NotNil(postProver)

	poetProver, err := newRPCPoetHarnessClient()
	r.NoError(err)
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.Teardown(true)
		r.NoError(err)
	}()
	poetDb := &poetDbMock{}
	nb := newNIPSTBuilder(minerIDNotInitialized, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))

	npst, err := nb.BuildNIPST(&nipstChallenge, nil, nil)
	r.EqualError(err, "PoST not initialized")
	r.Nil(npst)

	commitment, err := postProver.Initialize()
	defer func() {
		err := postProver.Reset()
		r.NoError(err)
	}()
	r.NoError(err)
	r.NotNil(commitment)

	npst, err = nb.BuildNIPST(&nipstChallenge, nil, nil)
	r.NoError(err)
	r.NotNil(npst)

	err = validateNIPST(npst, postCfg, nipstChallenge, poetDb, minerIDNotInitialized)
	r.NoError(err)
}

func TestNIPSTBuilder_BuildNIPST(t *testing.T) {
	assert := require.New(t)

	postProver := &postProverClientMock{}
	poetProver := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{errOn: false}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash, nil, nil)
	assert.NoError(err)
	assert.NotNil(npst)
	db := database.NewMemDatabase()
	assert.Equal(builderState{Nipst: &types.NIPST{}}, *nb.state)

	//fail after getting proof ref
	nb = newNIPSTBuilder(minerID, postProver, poetProver, poetDb, db, log.NewDefault(string(minerID)))
	poetDb.errOn = true
	npst, err = nb.BuildNIPST(&hash, nil, nil)
	assert.Nil(npst)
	assert.Error(err)

	//check that proof ref is not called again
	nb = newNIPSTBuilder(minerID, postProver, poetProver, poetDb, db, log.NewDefault(string(minerID)))
	npst, err = nb.BuildNIPST(&hash, nil, nil)
	assert.Equal(4, poetProver.called)
	assert.Nil(npst)
	assert.Error(err)

	//fail post exec
	nb = newNIPSTBuilder(minerID, postProver, poetProver, poetDb, db, log.NewDefault(string(minerID)))
	poetDb.errOn = false
	postProver.setError = true
	//check that proof ref is not called again
	npst, err = nb.BuildNIPST(&hash, nil, nil)
	assert.Equal(4, poetProver.called)
	assert.Nil(npst)
	assert.Error(err)

	//fail post exec
	nb = newNIPSTBuilder(minerID, postProver, poetProver, poetDb, db, log.NewDefault(string(minerID)))
	poetDb.errOn = false
	postProver.setError = false
	//check that proof ref is not called again
	npst, err = nb.BuildNIPST(&hash, nil, nil)
	assert.Equal(4, poetProver.called)
	assert.NotNil(npst)
	assert.NoError(err)

	assert.Equal(3, postProver.called)
	//test state not loading if other challenge provided
	hash2 := types.BytesToHash([]byte("anton1"))
	npst, err = nb.BuildNIPST(&hash2, nil, nil)
	assert.Equal(6, poetProver.called)
	assert.Equal(4, postProver.called)

	assert.NotNil(npst)
	assert.NoError(err)

}

func TestValidator_Validate(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	poetDb := &poetDbMock{}
	nipstChallenge := types.BytesToHash([]byte("anton"))

	npst := buildNIPST(r, postCfg, nipstChallenge, poetDb)

	err := validateNIPST(npst, postCfg, nipstChallenge, poetDb, minerID)
	r.NoError(err)

	newPostCfg := postCfg
	newPostCfg.SpacePerUnit += 1
	err = validateNIPST(npst, newPostCfg, nipstChallenge, poetDb, minerID)
	r.EqualError(err, "PoST space (1024) is less than a single space unit (1025)")

	newPostCfg = postCfg
	newPostCfg.Difficulty += 1
	err = validateNIPST(npst, newPostCfg, nipstChallenge, poetDb, minerID)
	r.EqualError(err, "PoST proof invalid: validation failed: number of derived leaf indices (8) doesn't match number of included proven leaves (9)")

	newPostCfg = postCfg
	newPostCfg.NumProvenLabels += 5
	err = validateNIPST(npst, newPostCfg, nipstChallenge, poetDb, minerID)
	r.EqualError(err, "PoST proof invalid: validation failed: number of derived leaf indices (12) doesn't match number of included proven leaves (9)")

	err = validateNIPST(npst, postCfg, types.BytesToHash([]byte("lerner")), poetDb, minerID)
	r.EqualError(err, "NIPST challenge is not equal to expected challenge")
}

func validateNIPST(npst *types.NIPST, postCfg config.Config, nipstChallenge types.Hash32, poetDb PoetDbApi, minerId []byte) error {
	v := &Validator{&postCfg, poetDb}
	return v.Validate(*signing.NewPublicKey(minerId), npst, nipstChallenge)
}

func TestNIPSTBuilder_TimeoutUnsubscribe(t *testing.T) {
	r := require.New(t)

	postProver := &postProverClientMock{}
	poetProver := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	poetDb.unsubscribed = false
	npst, err := nb.BuildNIPST(&hash, closedChan, nil) // closedChan will timeout immediately
	r.EqualError(err, "atx expired while waiting for poet proof, target epoch ended")
	r.Nil(npst)
	r.True(poetDb.unsubscribed)
}

func TestNIPSTBuilder_Close(t *testing.T) {
	r := require.New(t)

	postProver := &postProverClientMock{}
	poetProver := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, database.NewMemDatabase(), log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash, nil, closedChan) // closedChan will timeout immediately
	r.IsType(&StopRequestedError{}, err)
	r.Nil(npst)
}
