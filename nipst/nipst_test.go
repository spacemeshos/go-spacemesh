package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/post/proving"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type PostProverClientMock struct{}

func (p *PostProverClientMock) initialize(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*postProof, error) {
	return &postProof{}, nil
}

func (p *PostProverClientMock) execute(id []byte, challenge common.Hash, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*postProof, error) {
	return &postProof{}, nil
}

type PoetProvingServiceClientMock struct{}

func (p *PoetProvingServiceClientMock) id() string {
	return "1"
}

func (p *PoetProvingServiceClientMock) submit(challenge common.Hash,
	duration SeqWorkTicks) (*poetRound, error) {
	return &poetRound{}, nil
}

func (p *PoetProvingServiceClientMock) subscribeMembershipProof(r *poetRound, challenge common.Hash,
	timeout time.Duration) (*membershipProof, error) {
	return &membershipProof{}, nil
}

func (p *PoetProvingServiceClientMock) subscribeProof(r *poetRound, timeout time.Duration) (*poetProof, error) {
	return &poetProof{}, nil
}

type ActivationBuilderMock struct {
	nipst chan *NIPST
}

func (a *ActivationBuilderMock) BuildActivationTx(proof *NIPST) {
	a.nipst <- proof
}

func TestNIPSTBuilderWithMocks(t *testing.T) {
	assert := require.New(t)

	postProverMock := &PostProverClientMock{}
	poetProverMock := &PoetProvingServiceClientMock{}
	verifyPostMock := func(*postProof, uint64, uint8, proving.Difficulty) (bool, error) { return true, nil }
	verifyMembershipMock := func(*common.Hash, *membershipProof) (bool, error) { return true, nil }
	verifyPoetMock := func(*poetProof) (bool, error) { return true, nil }
	verifyPoetMembershipMock := func(*membershipProof, *poetProof) bool { return true }

	nipstChan := make(chan *NIPST)
	activationBuilder := &ActivationBuilderMock{nipst: nipstChan}

	nb := NewNIPSTBuilder(
		[]byte("id"),
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
		activationBuilder,
	)
	nb.Start()

	select {
	case nipst := <-nipstChan:
		assert.True(nipst.Valid())
	case <-time.After(5 * time.Second):
		assert.Fail("nipst creation timeout")
		return
	}

	nb.Stop()
}

func TestNIPSTBuilderWithClients(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	assert := require.New(t)

	postProver := newPostClient()

	poetProver, err := newRPCPoetHarnessClient()
	defer func() {
		err := poetProver.CleanUp()
		assert.NoError(err)
	}()
	assert.NoError(err)
	assert.NotNil(poetProver)

	nipstChan := make(chan *NIPST)
	activationBuilder := &ActivationBuilderMock{nipst: nipstChan}

	nb := NewNIPSTBuilder(
		[]byte("id"),
		1024,
		5,
		proving.NumberOfProvenLabels,
		600,
		postProver,
		poetProver,
		verifyPost,
		verifyMembership,
		verifyPoet,
		verifyPoetMembership,
		activationBuilder,
	)

	done := make(chan struct{})
	go func() {
		select {
		case err := <-nb.errChan:
			assert.Fail(err.Error())
		case <-done:
		}
	}()

	nb.Start()

	select {
	case nipst := <-nipstChan:
		assert.True(nipst.Valid())
	case <-time.After(5 * time.Second):
		assert.Fail("timeout")
	}

	nb.Stop()
	close(done)
}
