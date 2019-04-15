package nipst

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/proving"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PostProverClient provides proving functionality for PoST.
type PostProverClient interface {
	// initialize is the process in which the prover commits
	// to store some data, by having its storage being filled with
	// pseudo-random data with respect to a specific id.
	// This data is the result of a computationally-expensive operation.
	initialize(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (commitment *PostProof, err error)

	// execute is the phase in which the prover received a challenge,
	// and proves that his data is still stored (or was recomputed).
	// This phase can be repeated arbitrarily many times without repeating initialization;
	// thus despite the initialization essentially serving as a proof-of-work,
	// the amortized computational complexity can be made arbitrarily small.
	execute(id []byte, challenge common.Hash, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (proof *PostProof, err error)
}

// PoetProvingServiceClient provides a gateway to a trust-less public proving
// service, which may serve many PoET proving clients, and thus enormously
// reduce the cost-per-proof for PoET since each additional proof adds
// only a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// id is the unique id of the service.
	id() string

	// submit registers a challenge in the proving service
	// open round suited for the specified duration.
	submit(challenge common.Hash, duration SeqWorkTicks) (*poetRound, error)

	// subscribeMembershipProof returns a proof which can convince a verifier
	// that the prover challenge was included in the proving service
	// round root commitment.
	subscribeMembershipProof(r *poetRound, challenge common.Hash, timeout time.Duration) (*membershipProof, error)

	// proof returns the PoET for a specific round root commitment,
	// that can convince a verifier that at least T time must have
	// passed from when the initial challenge (the root commitment)
	// was learned.
	subscribeProof(r *poetRound, timeout time.Duration) (*poetProof, error)
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
	space uint64

	// duration is a the amount of sequential work ticks
	// which is used as a proxy for time since an actual clock time
	// cannot be verified. The duration implies time unit
	// which the prover need to reserve the PoST space/data for, and is
	// a part of PoET.
	duration SeqWorkTicks

	// nipstChallenge is the challenge for PoET which is
	// constructed from the PoST commitment for the initial NIPST (per id),
	// and from the previous NIPSTs chain for all the following.
	nipstChallenge *common.Hash

	// poetRound is the round of the PoET proving service
	// in which the PoET challenge was included in.
	poetRound *poetRound

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
	postProof *PostProof
}

// initialNIPST returns an initial NIPST instance to be used in the NIPST construction.
func initialNIPST(id []byte, space uint64, duration SeqWorkTicks) *NIPST {
	return &NIPST{
		id:       id,
		space:    space,
		duration: duration,
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

func (n *NIPST) ValidateNipstChallenge(expectedChallenge *common.Hash) bool {
	return bytes.Equal(expectedChallenge[:], n.nipstChallenge[:])
}

type ActivationBuilder interface {
	BuildActivationTx(proof *NIPST)
}

type NIPSTBuilder struct {
	id                          []byte
	space                       uint64
	difficulty                  proving.Difficulty
	numberOfProvenLabels        uint8
	duration                    SeqWorkTicks
	postProver                  PostProverClient
	poetProver                  PoetProvingServiceClient
	verifyPost                  verifyPostFunc
	verifyPoetMembership        verifyPoetMembershipFunc
	verifyPoet                  verifyPoetFunc
	verifyPoetMatchesMembership verifyPoetMatchesMembershipFunc

	stop    bool
	stopM   sync.Mutex
	errChan chan error

	nipst *NIPST
}

func NewNIPSTBuilder(
	id []byte,
	space uint64,
	difficulty proving.Difficulty,
	numberOfProvenLabels uint8,
	duration SeqWorkTicks,
	postProver PostProverClient,
	poetProver PoetProvingServiceClient,
	verifyPost verifyPostFunc,
	verifyPoetMembership verifyPoetMembershipFunc,
	verifyPoet verifyPoetFunc,
	verifyPoetMatchesMembership verifyPoetMatchesMembershipFunc,
) *NIPSTBuilder {
	return &NIPSTBuilder{
		id:                          id,
		space:                       space,
		duration:                    duration,
		difficulty:                  difficulty,
		numberOfProvenLabels:        numberOfProvenLabels,
		postProver:                  postProver,
		poetProver:                  poetProver,
		verifyPost:                  verifyPost,
		verifyPoetMembership:        verifyPoetMembership,
		verifyPoet:                  verifyPoet,
		verifyPoetMatchesMembership: verifyPoetMatchesMembership,
		stop:                        false,
		errChan:                     make(chan error),
		nipst:                       initialNIPST(id, space, duration),
	}
}

var numberOfProvenLabels = uint8(10)

func NewNipstBuilder(id []byte,spaceUnit uint64, difficulty proving.Difficulty, duration SeqWorkTicks, postProver PostProverClient, poetProver PoetProvingServiceClient) *NIPSTBuilder{
	return &NIPSTBuilder{
		id:                          id,
		space:                       spaceUnit,
		duration:                    duration,
		difficulty:                  difficulty,
		numberOfProvenLabels:        numberOfProvenLabels,
		postProver:                  postProver,
		poetProver:                  poetProver,
		verifyPost:                  verifyPost,
		verifyPoetMembership:        verifyPoetMembership,
		verifyPoet:                  verifyPoet,
		verifyPoetMatchesMembership: verifyPoetMatchesMembership,
		stop:                        false,
		errChan:                     make(chan error),
		nipst:                       initialNIPST(id, spaceUnit, duration),
	}
}

func (nb *NIPSTBuilder) BuildNIPST(challenge *common.Hash) (*NIPST, error) {
	defTimeout := 5 * time.Second // TODO: replace temporary solution
	nb.nipst.load()

	if !nb.IsPostInitialized() {
		return nil, errors.New("PoST not initialized")
	}

	// Phase 0: Submit challenge to PoET service.
	if nb.nipst.poetRound == nil {
		poetChallenge := challenge

		log.Info("submitting challenge to PoET proving service "+
			"(service id: %v, challenge: %x)",
			nb.poetProver.id(), poetChallenge)

		round, err := nb.poetProver.submit(*poetChallenge, nb.duration)
		if err != nil {
			return nil, fmt.Errorf("failed to submit challenge to poet service: %v", err)
		}

		log.Info("challenge submitted to PoET proving service "+
			"(service id: %v, round id: %v)",
			nb.poetProver.id(), round.id)

		nb.nipst.nipstChallenge = poetChallenge
		nb.nipst.poetRound = round
		nb.nipst.persist()
	}

	// Phase 1: Wait for PoET service round membership proof.
	if nb.nipst.poetMembershipProof == nil {
		log.Info("querying round membership proof from PoET proving service "+
			"(service id: %v, round id: %v, challenge: %x)",
			nb.poetProver.id(), nb.nipst.poetRound.id, nb.nipst.nipstChallenge)

		mproof, err := nb.poetProver.subscribeMembershipProof(nb.nipst.poetRound, *nb.nipst.nipstChallenge, defTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to receive PoET round membership proof: %v", err)
		}

		res, err := nb.verifyPoetMembership(nb.nipst.nipstChallenge, mproof)
		if err != nil {
			return nil, fmt.Errorf("received an invalid PoET round membership proof: %v", err)
		}
		if !res {
			return nil, fmt.Errorf("received an invalid PoET round membership proof")
		}

		log.Info("received a valid round membership proof from PoET proving service "+
			"(service id: %v, round id: %v, challenge: %x)",
			nb.poetProver.id(), nb.nipst.poetRound.id, nb.nipst.nipstChallenge)

		nb.nipst.poetMembershipProof = mproof
		nb.nipst.persist()
	}

	// Phase 2: Wait for PoET service proof.
	if nb.nipst.poetProof == nil {
		log.Info("waiting for PoET proof from PoET proving service "+
			"(service id: %v, round id: %v, round root commitment: %x)",
			nb.poetProver.id(), nb.nipst.poetRound.id, nb.nipst.poetMembershipProof.root)

		proof, err := nb.poetProver.subscribeProof(nb.nipst.poetRound, defTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to receive PoET proof: %v", err)

		}

		if !nb.verifyPoetMatchesMembership(nb.nipst.poetMembershipProof, proof) {
			return nil, fmt.Errorf("received an invalid PoET proof due to "+
				"commitment value (expected: %x, found: %x)",
				nb.nipst.poetMembershipProof.root, proof.commitment)
		}

		res, err := nb.verifyPoet(proof)
		if err != nil {
			return nil, fmt.Errorf("received an invalid PoET proof: %v", err)
		}
		if !res {
			return nil, fmt.Errorf("received an invalid PoET proof")
		}

		log.Info("received a valid PoET proof from PoET proving service "+
			"(service id: %v, round id: %v)",
			nb.poetProver.id(), nb.nipst.poetRound.id)

		nb.nipst.poetProof = proof
		nb.nipst.persist()
	}

	// Phase 3: PoST execution.
	if nb.nipst.postProof == nil {
		postChallenge := common.BytesToHash(nb.nipst.poetProof.commitment)

		log.Info("starting PoST execution (challenge: %x)",
			postChallenge)

		proof, err := nb.postProver.execute(nb.id, postChallenge, nb.numberOfProvenLabels, nb.difficulty, defTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to execute PoST: %v", err)
		}

		res, err := nb.verifyPost(proof, nb.space, nb.numberOfProvenLabels, nb.difficulty)
		if err != nil {
			return nil, fmt.Errorf("received an invalid PoST proof: %v", err)
		}
		if !res {
			return nil, fmt.Errorf("received an invalid PoST proof")
		}

		log.Info("finished PoST execution (proof: %v)", proof)

		nb.nipst.postChallenge = &postChallenge
		nb.nipst.postProof = proof
		nb.nipst.persist()
	}

	log.Info("finished NIPST construction")

	return nb.nipst, nil
}

func (nb *NIPSTBuilder) IsPostInitialized() bool {
	postDataPath := filesystem.GetCanonicalPath(config.Post.DataFolder)
	labelsPath := filepath.Join(postDataPath, hex.EncodeToString(nb.id))
	_, err := os.Stat(labelsPath)
	if os.IsNotExist(err) {
		log.Info("could not find labels path at %v", labelsPath)
		return false
	}
	return true
}

func (nb *NIPSTBuilder) InitializePost() (*PostProof, error) {
	defTimeout := 5 * time.Second // TODO: replace temporary solution

	if nb.IsPostInitialized() {
		return nil, errors.New("PoST already initialized")
	}

	commitment, err := nb.postProver.initialize(nb.id, nb.space, nb.numberOfProvenLabels, nb.difficulty, defTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize PoST: %v", err)
	}

	log.Info("finished PoST initialization (commitment: %v)", commitment)

	return commitment, nil
}

func NewNIPSTWithChallenge(challenge *common.Hash) *NIPST {
	return &NIPST{nipstChallenge: challenge}
}
