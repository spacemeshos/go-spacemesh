package nipst

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"time"
)

type postProof []byte

func (p *postProof) Valid() bool {
	return true
}

type PoetRound struct {
	id uint64
}

type merkleProof []byte

type membershipProof struct {
	Root  common.Hash
	Proof merkleProof
}

func (p *membershipProof) Valid() bool {
	// TODO: implement
	return true

}

// poetProof can convince a verifier that at least T time must have passed
// from the time the initial challenge was learned.
type poetProof []byte

func (p *poetProof) Valid() bool {
	// TODO: implement
	return true
}

type SeqWorkTicks uint64

type Space uint64

// PostProverClient provides proving functionality for PoST.
type PostProverClient interface {
	// initialize is the process in which the prover commits
	// to store some data, by having its storage being filled with
	// pseudo-random data with respect to a specific id.
	// This data is the result of a computationally-expensive operation.
	initialize(id []byte, space Space, timeout time.Duration) (commitment *postProof, err error)

	// execute is the phase in which the prover received a challenge,
	// and proves that his data is still stored (or was recomputed).
	// This phase can be repeated arbitrarily many times without repeating initialization;
	// thus despite the initialization essentially serving as a proof-of-work,
	// the amortized computational complexity can be made arbitrarily small.
	execute(id []byte, challenge common.Hash, timeout time.Duration) (proof *postProof, err error)
}

// PoetProvingServiceClient provides a gateway to a trust-less public proving
// service, which may serve many PoET proving clients, and thus enormously
// reduce the cost-per-proof for PoET since each additional proof adds
// only a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// id is the unique id of the service.
	id() string

	// submitChallenge registers a challenge in the proving service
	// open round suited for the specified duration.
	submitChallenge(challenge common.Hash, duration SeqWorkTicks) (*PoetRound, error)

	// subscribeMembershipProof returns a proof which can convince a verifier
	// that the prover challenge was included in the proving service
	// round root commitment.
	subscribeMembershipProof(r *PoetRound, challenge common.Hash, timeout time.Duration) (*membershipProof, error)

	// proof returns the PoET for a specific round root commitment,
	// that can convince a verifier that at least T time must have
	// passed from when the initial challenge (the root commitment)
	// was learned.
	subscribeProof(r *PoetRound, timeout time.Duration) (*poetProof, error)
}

// NIPST is Non-Interactive Proof of Space-Time.
// Given an id, a space parameter S, a duration D and a challenge C,
// it can convince a verifier that (1) the prover expended S * D space-time
// after learning the challenge C. (2) the prover did not know the NIPST until D time
// after the prover learned C.
type NIPST struct {
	// id is the unique identifier which
	// the entire NIPST chain is bound to.
	id []byte

	// space is the amount of storage which the prover
	// requires to dedicate for generating the NIPST.
	space Space

	// duration is a the amount of sequential work ticks
	// which is used as a proxy for time since an actual clock time
	// cannot be verified. The duration implies time unit
	// which the prover need to reserve the PoST space/data for, and is
	// a part of PoET.
	duration SeqWorkTicks

	// prev is the previous NIPST, creating a NIPST chain
	// in respect to a specific id.
	// TODO(moshababo): check whether the previous NIPST is sufficient, or that the ATX is necessary
	prev *NIPST

	// postCommitment is the result of the PoST
	// initialization process.
	postCommitment *postProof

	// poetChallenge is the challenge for PoET which is
	// constructed from the PoST commitment for the initial NIPST (per id),
	// and from the previous NIPSTs chain for all the following.
	poetChallenge *common.Hash

	// poetRound is the round of the PoET proving service
	// in which the PoET challenge was included in.
	poetRound *PoetRound

	// poetMembershipProof is the proof that the PoET challenge
	// was included in the proving service round root commitment.
	poetMembershipProof *membershipProof

	// poetProof is the proof that at least T time must have
	// passed from when the initial challenge (the root commitment)
	// was learned.
	poetProof *poetProof

	// postChallenge is the challenge for PoST which is
	// constructed from the PoET proof.
	postChallenge *common.Hash

	// postProof is the proof that the prover data
	// is still stored (or was recomputed).
	postProof *postProof
}

// initialNIPST returns an initial NIPST instance to be used in the NIPST construction.
func initialNIPST(id []byte, space Space, duration SeqWorkTicks, prev *NIPST) *NIPST {
	return &NIPST{
		id:       id,
		space:    space,
		duration: duration,
		prev:     prev,
	}
}

func (n *NIPST) persist() {
	// TODO: implement
}

func (n *NIPST) load() {
	// TODO: implement
}

func (n *NIPST) Valid() bool {
	// TODO: implement
	return true
}

type ActivationBuilder interface {
	BuildActivationTx(proof NIPST)
}

type NIPSTBuilder struct {
	id                []byte
	space             Space
	duration          SeqWorkTicks
	postProver        PostProverClient
	poetProver        PoetProvingServiceClient
	activationBuilder ActivationBuilder
	stop              bool
	stopM             sync.Mutex
	errChan           chan error

	nipst *NIPST
}

func NewNIPSTBuilder(id []byte, space Space, duration SeqWorkTicks, postProver PostProverClient,
	poetProver PoetProvingServiceClient, activationBuilder ActivationBuilder) *NIPSTBuilder {
	return &NIPSTBuilder{
		id:                id,
		space:             space,
		duration:          duration,
		postProver:        postProver,
		poetProver:        poetProver,
		activationBuilder: activationBuilder,
		stop:              false,
		errChan:           make(chan error),
		nipst:             initialNIPST(id, space, duration, nil),
	}
}

func (nb *NIPSTBuilder) stopped() bool {
	nb.stopM.Lock()
	defer nb.stopM.Unlock()
	return nb.stop
}

func (nb *NIPSTBuilder) Stop() {
	nb.stopM.Lock()
	nb.stop = true
	defer nb.stopM.Unlock()
}

func (nb *NIPSTBuilder) Start() {
	nb.stopM.Lock()
	nb.stop = false
	go nb.loop()
	defer nb.stopM.Unlock()
}

func (nb *NIPSTBuilder) loop() {
	defTimeout := 1 * time.Second // temporary solution
	nb.nipst.load()

	// Phase 0: PoST initialization.
	if nb.nipst.postCommitment == nil {
		log.Info("starting PoST initialization, id: %x, space: %v",
			nb.id, nb.space)

		com, err := nb.postProver.initialize(nb.id, nb.space, defTimeout)
		if err != nil {
			nb.errChan <- fmt.Errorf("failed to initialize PoST: %v", err)
			return
		}
		if !com.Valid() {
			nb.errChan <- errors.New("received an invalid PoST commitment")
			return
		}

		log.Info("finished PoST initialization, commitment: %x",
			*com)

		nb.nipst.postCommitment = com
		nb.nipst.persist()
	}

	for {
		if nb.stopped() {
			return
		}

		// Phase 1: Submit challenge to PoET service.
		if nb.nipst.poetRound == nil {
			var poetChallenge common.Hash

			// If it's the first NIPST in the chain, use the PoST commitment as
			// the PoET challenge. Otherwise, use the previous NIPST/ATX hash.
			if nb.nipst.prev == nil {
				// TODO(moshababo): check what exactly need to be hashed.
				poetChallenge = crypto.Keccak256Hash(nb.id, *nb.nipst.postCommitment)
			} else {
				// TODO(moshababo): check what exactly need to be hashed.
				poetChallenge = crypto.Keccak256Hash(*nb.nipst.prev.postProof)
			}

			log.Info("submitting challenge to PoET proving service, "+
				"service id: %v, challenge: %x",
				nb.poetProver.id(), poetChallenge)

			round, err := nb.poetProver.submitChallenge(poetChallenge, nb.duration)
			if err != nil {
				nb.errChan <- fmt.Errorf("failed to submit challenge to poet service: %v", err)
				return
			}

			log.Info("challenge submitted to PoET proving service, "+
				"service id: %v, round id: %v",
				nb.poetProver.id(), round.id)

			nb.nipst.poetChallenge = &poetChallenge
			nb.nipst.poetRound = round
			nb.nipst.persist()
		}

		if nb.stopped() {
			return
		}

		// Phase 2: Wait for PoET service round membership proof.
		if nb.nipst.poetMembershipProof == nil {
			log.Info("querying round membership proof from PoET proving service, "+
				"service id: %v, round id: %v, challenge: %x",
				nb.poetProver.id(), nb.nipst.poetRound.id, nb.nipst.poetChallenge)

			proof, err := nb.poetProver.subscribeMembershipProof(nb.nipst.poetRound, *nb.nipst.poetChallenge, defTimeout)
			if err != nil {
				nb.errChan <- fmt.Errorf("failed to receive PoET round membership proof: %v", err)
				continue
			}
			if !proof.Valid() {
				nb.errChan <- errors.New("receive an invalid PoET round membership proof")
				continue
			}

			log.Info("received a valid round membership proof from PoET proving service, "+
				"service id: %v, round id: %v, challenge: %x",
				nb.poetProver.id(), nb.nipst.poetRound.id, nb.nipst.poetChallenge)

			nb.nipst.poetMembershipProof = proof
			nb.nipst.persist()
		}

		if nb.stopped() {
			return
		}

		// Phase 3: Wait for PoET service proof.
		if nb.nipst.poetProof == nil {
			log.Info("waiting for PoET proof from PoET proving service, "+
				"service id: %v, round id: %v, round root commitment: %x",
				nb.poetProver.id(), nb.nipst.poetRound.id, nb.nipst.poetMembershipProof.Root)

			proof, err := nb.poetProver.subscribeProof(nb.nipst.poetRound, defTimeout)
			if err != nil {
				nb.errChan <- fmt.Errorf("failed to receive PoET proof: %v", err)
				continue
			}
			if !proof.Valid() {
				log.Error("receive an invalid PoET proof")
				continue
			}

			log.Info("received a valid PoET proof from PoET proving service, "+
				"service id: %v, round id: %v",
				nb.poetProver.id(), nb.nipst.poetRound.id)

			nb.nipst.poetProof = proof
			nb.nipst.persist()
		}

		if nb.stopped() {
			return
		}

		// Phase 4: PoST execution.
		if nb.nipst.postProof == nil {
			// TODO(moshababo): check what exactly need to be hashed.
			postChallenge := crypto.Keccak256Hash((*nb.nipst.poetProof)[:])

			log.Info("starting PoST execution, challenge: %x",
				postChallenge)

			proof, err := nb.postProver.execute(nb.id, postChallenge, defTimeout)
			if err != nil {
				nb.errChan <- fmt.Errorf("failed to execute PoST: %v", err)
				continue
			}
			if !proof.Valid() {
				log.Error("received an invalid PoST proof")
				continue
			}

			log.Info("finished PoST execution, proof: %x",
				*proof)

			nb.nipst.postChallenge = &postChallenge
			nb.nipst.postProof = proof
			nb.nipst.persist()
		}

		log.Info("finished NIPST construction")

		// build the ATX from the NIPST.
		nb.activationBuilder.BuildActivationTx(*nb.nipst)

		// create and set a new NIPST instance for the next iteration.
		nb.nipst = initialNIPST(nb.id, nb.space, nb.duration, nb.nipst)
		nb.nipst.persist()
	}
}
