package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type PostProverClientMock struct{}

func (p *PostProverClientMock) initialize(id []byte, space Space,
	timeout time.Duration) (*postProof, error) {
	return &postProof{255, 255, 255, 255}, nil
}

func (p *PostProverClientMock) execute(id []byte, challenge common.Hash,
	timeout time.Duration) (*postProof, error) {
	return &postProof{255, 255, 255, 255}, nil
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
	nipst chan NIPST
}

func (a *ActivationBuilderMock) BuildActivationTx(proof NIPST) {
	a.nipst <- proof
}

func TestNIPSTBuilderWithMockClients(t *testing.T) {
	assert := require.New(t)

	postProver := &PostProverClientMock{}
	poetProver := &PoetProvingServiceClientMock{}

	nipstChan := make(chan NIPST)
	activationBuilder := &ActivationBuilderMock{nipst: nipstChan}

	nb := NewNIPSTBuilder([]byte("id"), 1024, 600, postProver, poetProver, activationBuilder)
	nb.Start()

	select {
	case <-nipstChan:
	case <-time.After(5 * time.Second):
		assert.Fail("timeout")
		return
	}

	nb.Stop()
}

func TestNIPSTBuilderWithRPCClients(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	assert := require.New(t)

	poetProver, err := newRPCPoetHarness()
	defer func() {
		err := poetProver.cleanUp()

		assert.NoError(err)
	}()
	assert.NoError(err)
	assert.NotNil(poetProver)

	// TODO(moshababo): replace post client mock to a package client or rpc client.
	postProver := &PostProverClientMock{}

	nipstChan := make(chan NIPST)
	activationBuilder := &ActivationBuilderMock{nipst: nipstChan}

	nb := NewNIPSTBuilder([]byte("id"), 1024, 600, postProver, poetProver, activationBuilder)

	done := make(chan struct{})
	go func() {
		select {
		case err := <-nb.errChan:
			assert.NoError(err)
		case <-done:
		}
	}()

	nb.Start()

	select {
	case <-nipstChan:
	case <-time.After(5 * time.Second):
		assert.Fail("timeout")
	}

	nb.Stop()
	close(done)
}
