package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common"
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

func (p *PoetProvingServiceClientMock) submitChallenge(challenge common.Hash,
	duration SeqWorkTicks) (*PoetRound, error) {
	return &PoetRound{}, nil
}

func (p *PoetProvingServiceClientMock) subscribeMembershipProof(r *PoetRound, challenge common.Hash,
	timeout time.Duration) (*membershipProof, error) {
	return &membershipProof{}, nil
}

func (p *PoetProvingServiceClientMock) subscribeProof(r *PoetRound, timeout time.Duration) (*poetProof, error) {
	return &poetProof{}, nil
}

type ActivationBuilderMock struct {
	nipst chan NIPST
}

func (a *ActivationBuilderMock) BuildActivationTx(proof NIPST) {
	a.nipst <- proof
}

func TestNIPSTBuilder(t *testing.T) {
	postProver := &PostProverClientMock{}
	poetProver := &PoetProvingServiceClientMock{}

	nipstChan := make(chan NIPST)
	activationBuilder := &ActivationBuilderMock{nipst: nipstChan}

	nb := NewNIPSTBuilder([]byte("id"), 1024, 600, postProver, poetProver, activationBuilder)
	nb.Start()

	select {
	case <-nipstChan:
	case <-time.After(1 * time.Second):
		t.Fail()
		return
	}

	nb.Stop()
}
