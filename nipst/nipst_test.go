package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type PoetMock struct {
	membership *MembershipProof
	poetP      *PoetProof
}

type PostMock struct {
	firstP  *postCommitment
	secondP *postCommitment
}

type ActivationMock struct {
	nipst chan Nipst
}

func (a *ActivationMock) BuildActivationTx(proof Nipst) {
	a.nipst <- proof
}

func (*PoetMock) SendPostProof(root common.Hash, p proof) (Round, error) {
	return 0, nil
}
func (p *PoetMock) GetMembershipProof(rnd Round, root common.Hash, timeout time.Duration) (*MembershipProof, error) {
	return p.membership, nil
}
func (p *PoetMock) Proof(rnd Round, root common.Hash, timeout time.Duration) (*PoetProof, error) {
	return p.poetP, nil
}

func (*PostMock) SendChallenge(root common.Hash, challenge common.Hash) error {
	return nil
}

func (p *PostMock) ChallengeOutput(root common.Hash, challenge common.Hash, timeout time.Duration) (*postCommitment, error) {
	return p.firstP, nil
}

func TestNipstBuilder_Start(t *testing.T) {

}

func TestMembershipProof_loop(t *testing.T) {
	nipst := Nipst{&postCommitment{}, &MembershipProof{}, &PoetProof{}, &postCommitment{}, nil}

	poet := PoetMock{nipst.membershipProof, nipst.poetProof}
	post := PostMock{nipst.initialPost, nipst.secondPost}
	npstChan := make(chan Nipst)
	activation := ActivationMock{npstChan}
	builder := NewNipstBuilder(&poet, &post, &activation)
	builder.Start()

	timer := time.NewTimer(1 * time.Second)
	select {
	case recv := <-npstChan:
		recv.round = nipst.round
		assert.Equal(t, recv, nipst)
	case <-timer.C:
		t.Fail()
		return
	}
	builder.Stop()
}
