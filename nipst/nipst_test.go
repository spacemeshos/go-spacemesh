package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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

type postProverClientMock struct{}

// A compile time check to ensure that postProverClientMock fully implements PostProverClient.
var _ PostProverClient = (*postProverClientMock)(nil)

func (*postProverClientMock) Initialize() (*types.PostProof, error) { return &types.PostProof{}, nil }

func (*postProverClientMock) Execute(challenge []byte) (*types.PostProof, error) {
	return &types.PostProof{}, nil
}

func (*postProverClientMock) Reset() error { return nil }

func (*postProverClientMock) IsInitialized() (bool, error) { return true, nil }

func (*postProverClientMock) VerifyInitAllowed() error { return nil }

func (*postProverClientMock) SetLogger(shared.Logger) {}

func (*postProverClientMock) SetParams(datadir string, space uint64) error { return nil }

func (*postProverClientMock) Cfg() *config.Config { return &config.Config{} }

type poetProvingServiceClientMock struct{}

// A compile time check to ensure that poetProvingServiceClientMock fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*poetProvingServiceClientMock)(nil)

func (p *poetProvingServiceClientMock) submit(challenge types.Hash32) (*types.PoetRound, error) {
	return &types.PoetRound{}, nil
}

func (p *poetProvingServiceClientMock) getPoetServiceId() ([types.PoetServiceIdLength]byte, error) {
	return [32]byte{}, nil
}

type poetDbMock struct{}

// A compile time check to ensure that poetDbMock fully implements PoetDb.
var _ PoetDb = (*poetDbMock)(nil)

func (*poetDbMock) SubscribeToProofRef(poetId [types.PoetServiceIdLength]byte, roundId uint64) chan []byte {
	ch := make(chan []byte)
	go func() {
		ch <- []byte("hello there")
	}()
	return ch
}

func (*poetDbMock) GetMembershipMap(poetRoot []byte) (map[types.Hash32]bool, error) {
	hash := types.BytesToHash([]byte("anton"))
	return map[types.Hash32]bool{hash: true}, nil
}

func TestNIPSTBuilderWithMocks(t *testing.T) {
	assert := require.New(t)

	postProver := &postProverClientMock{}
	poetProver := &poetProvingServiceClientMock{}

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, log.NewDefault(string(minerID)))
	hash := types.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash)
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
		poetDb, log.NewDefault(string(minerID)))
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
	npst, err := nb.BuildNIPST(&hash)
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

	err := validateNIPST(npst, postCfg, nipstChallenge, poetDb)
	r.NoError(err)
}

func buildNIPST(r *require.Assertions, postCfg config.Config, nipstChallenge types.Hash32, poetDb PoetDb) *types.NIPST {
	poetProver, err := newRPCPoetHarnessClient()
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)

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
		poetDb, log.NewDefault(string(minerID)))

	npst, err := nb.BuildNIPST(&nipstChallenge)
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
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)
	poetDb := &poetDbMock{}
	nb := newNIPSTBuilder(minerIDNotInitialized, postProver, poetProver,
		poetDb, log.NewDefault(string(minerID)))

	npst, err := nb.BuildNIPST(&nipstChallenge)
	r.EqualError(err, "PoST not initialized")
	r.Nil(npst)

	commitment, err := postProver.Initialize()
	defer func() {
		err := postProver.Reset()
		r.NoError(err)
	}()
	r.NoError(err)
	r.NotNil(commitment)

	npst, err = nb.BuildNIPST(&nipstChallenge)
	r.NoError(err)
	r.NotNil(npst)

	err = validateNIPST(npst, postCfg, nipstChallenge, poetDb)
	r.NoError(err)
}

func TestValidator_Validate(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	poetDb := &poetDbMock{}
	nipstChallenge := types.BytesToHash([]byte("anton"))

	npst := buildNIPST(r, postCfg, nipstChallenge, poetDb)

	err := validateNIPST(npst, postCfg, nipstChallenge, poetDb)
	r.NoError(err)

	newPostCfg := postCfg
	newPostCfg.SpacePerUnit += 1
	err = validateNIPST(npst, newPostCfg, nipstChallenge, poetDb)
	r.EqualError(err, "PoST space (1024) is less than a single space unit (1025)")

	newPostCfg = postCfg
	newPostCfg.Difficulty += 1
	err = validateNIPST(npst, newPostCfg, nipstChallenge, poetDb)
	r.EqualError(err, "PoST proof invalid: validation failed: number of derived leaf indices (8) doesn't match number of included proven leaves (9)")

	newPostCfg = postCfg
	newPostCfg.NumProvenLabels += 5
	err = validateNIPST(npst, newPostCfg, nipstChallenge, poetDb)
	r.EqualError(err, "PoST proof invalid: validation failed: number of derived leaf indices (12) doesn't match number of included proven leaves (9)")

	err = validateNIPST(npst, postCfg, types.BytesToHash([]byte("lerner")), poetDb)
	r.EqualError(err, "NIPST challenge is not equal to expected challenge")
}

func validateNIPST(npst *types.NIPST, postCfg config.Config, nipstChallenge types.Hash32, poetDb PoetDb) error {
	v := &Validator{&postCfg, poetDb}
	return v.Validate(npst, nipstChallenge)
}
