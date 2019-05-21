package nipst

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
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
	initialize(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (commitment *types.PostProof, err error)

	// execute is the phase in which the prover received a challenge,
	// and proves that his data is still stored (or was recomputed).
	// This phase can be repeated arbitrarily many times without repeating initialization;
	// thus despite the initialization essentially serving as a proof-of-work,
	// the amortized computational complexity can be made arbitrarily small.
	execute(id []byte, challenge []byte, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (proof *types.PostProof, err error)
}

// PoetProvingServiceClient provides a gateway to a trust-less public proving
// service, which may serve many PoET proving clients, and thus enormously
// reduce the cost-per-proof for PoET since each additional proof adds
// only a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// id is the unique id of the service.
	id() []byte

	// submit registers a challenge in the proving service
	// open round suited for the specified duration.
	submit(challenge common.Hash, duration SeqWorkTicks) (*types.PoetRound, error)

	// subscribeMembershipProof returns a proof which can convince a verifier
	// that the prover challenge was included in the proving service
	// round root commitment.
	subscribeMembershipProof(r *types.PoetRound, challenge common.Hash, timeout time.Duration) (*MembershipProof, error)

	// proof returns the PoET for a specific round root commitment,
	// that can convince a verifier that at least T time must have
	// passed from when the initial challenge (the root commitment)
	// was learned.
	subscribeProof(r *types.PoetRound, timeout time.Duration) (*PoetProof, error)
}

// initialNIPST returns an initial NIPST instance to be used in the NIPST construction.
func initialNIPST(space uint64) *types.NIPST {
	return &types.NIPST{Space: space}
}

type ActivationBuilder interface {
	BuildActivationTx(proof *types.NIPST)
}

type builderState struct {
	nipst *types.NIPST

	// PoetRound is the round of the PoET proving service
	// in which the PoET challenge was included in.
	PoetRound *types.PoetRound

	// PoetId is the public key of the PoET proving service.
	PoetId []byte

	// PoetProofRoot is the root of the proof received from the PoET service.
	PoetProofRoot []byte
}

func (s *builderState) load() {
	// TODO(noamnelke): implement
}

func (s *builderState) persist() {
	// TODO(noamnelke): implement
}

type NIPSTBuilder struct {
	id                          []byte
	space                       uint64
	difficulty                  proving.Difficulty
	numberOfProvenLabels        uint8
	duration                    SeqWorkTicks
	postProver                  PostProverClient
	poetProver                  PoetProvingServiceClient
	poetDb                      PoetDb
	verifyPost                  verifyPostFunc
	verifyPoet                  verifyPoetFunc
	verifyPoetMatchesMembership verifyPoetMatchesMembershipFunc

	stop    bool
	stopM   sync.Mutex
	errChan chan error

	state *builderState

	log log.Log
}

type PoetDb interface {
	GetPoetProofRoot(poetId []byte, round *types.PoetRound) ([]byte, error)
	GetMembershipByPoetProofRoot(poetRoot []byte) (map[common.Hash]bool, error)
}

func NewNIPSTBuilder(
	id []byte,
	space uint64,
	difficulty proving.Difficulty,
	numberOfProvenLabels uint8,
	duration SeqWorkTicks,
	postProver PostProverClient,
	poetProver PoetProvingServiceClient,
	poetDb PoetDb,
	verifyPost verifyPostFunc,
	verifyPoet verifyPoetFunc,
	verifyPoetMatchesMembership verifyPoetMatchesMembershipFunc,
	log log.Log,
) *NIPSTBuilder {
	return &NIPSTBuilder{
		id:                          id,
		space:                       space,
		duration:                    duration,
		difficulty:                  difficulty,
		numberOfProvenLabels:        numberOfProvenLabels,
		postProver:                  postProver,
		poetProver:                  poetProver,
		poetDb:                      poetDb,
		verifyPost:                  verifyPost,
		verifyPoet:                  verifyPoet,
		verifyPoetMatchesMembership: verifyPoetMatchesMembership,
		stop:                        false,
		errChan:                     make(chan error),
		log:                         log,
		state: &builderState{
			nipst: initialNIPST(space),
		},
	}
}

var numberOfProvenLabels = uint8(10)

func NewNipstBuilder(
	id []byte,
	space uint64,
	difficulty proving.Difficulty,
	duration SeqWorkTicks,
	postProver PostProverClient,
	poetProver PoetProvingServiceClient,
	poetDb PoetDb,
	log log.Log,
) *NIPSTBuilder {
	return &NIPSTBuilder{
		id:                          id,
		space:                       space,
		duration:                    duration,
		difficulty:                  difficulty,
		numberOfProvenLabels:        numberOfProvenLabels,
		postProver:                  postProver,
		poetProver:                  poetProver,
		poetDb:                      poetDb,
		verifyPost:                  verifyPost,
		verifyPoet:                  verifyPoet,
		verifyPoetMatchesMembership: verifyPoetMatchesMembership,
		stop:                        false,
		errChan:                     make(chan error),
		log:                         log,
		state: &builderState{
			nipst: initialNIPST(space),
		},
	}
}

func (nb *NIPSTBuilder) BuildNIPST(challenge *common.Hash) (*types.NIPST, error) {
	defTimeout := 10 * time.Second // TODO: replace temporary solution
	nb.state.load()

	if !nb.IsPostInitialized() {
		return nil, errors.New("PoST not initialized")
	}

	nipst := nb.state.nipst

	// Phase 0: Submit challenge to PoET service.
	if nb.state.PoetRound == nil {
		poetChallenge := challenge

		nb.log.Info("submitting challenge to PoET proving service "+
			"(service id: %v, challenge: %x)",
			nb.poetProver.id(), poetChallenge)

		round, err := nb.poetProver.submit(*poetChallenge, nb.duration)
		if err != nil {
			return nil, fmt.Errorf("failed to submit challenge to poet service: %v", err)
		}

		nb.log.Info("challenge submitted to PoET proving service "+
			"(service id: %v, round id: %v)",
			nb.poetProver.id(), round.Id)

		nipst.NipstChallenge = poetChallenge
		nb.state.PoetRound = round
		nb.state.persist()
	}

	// Phase 1: receive proofs from PoET service
	if nb.state.PoetProofRoot == nil {
		poetProofRoot, err := nb.poetDb.GetPoetProofRoot(nb.state.PoetId, nb.state.PoetRound)
		if err != nil {
			// TODO(noamnelke): handle timeout
			return nil, fmt.Errorf("failed to find PoET proof for round: %d and id: %x",
				nb.state.PoetRound.Id, nb.state.PoetId)
		}

		membership, err := nb.poetDb.GetMembershipByPoetProofRoot(poetProofRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch membership for PoET proof") // inconsistent state
		}
		if !membership[*nipst.NipstChallenge] {
			return nil, fmt.Errorf("not a member of this round") // TODO(noamnelke): handle this case!
		}
		nb.state.PoetProofRoot = poetProofRoot
		nb.state.persist()
	}

	// Phase 2: PoST execution.
	if nipst.PostProof == nil {
		nb.log.Info("starting PoST execution (challenge: %x)", nb.state.PoetProofRoot)

		proof, err := nb.postProver.execute(nb.id, nb.state.PoetProofRoot, nb.numberOfProvenLabels, nb.difficulty, defTimeout)
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

		nb.log.Info("finished PoST execution (proof: %v)", proof)

		nipst.PostProof = proof
		nb.state.persist()
	}

	nb.log.Info("finished NIPST construction")

	nb.state = &builderState{
		nipst: initialNIPST(nb.space),
	}
	return nipst, nil
}

func (nb *NIPSTBuilder) IsPostInitialized() bool {
	postDataPath := filesystem.GetCanonicalPath(config.Post.DataFolder)
	labelsPath := filepath.Join(postDataPath, hex.EncodeToString(nb.id))
	_, err := os.Stat(labelsPath)
	if os.IsNotExist(err) {
		nb.log.Info("could not find labels path at %v", labelsPath)
		return false
	}
	return true
}

func (nb *NIPSTBuilder) InitializePost() (*types.PostProof, error) {
	defTimeout := 5 * time.Second // TODO: replace temporary solution

	if nb.IsPostInitialized() {
		return nil, errors.New("PoST already initialized")
	}

	commitment, err := nb.postProver.initialize(nb.id, nb.space, nb.numberOfProvenLabels, nb.difficulty, defTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize PoST: %v", err)
	}

	nb.log.Info("finished PoST initialization (commitment: %v)", commitment)

	return commitment, nil
}

func NewNIPSTWithChallenge(challenge *common.Hash) *types.NIPST {
	return &types.NIPST{
		Space:          0,
		NipstChallenge: challenge,
		PostProof: &types.PostProof{
			Identity:     []byte(nil),
			Challenge:    []byte(nil),
			MerkleRoot:   []byte(nil),
			ProofNodes:   [][]byte(nil),
			ProvenLeaves: [][]byte(nil),
		},
	}
}
