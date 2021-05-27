package activation

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"time"
)

// PostProverClient provides proving functionality for PoST.
type PostProverClient interface {
	// Initialize is the process in which the prover commits to store some data, by having its storage filled with
	// pseudo-random data with respect to a specific id. This data is the result of a computationally-expensive
	// operation.
	Initialize() (commitment *types.PostProof, err error)

	// Execute is the phase in which the prover received a challenge, and proves that his data is still stored (or was
	// recomputed). This phase can be repeated arbitrarily many times without repeating initialization; thus despite the
	// initialization essentially serving as a proof-of-work, the amortized computational complexity can be made
	// arbitrarily small.
	Execute(challenge []byte) (proof *types.PostProof, err error)

	// Reset removes the initialization phase files.
	Reset() error

	// IsInitialized indicates whether the initialization phase has been completed. If it's not complete the remaining
	// bytes are also returned.
	IsInitialized() (initComplete bool, remainingBytes uint64, err error)

	// VerifyInitAllowed indicates whether the preconditions for starting
	// the initialization phase are met.
	VerifyInitAllowed() error

	// SetParams updates the datadir and space params in the client config, to be used in the initialization and the
	// execution phases. It overrides the config which the client was instantiated with.
	SetParams(datadir string, space uint64) error

	// SetLogger sets a logger for the client.
	SetLogger(logger shared.Logger)

	// Cfg returns the the client latest config.
	Cfg() *config.Config
}

// PoetProvingServiceClient provides a gateway to a trust-less public proving service, which may serve many PoET
// proving clients, and thus enormously reduce the cost-per-proof for PoET since each additional proof adds only
// a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// Submit registers a challenge in the proving service current open round.
	Submit(challenge types.Hash32) (*types.PoetRound, error)

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID() ([]byte, error)
}

type builderState struct {
	Challenge types.Hash32

	Nipst *types.NIPST

	// PoetRound is the round of the PoET proving service in which the PoET challenge was included in.
	PoetRound *types.PoetRound

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID []byte

	// PoetProofRef is the root of the proof received from the PoET service.
	PoetProofRef []byte
}

func nipstBuildStateKey() []byte {
	return []byte("nipstate")
}

func (nb *NIPSTBuilder) load(challenge types.Hash32) {
	bts, err := nb.store.Get(nipstBuildStateKey())
	if err != nil {
		nb.log.Warning("cannot load Nipst state %v", err)
		return
	}
	if len(bts) > 0 {
		var state builderState
		err = types.BytesToInterface(bts, &state)
		if err != nil {
			nb.log.Error("cannot load Nipst state %v", err)
		}
		if state.Challenge == challenge {
			nb.state = &state
		} else {
			nb.state = &builderState{Challenge: challenge, Nipst: &types.NIPST{}}
		}
	}
}

func (nb *NIPSTBuilder) persist() {
	bts, err := types.InterfaceToBytes(&nb.state)
	if err != nil {
		nb.log.Warning("cannot store Nipst state %v", err)
		return
	}
	err = nb.store.Put(nipstBuildStateKey(), bts)
	if err != nil {
		nb.log.Warning("cannot store Nipst state %v", err)
	}
}

// NIPSTBuilder holds the required state and dependencies to create Non-Interactive Proofs of Space-Time (NIPST).
type NIPSTBuilder struct {
	minerID    []byte
	postProver PostProverClient
	poetProver PoetProvingServiceClient
	poetDB     poetDbAPI
	errChan    chan error
	state      *builderState
	store      bytesStore
	log        log.Log
}

type poetDbAPI interface {
	SubscribeToProofRef(poetID []byte, roundID string) chan []byte
	GetMembershipMap(proofRef []byte) (map[types.Hash32]bool, error)
	UnsubscribeFromProofRef(poetID []byte, roundID string)
}

// NewNIPSTBuilder returns a NIPSTBuilder.
func NewNIPSTBuilder(
	minerID []byte,
	postProver PostProverClient,
	poetProver PoetProvingServiceClient,
	poetDB poetDbAPI,
	store bytesStore,
	log log.Log,
) *NIPSTBuilder {
	return &NIPSTBuilder{
		minerID:    minerID,
		postProver: postProver,
		poetProver: poetProver,
		poetDB:     poetDB,
		errChan:    make(chan error),
		state:      &builderState{Nipst: &types.NIPST{}},
		store:      store,
		log:        log,
	}
}

// BuildNIPST uses the given challenge to build a NIPST. "atxExpired" and "stop" are channels for early termination of
// the building process. The process can take considerable time, because it includes waiting for the poet service to
// publish a proof - a process that takes about an epoch.
func (nb *NIPSTBuilder) BuildNIPST(challenge *types.Hash32, atxExpired, stop chan struct{}) (*types.NIPST, error) {
	nb.load(*challenge)

	if initialized, _, err := nb.postProver.IsInitialized(); !initialized || err != nil {
		return nil, errors.New("PoST not initialized")
	}
	nipst := nb.state.Nipst

	// Phase 0: Submit challenge to PoET service.
	if nb.state.PoetRound == nil {
		poetServiceID, err := nb.poetProver.PoetServiceID()
		if err != nil {
			return nil, fmt.Errorf("failed to get PoET service ID: %v", err)
		}
		nb.state.PoetServiceID = poetServiceID

		poetChallenge := challenge
		nb.state.Challenge = *challenge

		nb.log.Debug("submitting challenge to PoET proving service (PoET id: %x, challenge: %x)",
			nb.state.PoetServiceID, poetChallenge)

		round, err := nb.poetProver.Submit(*poetChallenge)
		if err != nil {
			return nil, fmt.Errorf("failed to submit challenge to poet service: %v", err)
		}

		nb.log.Info("challenge submitted to PoET proving service (PoET id: %x, round id: %v, challenge: %x)",
			nb.state.PoetServiceID, round.ID, poetChallenge)

		nipst.NipstChallenge = poetChallenge
		nb.state.PoetRound = round
		nb.persist()
	}

	// Phase 1: receive proofs from PoET service
	if nb.state.PoetProofRef == nil {
		var poetProofRef []byte
		select {
		case poetProofRef = <-nb.poetDB.SubscribeToProofRef(nb.state.PoetServiceID, nb.state.PoetRound.ID):
		case <-atxExpired:
			nb.poetDB.UnsubscribeFromProofRef(nb.state.PoetServiceID, nb.state.PoetRound.ID)
			return nil, fmt.Errorf("atx expired while waiting for poet proof, target epoch ended")
		case <-stop:
			return nil, StopRequestedError{}
		}

		membership, err := nb.poetDB.GetMembershipMap(poetProofRef)
		if err != nil {
			log.Panic("failed to fetch membership for PoET proof")              // TODO: handle inconsistent state
			return nil, fmt.Errorf("failed to fetch membership for PoET proof") // inconsistent state
		}
		if !membership[*nipst.NipstChallenge] {
			return nil, fmt.Errorf("not a member of this round (poetId: %x, roundId: %s, challenge: %x, num of members: %d)",
				nb.state.PoetServiceID, nb.state.PoetRound.ID, *nipst.NipstChallenge, len(membership)) // TODO(noamnelke): handle this case!
		}
		nb.state.PoetProofRef = poetProofRef
		nb.persist()
	}

	// Phase 2: PoST execution.
	if nipst.PostProof == nil {
		nb.log.With().Info("starting PoST execution",
			log.String("challenge", fmt.Sprintf("%x", nb.state.PoetProofRef)))
		startTime := time.Now()
		proof, err := nb.postProver.Execute(nb.state.PoetProofRef)
		if err != nil {
			return nil, fmt.Errorf("failed to execute PoST: %v", err)
		}

		nb.log.With().Info("finished PoST execution",
			log.String("proof_merkle_root", fmt.Sprintf("%x", proof.MerkleRoot)),
			log.String("duration", time.Now().Sub(startTime).String()))

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

// NewNIPSTWithChallenge is a convenience method FOR TESTS ONLY. TODO: move this out of production code.
func NewNIPSTWithChallenge(challenge *types.Hash32, poetRef []byte) *types.NIPST {
	return &types.NIPST{
		NipstChallenge: challenge,
		PostProof: &types.PostProof{
			Challenge:    poetRef,
			MerkleRoot:   []byte(nil),
			ProofNodes:   [][]byte(nil),
			ProvenLeaves: [][]byte(nil),
		},
	}
}
