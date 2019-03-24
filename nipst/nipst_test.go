package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"testing"
	"time"
)

type PostProverMock struct{}

func (p *PostProverMock) initialize(id []byte, space Space,
	timeout time.Duration) (*postProof, error) {
	return &postProof{255, 255, 255, 255}, nil
}

func (p *PostProverMock) execute(id []byte, challenge common.Hash,
	timeout time.Duration) (*postProof, error) {
	return &postProof{255, 255, 255, 255}, nil
}

type PoetProvingServiceMock struct{}

func (p *PoetProvingServiceMock) id() string {
	return "1"
}

func (p *PoetProvingServiceMock) submitChallenge(challenge common.Hash,
	duration SeqWorkTicks) (*PoetRound, error) {
	return &PoetRound{}, nil
}

func (p *PoetProvingServiceMock) subscribeMembershipProof(r *PoetRound, challenge common.Hash,
	timeout time.Duration) (*membershipProof, error) {
	return &membershipProof{}, nil
}

func (p *PoetProvingServiceMock) subscribeProof(r *PoetRound, timeout time.Duration) (*poetProof, error) {
	return &poetProof{}, nil
}

type ActivationBuilderMock struct {
	nipst chan NIPST
}

func (a *ActivationBuilderMock) BuildActivationTx(proof NIPST) {
	a.nipst <- proof
}

func TestNIPSTBuilder(t *testing.T) {
	postProver := &PostProverMock{}
	poetProver := &PoetProvingServiceMock{}

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
