package activation

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"time"
)

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

	NIPoST *types.NIPoST

	// PoetRound is the round of the PoET proving service in which the PoET challenge was included in.
	PoetRound *types.PoetRound

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID []byte

	// PoetProofRef is the root of the proof received from the PoET service.
	PoetProofRef []byte
}

func nipostBuildStateKey() []byte {
	return []byte("nipstate")
}

func (nb *NIPoSTBuilder) load(challenge types.Hash32) {
	bts, err := nb.store.Get(nipostBuildStateKey())
	if err != nil {
		nb.log.Warning("cannot load NIPoST state %v", err)
		return
	}
	if len(bts) > 0 {
		var state builderState
		err = types.BytesToInterface(bts, &state)
		if err != nil {
			nb.log.Error("cannot load NIPoST state %v", err)
		}
		if state.Challenge == challenge {
			nb.state = &state
		} else {
			nb.state = &builderState{Challenge: challenge, NIPoST: &types.NIPoST{}}
		}
	}
}

func (nb *NIPoSTBuilder) persist() {
	bts, err := types.InterfaceToBytes(&nb.state)
	if err != nil {
		nb.log.Warning("cannot store NIPoST state %v", err)
		return
	}
	err = nb.store.Put(nipostBuildStateKey(), bts)
	if err != nil {
		nb.log.Warning("cannot store NIPoST state %v", err)
	}
}

// NIPoSTBuilder holds the required state and dependencies to create Non-Interactive Proofs of Space-Time (NIPoST).
type NIPoSTBuilder struct {
	minerID    []byte
	post       PostProvider
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

// NewNIPoSTBuilder returns a NIPoSTBuilder.
func NewNIPoSTBuilder(
	minerID []byte,
	post PostProvider,
	poetProver PoetProvingServiceClient,
	poetDB poetDbAPI,
	store bytesStore,
	log log.Log,
) *NIPoSTBuilder {
	return &NIPoSTBuilder{
		minerID:    minerID,
		post:       post,
		poetProver: poetProver,
		poetDB:     poetDB,
		errChan:    make(chan error),
		state:      &builderState{NIPoST: &types.NIPoST{}},
		store:      store,
		log:        log,
	}
}

// BuildNIPoST uses the given challenge to build a NIPoST. "atxExpired" and "stop" are channels for early termination of
// the building process. The process can take considerable time, because it includes waiting for the poet service to
// publish a proof - a process that takes about an epoch.
func (nb *NIPoSTBuilder) BuildNIPoST(challenge *types.Hash32, atxExpired, stop chan struct{}) (*types.NIPoST, error) {
	nb.load(*challenge)

	if _, ok := nb.post.InitCompleted(); !ok {
		return nil, errors.New("PoST init not completed")
	}

	nipost := nb.state.NIPoST

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

		nipost.Challenge = poetChallenge
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

		membership, err := nb.poetDB.GetMembershipMap(types.CalcHash32(poetProofRef).Bytes())
		if err != nil {
			log.Panic("failed to fetch membership for PoET proof")              // TODO: handle inconsistent state
			return nil, fmt.Errorf("failed to fetch membership for PoET proof") // inconsistent state
		}
		if !membership[*nipost.Challenge] {
			return nil, fmt.Errorf("not a member of this round (poetId: %x, roundId: %s, challenge: %x, num of members: %d)",
				nb.state.PoetServiceID, nb.state.PoetRound.ID, *nipost.Challenge, len(membership)) // TODO(noamnelke): handle this case!
		}
		nb.state.PoetProofRef = poetProofRef
		nb.persist()
	}

	// Phase 2: PoST execution.
	if nipost.PoST == nil {
		nb.log.With().Info("starting PoST execution",
			log.String("challenge", fmt.Sprintf("%x", nb.state.PoetProofRef)))
		startTime := time.Now()
		proof, proofMetadata, err := nb.post.GenerateProof(nb.state.PoetProofRef)
		if err != nil {
			return nil, fmt.Errorf("failed to execute PoST: %v", err)
		}

		nb.log.With().Info("finished PoST execution",
			log.String("duration", time.Now().Sub(startTime).String()))

		nipost.PoST = proof

		proofMetadata.ID = nil // ID doesn't need to be included when metadata is embedded inside ATX.
		nipost.PoSTMetadata = proofMetadata
		nb.persist()
	}

	nb.log.Info("finished NIPoST construction")

	nb.state = &builderState{
		NIPoST: &types.NIPoST{},
	}
	nb.persist()
	return nipost, nil
}

// NewNIPoSTWithChallenge is a convenience method FOR TESTS ONLY. TODO: move this out of production code.
func NewNIPoSTWithChallenge(challenge *types.Hash32, poetRef []byte) *types.NIPoST {
	return &types.NIPoST{
		Challenge: challenge,
		PoST: &types.PoST{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PoSTMetadata: &types.PoSTMetadata{
			Challenge: poetRef,
		},
	}
}
