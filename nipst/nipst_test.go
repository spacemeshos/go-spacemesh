package nipst

import (
	"flag"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

var minerID = []byte("id")
var idsToCleanup [][]byte
var postCfg config.Config

func init() {
	postCfg = *config.DefaultConfig()
	postCfg.Difficulty = 5
	postCfg.NumProvenLabels = 10
	postCfg.SpacePerUnit = 1 << 10 // 1KB.
	postCfg.FileSize = 1 << 10     // 1KB.
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

func (*postProverClientMock) SetParams(datadir string, space uint64) {}

func (*postProverClientMock) Cfg() *config.Config { return &config.Config{} }

type poetProvingServiceClientMock struct{}

// A compile time check to ensure that poetProvingServiceClientMock fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*poetProvingServiceClientMock)(nil)

func (p *poetProvingServiceClientMock) submit(challenge common.Hash) (*types.PoetRound, error) {
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

func (*poetDbMock) GetMembershipMap(poetRoot []byte) (map[common.Hash]bool, error) {
	hash := common.BytesToHash([]byte("anton"))
	return map[common.Hash]bool{hash: true}, nil
}

func TestNIPSTBuilderWithMocks(t *testing.T) {
	assert := require.New(t)

	postProver := &postProverClientMock{}
	poetProver := &poetProvingServiceClientMock{}
	verifyPost := func(*types.PostProof, uint64, uint, uint) error { return nil }

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, verifyPost, log.NewDefault(string(minerID)))
	hash := common.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash)
	assert.NoError(err)
	assert.NotNil(npst)
}

func TestInitializePost(t *testing.T) {
	assert := require.New(t)

	postProver := NewPostClient(&postCfg, minerID)
	poetProver := &poetProvingServiceClientMock{}
	verifyPost := func(*types.PostProof, uint64, uint, uint) error { return nil }

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, verifyPost, log.NewDefault(string(minerID)))
	datadir := "/tmp/anton"
	space := uint64(2048)

	postProver.SetParams(datadir, space)
	_, err := postProver.Initialize()
	assert.NoError(err)
	defer func() {
		assert.NoError(postProver.Reset())
	}()

	hash := common.BytesToHash([]byte("anton"))
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

	nipstChallenge := common.BytesToHash([]byte("anton"))

	npst := buildNIPST(r, postCfg, nipstChallenge, poetDb)

	err := validateNIPST(npst, postCfg, nipstChallenge, poetDb)
	r.NoError(err)
}

func buildNIPST(r *require.Assertions, postCfg config.Config, nipstChallenge common.Hash, poetDb PoetDb) *types.NIPST {
	postProver := NewPostClient(&postCfg, minerID)
	poetProver, err := newRPCPoetHarnessClient()
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)
	nb := newNIPSTBuilder(minerID, postProver, poetProver,
		poetDb, verifyPost, log.NewDefault(string(minerID)))
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
	nipstChallenge := common.BytesToHash([]byte("anton"))

	postProver := NewPostClient(&postCfg, minerIDNotInitialized)
	poetProver, err := newRPCPoetHarnessClient()
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)
	poetDb := &poetDbMock{}
	nb := newNIPSTBuilder(minerIDNotInitialized, postProver, poetProver,
		poetDb, verifyPost, log.NewDefault(string(minerID)))

	npst, err := nb.BuildNIPST(&nipstChallenge)
	r.EqualError(err, "PoST not initialized")
	r.Nil(npst)

	idsToCleanup = append(idsToCleanup, minerIDNotInitialized)
	initialProof, err := postProver.Initialize()
	defer func() {
		assert.NoError(t, nb.postProver.Reset())
	}()
	r.NoError(err)
	r.NotNil(initialProof)

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
	nipstChallenge := common.BytesToHash([]byte("anton"))

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

	err = validateNIPST(npst, postCfg, common.BytesToHash([]byte("lerner")), poetDb)
	r.EqualError(err, "NIPST challenge is not equal to expected challenge")
}

func validateNIPST(npst *types.NIPST, postCfg config.Config, nipstChallenge common.Hash, poetDb PoetDb) error {
	v := &Validator{&postCfg, poetDb}
	return v.Validate(npst, nipstChallenge)
}

func TestMain(m *testing.M) {
	flag.Parse()
	initPost(minerID)
	res := m.Run()
	cleanup()
	os.Exit(res)
}

func initPost(id []byte) {
	idsToCleanup = append(idsToCleanup, id)
	_, err := NewPostClient(&postCfg, id).Initialize()
	logIfError(err)
}

func cleanup() {
	matches, err := filepath.Glob("*.bin")
	logIfError(err)
	for _, f := range matches {
		err = os.Remove(f)
		logIfError(err)
	}

	for _, id := range idsToCleanup {
		dir := shared.GetInitDir(postCfg.DataDir, id)
		err = os.RemoveAll(dir)
		logIfError(err)
	}
}

func logIfError(err error) {
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
	}
}
