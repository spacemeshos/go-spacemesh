package nipst

import (
	"flag"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func (p *postProverClientMock) initialize(id []byte, timeout time.Duration) (*types.PostProof, error) {
	return &types.PostProof{}, nil
}

func (p *postProverClientMock) execute(id []byte, challenge []byte, timeout time.Duration) (*types.PostProof, error) {
	return &types.PostProof{}, nil
}

func (p *postProverClientMock) SetLogger(shared.Logger) {}

func (p *postProverClientMock) SetPostParams(logicalDrive string, commitmentSize uint64) {}

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
	verifyPost := func(*types.PostProof, uint64, uint, uint) (bool, error) { return true, nil }

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postCfg, postProver, poetProver,
		poetDb, verifyPost, log.NewDefault(string(minerID)))
	hash := common.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash)
	assert.NoError(err)
	assert.NotNil(npst)
}

func TestInitializePost(t *testing.T) {
	assert := require.New(t)

	postProver := NewPostClient(&postCfg)
	poetProver := &poetProvingServiceClientMock{}
	verifyPost := func(*types.PostProof, uint64, uint, uint) (bool, error) { return true, nil }

	poetDb := &poetDbMock{}

	nb := newNIPSTBuilder(minerID, postCfg, postProver, poetProver,
		poetDb, verifyPost, log.NewDefault(string(minerID)))
	drive := "/tmp/anton"
	unitSize := 2048
	_, err := nb.InitializePost(drive, uint64(unitSize))
	assert.NoError(err)
	assert.Equal(nb.postCfg.DataDir, drive)
	assert.Equal(nb.postCfg.SpacePerUnit, uint64(unitSize))

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
	postProver := NewPostClient(&postCfg)
	poetProver, err := newRPCPoetHarnessClient()
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)
	nb := newNIPSTBuilder(minerID, postCfg, postProver, poetProver,
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

	postProver := NewPostClient(&postCfg)
	poetProver, err := newRPCPoetHarnessClient()
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)
	poetDb := &poetDbMock{}
	nb := newNIPSTBuilder(minerIDNotInitialized, postCfg, postProver, poetProver,
		poetDb, verifyPost, log.NewDefault(string(minerID)))

	npst, err := nb.BuildNIPST(&nipstChallenge)
	r.EqualError(err, "PoST not initialized")
	r.Nil(npst)

	idsToCleanup = append(idsToCleanup, minerIDNotInitialized)
	initialProof, err := nb.InitializePost(postCfg.DataDir, postCfg.SpacePerUnit)
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
	v := &Validator{&postCfg, poetDb, verifyPost}
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
	defTimeout := 5 * time.Second
	idsToCleanup = append(idsToCleanup, id)
	_, err := NewPostClient(&postCfg).initialize(id, defTimeout)
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
