package activation

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"sync"
)

// PostProverClient provides proving functionality for PoST.
type PostProverClient interface {
	// initialize is the process in which the prover commits
	// to store some data, by having its storage being filled with
	// pseudo-random data with respect to a specific id.
	// This data is the result of a computationally-expensive operation.
	Initialize() (commitment *types.PostProof, err error)

	// Execute is the phase in which the prover received a challenge,
	// and proves that his data is still stored (or was recomputed).
	// This phase can be repeated arbitrarily many times without repeating initialization;
	// thus despite the initialization essentially serving as a proof-of-work,
	// the amortized computational complexity can be made arbitrarily small.
	Execute(challenge []byte) (proof *types.PostProof, err error)

	// Reset removes the initialization phase files.
	Reset() error

	// IsInitialized indicates whether the initialization phase has been completed.
	IsInitialized() (bool, error)

	// VerifyInitAllowed indicates whether the preconditions for starting
	// the initialization phase are met.
	VerifyInitAllowed() error

	// SetParams updates the datadir and space params in the client config, to be used in the initialization
	// and the execution phases. It overrides the config which the client was instantiated with.
	SetParams(datadir string, space uint64) error

	// SetLogger sets a logger for the client.
	SetLogger(logger shared.Logger)

	// Cfg returns the the client latest config.
	Cfg() *config.Config
}

// PoetProvingServiceClient provides a gateway to a trust-less public proving
// service, which may serve many PoET proving clients, and thus enormously
// reduce the cost-per-proof for PoET since each additional proof adds
// only a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// submit registers a challenge in the proving service
	// open round suited for the specified duration.
	submit(challenge types.Hash32) (*types.PoetRound, error)

	getPoetServiceId() ([]byte, error)
}

type builderState struct {
	Challenge types.Hash32

	Nipst *types.NIPST

	// PoetRound is the round of the PoET proving service
	// in which the PoET challenge was included in.
	PoetRound *types.PoetRound

	// PoetServiceId is the public key of the PoET proving service.
	PoetServiceId []byte

	// PoetProofRef is the root of the proof received from the PoET service.
	PoetProofRef []byte
}

func nipstBuildStateKey() []byte {
	return []byte("nipstate")
}

func (s *NIPSTBuilder) load(challenge types.Hash32) {
	bts, err := s.store.Get(nipstBuildStateKey())
	if err != nil {
		s.log.Warning("cannot load Nipst state %v", err)
		return
	}
	if len(bts) > 0 {
		var state builderState
		err = types.BytesToInterface(bts, &state)
		if err != nil {
			s.log.Error("cannot load Nipst state %v", err)
		}
		if state.Challenge == challenge {
			s.state = &state
		} else {
			s.state = &builderState{Challenge: challenge, Nipst: &types.NIPST{}}
		}
	}
}

func (s *NIPSTBuilder) persist() {
	bts, err := types.InterfaceToBytes(&s.state)
	if err != nil {
		s.log.Warning("cannot store Nipst state %v", err)
		return
	}
	err = s.store.Put(nipstBuildStateKey(), bts)
	if err != nil {
		s.log.Warning("cannot store Nipst state %v", err)
	}
}

type NIPSTBuilder struct {
	id         []byte
	postProver PostProverClient
	poetProver PoetProvingServiceClient
	poetDb     PoetDbApi

	stop    bool
	stopM   sync.Mutex
	errChan chan error
	state   *builderState
	store   BytesStore
	log     log.Log
}

type PoetDbApi interface {
	SubscribeToProofRef(poetId []byte, roundId string) chan []byte
	GetMembershipMap(proofRef []byte) (map[types.Hash32]bool, error)
}

func NewNIPSTBuilder(id []byte, postProver PostProverClient, poetProver PoetProvingServiceClient, poetDb PoetDbApi, store BytesStore, log log.Log) *NIPSTBuilder {
	return newNIPSTBuilder(
		id,
		postProver,
		poetProver,
		poetDb,
		store,
		log,
	)
}

func newNIPSTBuilder(
	id []byte,
	postProver PostProverClient,
	poetProver PoetProvingServiceClient,
	poetDb PoetDbApi,
	store BytesStore,
	log log.Log,
) *NIPSTBuilder {
	return &NIPSTBuilder{
		id:         id,
		postProver: postProver,
		poetProver: poetProver,
		poetDb:     poetDb,
		stop:       false,
		errChan:    make(chan error),
		store:      store,
		log:        log,
		state: &builderState{
			Nipst: &types.NIPST{},
		},
	}
}

func (nb *NIPSTBuilder) BuildNIPST(challenge *types.Hash32) (*types.NIPST, error) {
	nb.load(*challenge)

	if initialized, err := nb.postProver.IsInitialized(); !initialized || err != nil {
		return nil, errors.New("PoST not initialized")
	}

	cfg := nb.postProver.Cfg()
	nipst := nb.state.Nipst

	nipst.Space = cfg.SpacePerUnit

	// Phase 0: Submit challenge to PoET service.
	if nb.state.PoetRound == nil {
		poetServiceId, err := nb.poetProver.getPoetServiceId()
		if err != nil {
			return nil, fmt.Errorf("failed to get PoET service ID: %v", err)
		}
		nb.state.PoetServiceId = poetServiceId

		poetChallenge := challenge
		nb.state.Challenge = *challenge

		nb.log.Debug("submitting challenge to PoET proving service (PoET id: %x, challenge: %x)",
			nb.state.PoetServiceId, poetChallenge)

		round, err := nb.poetProver.submit(*poetChallenge)
		if err != nil {
			return nil, fmt.Errorf("failed to submit challenge to poet service: %v", err)
		}

		nb.log.Info("challenge submitted to PoET proving service (PoET id: %x, round id: %v, challenge: %x)",
			nb.state.PoetServiceId, round.Id, poetChallenge)

		nipst.NipstChallenge = poetChallenge
		nb.state.PoetRound = round
		nb.persist()
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
			return nil, fmt.Errorf("not a member of this round (poetId: %x, roundId: %s, challenge: %x, num of members: %d)",
				nb.state.PoetServiceId, nb.state.PoetRound.Id, *nipst.NipstChallenge, len(membership)) // TODO(noamnelke): handle this case!
		}
		nb.state.PoetProofRef = poetProofRef
		nb.persist()
	}

	// Phase 2: PoST execution.
	if nipst.PostProof == nil {
		nb.log.Info("starting PoST execution (challenge: %x)", nb.state.PoetProofRef)

		proof, err := nb.postProver.Execute(nb.state.PoetProofRef)
		if err != nil {
			return nil, fmt.Errorf("failed to execute PoST: %v", err)
		}
		b,_ := types.InterfaceToBytes(proof)
		nb.log.With().Info("finished PoST execution",
			log.String("proof merkle root", fmt.Sprintf("%x", proof.MerkleRoot)), log.Int("post_size", len(b)))

		nipst.PostProof = proof
		nb.persist()
	}

	nb.log.Info("finished NIPST construction")

	nb.state = &builderState{
		Nipst: &types.NIPST{},
	}
	nb.persist()
	return nipst, nil
}

func NewNIPSTWithChallenge(challenge *types.Hash32, poetRef []byte) *types.NIPST {
	return &types.NIPST{
		Space:          0,
		NipstChallenge: challenge,
		PostProof: &types.PostProof{
			Challenge:    poetRef,
			MerkleRoot:   []byte(nil),
			ProofNodes:   [][]byte(nil),
			ProvenLeaves: [][]byte(nil),
		},
	}
}
