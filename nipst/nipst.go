package nipst

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/persistence"
	"github.com/spacemeshos/post/shared"
	"sync"
	"time"
)

// PostProverClient provides proving functionality for PoST.
type PostProverClient interface {
	// initialize is the process in which the prover commits
	// to store some data, by having its storage being filled with
	// pseudo-random data with respect to a specific id.
	// This data is the result of a computationally-expensive operation.
	initialize(id []byte, timeout time.Duration) (commitment *types.PostProof, err error)

	// execute is the phase in which the prover received a challenge,
	// and proves that his data is still stored (or was recomputed).
	// This phase can be repeated arbitrarily many times without repeating initialization;
	// thus despite the initialization essentially serving as a proof-of-work,
	// the amortized computational complexity can be made arbitrarily small.
	execute(id []byte, challenge []byte, timeout time.Duration) (proof *types.PostProof, err error)

	SetLogger(logger shared.Logger)
}

// PoetProvingServiceClient provides a gateway to a trust-less public proving
// service, which may serve many PoET proving clients, and thus enormously
// reduce the cost-per-proof for PoET since each additional proof adds
// only a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// submit registers a challenge in the proving service
	// open round suited for the specified duration.
	submit(challenge common.Hash) (*types.PoetRound, error)
	getPoetServiceId() ([types.PoetServiceIdLength]byte, error)
}

// initialNIPST returns an initial NIPST instance to be used in the NIPST construction.
func initialNIPST(space uint64) *types.NIPST {
	return &types.NIPST{Space: space}
}

type builderState struct {
	nipst *types.NIPST

	// PoetRound is the round of the PoET proving service
	// in which the PoET challenge was included in.
	PoetRound *types.PoetRound

	// PoetServiceId is the public key of the PoET proving service.
	PoetServiceId [types.PoetServiceIdLength]byte

	// PoetProofRef is the root of the proof received from the PoET service.
	PoetProofRef []byte
}

func (s *builderState) load() {

}

func (s *builderState) persist() {
	// TODO(noamnelke): implement
}

type NIPSTBuilder struct {
	id         []byte
	postCfg    config.Config
	postProver PostProverClient
	poetProver PoetProvingServiceClient
	poetDb     PoetDb
	verifyPost verifyPostFunc

	stop    bool
	stopM   sync.Mutex
	errChan chan error

	state *builderState

	log log.Log
}

type PoetDb interface {
	SubscribeToProofRef(poetId [types.PoetServiceIdLength]byte, roundId uint64) chan []byte
	GetMembershipMap(proofRef []byte) (map[common.Hash]bool, error)
}

func NewNIPSTBuilder(id []byte, postCfg config.Config, postProver PostProverClient,
	poetProver PoetProvingServiceClient, poetDb PoetDb, log log.Log) *NIPSTBuilder {
	return newNIPSTBuilder(
		id,
		postCfg,
		postProver,
		poetProver,
		poetDb,
		verifyPost,
		log,
	)
}

func newNIPSTBuilder(
	id []byte,
	postCfg config.Config,
	postProver PostProverClient,
	poetProver PoetProvingServiceClient,
	poetDb PoetDb,
	verifyPost verifyPostFunc,
	log log.Log,
) *NIPSTBuilder {
	return &NIPSTBuilder{
		id:         id,
		postCfg:    postCfg,
		postProver: postProver,
		poetProver: poetProver,
		poetDb:     poetDb,
		verifyPost: verifyPost,
		stop:       false,
		errChan:    make(chan error),
		log:        log,
		state: &builderState{
			nipst: initialNIPST(postCfg.SpacePerUnit),
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
		poetServiceId, err := nb.poetProver.getPoetServiceId()
		if err != nil {
			return nil, fmt.Errorf("failed to get PoET service ID: %v", err)
		}
		nb.state.PoetServiceId = poetServiceId

		poetChallenge := challenge

		nb.log.Debug("submitting challenge to PoET proving service (PoET id: %x, challenge: %x)",
			nb.state.PoetServiceId, poetChallenge)

		round, err := nb.poetProver.submit(*poetChallenge)
		if err != nil {
			return nil, fmt.Errorf("failed to submit challenge to poet service: %v", err)
		}

		nb.log.Info("challenge submitted to PoET proving service (PoET id: %x, round id: %v)",
			nb.state.PoetServiceId, round.Id)

		nipst.NipstChallenge = poetChallenge
		nb.state.PoetRound = round
		nb.state.persist()
	}

	// Phase 1: receive proofs from PoET service
	if nb.state.PoetProofRef == nil {
		proofRefChan := nb.poetDb.SubscribeToProofRef(nb.state.PoetServiceId, nb.state.PoetRound.Id)
		poetProofRef := <-proofRefChan // TODO(noamnelke): handle timeout

		membership, err := nb.poetDb.GetMembershipMap(poetProofRef)
		if err != nil {
			log.Panic("failed to fetch membership for PoET proof")              // TODO: handle inconsistent state
			return nil, fmt.Errorf("failed to fetch membership for PoET proof") // inconsistent state
		}
		if !membership[*nipst.NipstChallenge] {
			return nil, fmt.Errorf("not a member of this round (poetId: %x, roundId: %d)",
				nb.state.PoetServiceId, nb.state.PoetRound.Id) // TODO(noamnelke): handle this case!
		}
		nipst.PoetProofRef = poetProofRef
		nb.state.PoetProofRef = poetProofRef
		nb.state.persist()
	}

	// Phase 2: PoST execution.
	if nipst.PostProof == nil {
		nb.log.Info("starting PoST execution (challenge: %x)", nb.state.PoetProofRef)

		proof, err := nb.postProver.execute(nb.id, nb.state.PoetProofRef, defTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to execute PoST: %v", err)
		}

		res, err := nb.verifyPost(proof, nb.postCfg.SpacePerUnit, nb.postCfg.NumProvenLabels, nb.postCfg.Difficulty)
		if err != nil {
			return nil, fmt.Errorf("created an invalid PoST proof: %v", err)
		}
		if !res {
			return nil, fmt.Errorf("created an invalid PoST proof")
		}

		nb.log.Info("finished PoST execution (proof: %v)", proof)

		nipst.PostProof = proof
		nb.state.persist()
	}

	nb.log.Info("finished NIPST construction")

	nb.state = &builderState{
		nipst: initialNIPST(nb.postCfg.SpacePerUnit),
	}
	return nipst, nil
}

func (nb *NIPSTBuilder) IsPostInitialized() bool {
	readers, err := persistence.GetReaders(nb.postCfg.DataDir, nb.id)
	if err != nil {
		nb.log.WithFields(log.Err(err)).Error("failed to look for init files")
		return false
	}
	if len(readers) == 0 {
		nb.log.Info("could not find init files")
		return false
	}
	return true
}

func (nb *NIPSTBuilder) InitializePost() (*types.PostProof, error) {
	defTimeout := 5 * time.Second // TODO: replace temporary solution

	if nb.IsPostInitialized() {
		return nil, errors.New("PoST already initialized")
	}

	commitment, err := nb.postProver.initialize(nb.id, defTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize PoST: %v", err)
	}

	nb.log.Info("finished PoST initialization (commitment: %v), space: %v", commitment, nb.postCfg.FileSize)

	return commitment, nil
}

func NewNIPSTWithChallenge(challenge *common.Hash, poetRef []byte) *types.NIPST {
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
		PoetProofRef: poetRef,
	}
}
