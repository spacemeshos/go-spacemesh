package nipst

import (
	"github.com/magiconair/properties/assert"
	"github.com/spacemeshos/go-spacemesh/common"
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

func (*PoetMock) SendPostProof(root common.Hash, p proof) error {
	return nil
}
func (p *PoetMock) GetMembershipProof(root common.Hash, timeout time.Duration) (*MembershipProof, error) {
	return p.membership, nil
}
func (p *PoetMock) GetPoetProof(root common.Hash, timeout time.Duration) (*PoetProof, error) {
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
	nipst := Nipst{&postCommitment{}, &MembershipProof{}, &PoetProof{}, &postCommitment{}}

	poet := PoetMock{nipst.membershipProof, nipst.poetProof}
	post := PostMock{nipst.initialPost, nipst.secondPost}
	npstChan := make(chan Nipst)
	activation := ActivationMock{npstChan}
	builder := NewNipstBuilder(&poet, &post, &activation)
	builder.Start()

	timer := time.NewTimer(1 * time.Second)
	select {
	case recv := <-npstChan:
		assert.Equal(t, recv, nipst)
	case <-timer.C:
		t.Fail()
		return
	}
	builder.Stop()
}
