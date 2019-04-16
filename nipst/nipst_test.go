package nipst

import (
	"encoding/hex"
	"flag"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/proving"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var minerID = []byte("id")
var idsToCleanup [][]byte
var spaceUnit = uint64(1024)
var difficulty = proving.Difficulty(5)

type PostProverClientMock struct{}

func (p *PostProverClientMock) initialize(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*PostProof, error) {
	return &PostProof{}, nil
}

func (p *PostProverClientMock) execute(id []byte, challenge common.Hash, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*PostProof, error) {
	return &PostProof{}, nil
}

type PoetProvingServiceClientMock struct{}

func (p *PoetProvingServiceClientMock) id() string {
	return "1"
}

func (p *PoetProvingServiceClientMock) submit(challenge common.Hash,
	duration SeqWorkTicks) (*PoetRound, error) {
	return &PoetRound{}, nil
}

func (p *PoetProvingServiceClientMock) subscribeMembershipProof(r *PoetRound, challenge common.Hash,
	timeout time.Duration) (*MembershipProof, error) {
	return &MembershipProof{}, nil
}

func (p *PoetProvingServiceClientMock) subscribeProof(r *PoetRound, timeout time.Duration) (*PoetProof, error) {
	return &PoetProof{}, nil
}

func TestNIPSTBuilderWithMocks(t *testing.T) {
	assert := require.New(t)

	postProverMock := &PostProverClientMock{}
	poetProverMock := &PoetProvingServiceClientMock{}
	verifyPostMock := func(*PostProof, uint64, uint8, proving.Difficulty) (bool, error) { return true, nil }
	verifyMembershipMock := func(*common.Hash, *MembershipProof) (bool, error) { return true, nil }
	verifyPoetMock := func(*PoetProof) (bool, error) { return true, nil }
	verifyPoetMembershipMock := func(*MembershipProof, *PoetProof) bool { return true }

	nb := NewNIPSTBuilder(
		minerID,
		1024,
		5,
		proving.NumberOfProvenLabels,
		600,
		postProverMock,
		poetProverMock,
		verifyPostMock,
		verifyMembershipMock,
		verifyPoetMock,
		verifyPoetMembershipMock,
	)
	hash := common.BytesToHash([]byte("anton"))
	npst, err := nb.BuildNIPST(&hash)
	assert.NoError(err)

	assert.True(npst.Valid())
}

func TestNIPSTBuilderWithClients(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	nipstChallenge := common.BytesToHash([]byte("anton"))

	npst := buildNIPST(r, spaceUnit, difficulty, numberOfProvenLabels, nipstChallenge)

	err := validateNIPST(npst, spaceUnit, difficulty, numberOfProvenLabels, nipstChallenge)
	r.NoError(err)
}

func buildNIPST(r *require.Assertions, spaceUnit uint64, difficulty proving.Difficulty, numberOfProvenLabels uint8, nipstChallenge common.Hash) *NIPST {
	postProver := NewPostClient()
	poetProver, err := newRPCPoetHarnessClient()
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)
	nb := NewNIPSTBuilder(
		minerID,
		spaceUnit,
		difficulty,
		numberOfProvenLabels,
		600,
		postProver,
		poetProver,
		verifyPost,
		verifyPoetMembership,
		verifyPoet,
		verifyPoetMatchesMembership,
	)
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

	postProver := NewPostClient()
	poetProver, err := newRPCPoetHarnessClient()
	r.NotNil(poetProver)
	defer func() {
		err = poetProver.CleanUp()
		r.NoError(err)
	}()
	r.NoError(err)
	nb := NewNIPSTBuilder(
		minerIDNotInitialized,
		spaceUnit,
		difficulty,
		numberOfProvenLabels,
		600,
		postProver,
		poetProver,
		verifyPost,
		verifyPoetMembership,
		verifyPoet,
		verifyPoetMatchesMembership,
	)

	npst, err := nb.BuildNIPST(&nipstChallenge)
	r.EqualError(err, "PoST not initialized")
	r.Nil(npst)

	idsToCleanup = append(idsToCleanup, minerIDNotInitialized)
	initialProof, err := nb.InitializePost()
	r.NoError(err)
	r.NotNil(initialProof)

	npst, err = nb.BuildNIPST(&nipstChallenge)
	r.NoError(err)
	r.NotNil(npst)

	err = validateNIPST(npst, spaceUnit, difficulty, numberOfProvenLabels, nipstChallenge)
	r.NoError(err)
}

func TestValidator_Validate(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	r := require.New(t)

	nipstChallenge := common.BytesToHash([]byte("anton"))

	npst := buildNIPST(r, spaceUnit, difficulty, numberOfProvenLabels, nipstChallenge)

	err := validateNIPST(npst, spaceUnit, difficulty, numberOfProvenLabels, nipstChallenge)
	r.NoError(err)

	err = validateNIPST(npst, spaceUnit+1, difficulty, numberOfProvenLabels, nipstChallenge)
	r.EqualError(err, "PoST space (1024) is less than a single space unit (1025)")

	err = validateNIPST(npst, spaceUnit, difficulty+1, numberOfProvenLabels, nipstChallenge)
	r.EqualError(err, "PoST proof invalid: validation failed: number of derived leaf indices (9) doesn't match number of included proven leaves (8)")

	err = validateNIPST(npst, spaceUnit, difficulty, numberOfProvenLabels+5, nipstChallenge)
	r.EqualError(err, "PoST proof invalid: validation failed: number of derived leaf indices (10) doesn't match number of included proven leaves (8)")

	err = validateNIPST(npst, spaceUnit, difficulty, numberOfProvenLabels, common.BytesToHash([]byte("lerner")))
	r.EqualError(err, "NIPST challenge is not equal to expected challenge")
}

func validateNIPST(npst *NIPST, spaceUnit uint64, difficulty proving.Difficulty, numberOfProvenLabels uint8,
	nipstChallenge common.Hash) error {

	v := &Validator{
		PostParams: PostParams{
			Difficulty:           difficulty,
			NumberOfProvenLabels: numberOfProvenLabels,
			SpaceUnit:            spaceUnit,
		},
		verifyPost:                  verifyPost,
		verifyPoetMembership:        verifyPoetMembership,
		verifyPoet:                  verifyPoet,
		verifyPoetMatchesMembership: verifyPoetMatchesMembership,
	}
	return v.Validate(npst, nipstChallenge)
}

func TestMain(m *testing.M) {
	flag.Parse()
	initPost(minerID, spaceUnit, 0, difficulty)
	res := m.Run()
	cleanup()
	os.Exit(res)
}

func initPost(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty) {
	defTimeout := 5 * time.Second
	idsToCleanup = append(idsToCleanup, id)
	_, err := NewPostClient().initialize(id, space, numberOfProvenLabels, difficulty, defTimeout)
	logIfError(err)
}

func cleanup() {
	matches, err := filepath.Glob("*.bin")
	logIfError(err)
	for _, f := range matches {
		err = os.Remove(f)
		logIfError(err)
	}

	postDataPath := filesystem.GetCanonicalPath(config.Post.DataFolder)
	for _, id := range idsToCleanup {
		labelsPath := filepath.Join(postDataPath, hex.EncodeToString(id))
		err = os.RemoveAll(labelsPath)
		logIfError(err)
	}
}

func logIfError(err error) {
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
	}
}
