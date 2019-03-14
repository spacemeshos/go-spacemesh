package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"time"
)

type proof struct{}

type postCommitment struct {
	Root  common.Hash
	Proof proof
}

func (p *postCommitment) Valid() bool {
	return true
}

//todo: figure out which fields are needed
type MembershipProof struct {
	Root  common.Hash
	Proof proof
}

func (p *MembershipProof) Valid() bool {
	return true
}

//todo: poet proof
type PoetProof struct {
	Root  common.Hash
	Proof proof
}

func (p *PoetProof) Valid() bool {
	return true
}

type PoetService interface {
	SendPostProof(root common.Hash, p proof) error
	GetMembershipProof(root common.Hash, timeout time.Duration) (*MembershipProof, error)
	GetPoetProof(root common.Hash, timeout time.Duration) (*PoetProof, error)
}

type PostService interface {
	SendChallenge(root common.Hash, challenge common.Hash) error
	ChallengeOutput(root common.Hash, challenge common.Hash, timeout time.Duration) (*postCommitment, error)
}

type ActivationBuilder interface {
	BuildActivationTx(proof Nipst)
}

type Nipst struct {
	initialPost     *postCommitment
	membershipProof *MembershipProof
	poetProof       *PoetProof
	secondPost      *postCommitment
}

type NipstBuilder struct {
	poet              PoetService
	post              PostService
	currentNipst      Nipst
	activationBuilder ActivationBuilder
	stop              bool
	stopM             sync.Mutex
}

func NewNipstBuilder(poet PoetService, post PostService, activationBuilder ActivationBuilder) *NipstBuilder {
	return &NipstBuilder{
		poet:              poet,
		post:              post,
		currentNipst:      Nipst{},
		activationBuilder: activationBuilder,
		stop:              false,
	}
}

func (n *NipstBuilder) stopped() bool {
	n.stopM.Lock()
	defer n.stopM.Unlock()
	return n.stop
}

func (n *NipstBuilder) Stop() {
	n.stopM.Lock()
	n.stop = true
	defer n.stopM.Unlock()
}

func (n *NipstBuilder) Start() {
	n.stopM.Lock()
	n.stop = false
	go n.loop()
	defer n.stopM.Unlock()
}

func (n *NipstBuilder) loop() {
	n.loadNipst()
	postRoot, challenge := common.Hash{}, common.Hash{}

	if n.currentNipst.initialPost == nil {
		post := n.waitForPostChallenge(postRoot, challenge)

		n.currentNipst.initialPost = post
		n.persistNipst()
	}
	for {
		if n.stopped() {
			return
		}
		if n.currentNipst.membershipProof == nil {
			membership := n.waitForMembershipProof(n.currentNipst.initialPost)
			if !membership.Valid() {
				log.Error("invalid membershipProof")
				//todo: what to do when error?
				continue
			}
			n.currentNipst.membershipProof = membership
			n.persistNipst()
			if n.stopped() {
				return
			}
		}
		if n.currentNipst.poetProof == nil {
			poetProof := n.waitForPoetProof(n.currentNipst.membershipProof)
			if !poetProof.Valid() {
				log.Error("invalid poet proof")
				continue
			}
			n.currentNipst.poetProof = poetProof
			n.persistNipst()
			if n.stopped() {
				return
			}
		}
		if n.currentNipst.secondPost == nil {
			postRoot = n.currentNipst.initialPost.Root
			challenge = n.currentNipst.poetProof.Root
			post := n.waitForPostChallenge(postRoot, challenge)
			if !post.Valid() {
				log.Error("invalid second post")
				continue
			}
			n.currentNipst.secondPost = post
			n.activationBuilder.BuildActivationTx(n.currentNipst)

			n.resetNipst()

			if n.stopped() {
				return
			}
		}
	}
}

func (n *NipstBuilder) persistNipst() {
	//todo: implement
}

func (n *NipstBuilder) loadNipst() {
	//todo: implement
}

func (n *NipstBuilder) resetNipst() {
	//set up all parameters as preparation for next iteration of creating nipst
	n.currentNipst.initialPost = n.currentNipst.secondPost
	n.currentNipst.membershipProof = nil
	n.currentNipst.poetProof = nil
	n.currentNipst.secondPost = nil
	n.persistNipst()
}

func (n *NipstBuilder) waitForPostChallenge(root common.Hash, challenge common.Hash) *postCommitment {
	for {
		post, err := n.post.ChallengeOutput(root, challenge, 1*time.Second)
		if n.stopped() {
			return nil
		}
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		return post
	}
}

func (n *NipstBuilder) waitForMembershipProof(commitment *postCommitment) *MembershipProof {
	log.Info("Sending post commitment %v to POET", commitment.Root)
	n.poet.SendPostProof(commitment.Root, commitment.Proof)
	then := time.Now()
	for {
		proof, err := n.poet.GetMembershipProof(commitment.Root, 1*time.Second)
		if n.stopped() {
			return nil
		}
		if err != nil {
			continue
		}
		log.Info("Received %v proof from POET after %v ", commitment.Root, time.Since(then))
		return proof
	}
}

func (n *NipstBuilder) waitForPoetProof(m *MembershipProof) *PoetProof {
	for {
		proof, err := n.poet.GetPoetProof(m.Root, 1*time.Second)
		if n.stopped() {
			return nil
		}
		if err != nil {
			time.Sleep(1 * time.Minute)
			continue
		}
		return proof
	}

}
