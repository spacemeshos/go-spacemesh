package activation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate mockgen -package=activation -destination=./poet_client_mock_test.go -source=./nipost.go PoetProvingServiceClient

// PoetProvingServiceClient provides a gateway to a trust-less public proving service, which may serve many PoET
// proving clients, and thus enormously reduce the cost-per-proof for PoET since each additional proof adds only
// a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// Submit registers a challenge in the proving service current open round.
	Submit(ctx context.Context, challenge types.Hash32) (*types.PoetRound, error)

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID(context.Context) ([]byte, error)
}

type builderState struct {
	Challenge types.Hash32

	NIPost *types.NIPost

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

func (nb *NIPostBuilder) load(challenge types.Hash32) {
	bts, err := nb.store.Get(nipostBuildStateKey())
	if err != nil {
		nb.log.Warning("cannot load NIPost state %v", err)
		return
	}
	if len(bts) > 0 {
		var state builderState
		err = types.BytesToInterface(bts, &state)
		if err != nil {
			nb.log.Error("cannot load NIPost state %v", err)
		}
		if state.Challenge == challenge {
			nb.state = &state
		} else {
			nb.state = &builderState{Challenge: challenge, NIPost: &types.NIPost{}}
		}
	}
}

func (nb *NIPostBuilder) persist() {
	bts, err := types.InterfaceToBytes(&nb.state)
	if err != nil {
		nb.log.Warning("cannot store NIPost state %v", err)
		return
	}
	err = nb.store.Put(nipostBuildStateKey(), bts)
	if err != nil {
		nb.log.Warning("cannot store NIPost state %v", err)
	}
}

// NIPostBuilder holds the required state and dependencies to create Non-Interactive Proofs of Space-Time (NIPost).
type NIPostBuilder struct {
	minerID           []byte
	postSetupProvider PostSetupProvider
	poetProver        PoetProvingServiceClient
	poetDB            poetDbAPI
	state             *builderState
	store             bytesStore
	log               log.Log
}

type poetDbAPI interface {
	SubscribeToProofRef(poetID []byte, roundID string) chan []byte
	GetMembershipMap(proofRef []byte) (map[types.Hash32]bool, error)
	UnsubscribeFromProofRef(poetID []byte, roundID string)
}

// NewNIPostBuilder returns a NIPostBuilder.
func NewNIPostBuilder(
	minerID []byte,
	postSetupProvider PostSetupProvider,
	poetProver PoetProvingServiceClient,
	poetDB poetDbAPI,
	store bytesStore,
	log log.Log,
) *NIPostBuilder {
	return &NIPostBuilder{
		minerID:           minerID,
		postSetupProvider: postSetupProvider,
		poetProver:        poetProver,
		poetDB:            poetDB,
		state:             &builderState{NIPost: &types.NIPost{}},
		store:             store,
		log:               log,
	}
}

// updatePoETProver updates poetProver reference. It should not be executed concurently with BuildNIPST.
func (nb *NIPostBuilder) updatePoETProver(poetProver PoetProvingServiceClient) {
	// reset the state for safety to avoid accidental erroneous wait in Phase 1.
	nb.state = &builderState{
		NIPost: &types.NIPost{},
	}
	nb.poetProver = poetProver
}

// BuildNIPost uses the given challenge to build a NIPost. "atxExpired" and "stop" are channels for early termination of
// the building process. The process can take considerable time, because it includes waiting for the poet service to
// publish a proof - a process that takes about an epoch.
func (nb *NIPostBuilder) BuildNIPost(ctx context.Context, challenge *types.Hash32, atxExpired chan struct{}) (*types.NIPost, error) {
	nb.load(*challenge)

	if s := nb.postSetupProvider.Status(); s.State != postSetupStateComplete {
		return nil, errors.New("post setup not complete")
	}

	nipost := nb.state.NIPost

	// Phase 0: Submit challenge to PoET service.
	if nb.state.PoetRound == nil {
		poetServiceID, err := nb.poetProver.PoetServiceID(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to get PoET service ID: %v", ErrPoetServiceUnstable, err)
		}
		nb.state.PoetServiceID = poetServiceID

		poetChallenge := challenge
		nb.state.Challenge = *challenge

		nb.log.Debug("submitting challenge to PoET proving service (PoET id: %x, challenge: %x)",
			nb.state.PoetServiceID, poetChallenge)

		round, err := nb.poetProver.Submit(ctx, *poetChallenge)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to submit challenge to poet service: %v", ErrPoetServiceUnstable, err)
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
			return nil, fmt.Errorf("%w: while waiting for poet proof, target epoch ended", ErrATXChallengeExpired)
		case <-ctx.Done():
			return nil, ErrStopRequested
		}

		membership, err := nb.poetDB.GetMembershipMap(poetProofRef)
		if err != nil {
			nb.log.With().Panic("failed to fetch membership for PoET proof",
				log.String("challenge", fmt.Sprintf("%x", nb.state.PoetProofRef))) // TODO: handle inconsistent state
		}
		if !membership[*nipost.Challenge] {
			round := nb.state.PoetRound
			nb.state.PoetRound = nil // no point in waiting in Phase 1 since we are already received a proof
			return nil, fmt.Errorf("%w not a member of this round (poetId: %x, roundId: %s, challenge: %x, num of members: %d)", ErrPoetServiceUnstable,
				nb.state.PoetServiceID, round.ID, *nipost.Challenge, len(membership)) // TODO(noamnelke): handle this case!
		}
		nb.state.PoetProofRef = poetProofRef
		nb.persist()
	}

	// Phase 2: Post execution.
	if nipost.Post == nil {
		nb.log.With().Info("starting Post execution",
			log.String("challenge", fmt.Sprintf("%x", nb.state.PoetProofRef)))
		startTime := time.Now()
		proof, proofMetadata, err := nb.postSetupProvider.GenerateProof(nb.state.PoetProofRef)
		if err != nil {
			return nil, fmt.Errorf("failed to execute Post: %v", err)
		}

		nb.log.With().Info("finished Post execution",
			log.Duration("duration", time.Now().Sub(startTime)))

		nipost.Post = proof
		nipost.PostMetadata = proofMetadata

		nb.persist()
	}

	nb.log.Info("finished NIPost construction")

	nb.state = &builderState{
		NIPost: &types.NIPost{},
	}
	nb.persist()
	return nipost, nil
}

// NewNIPostWithChallenge is a convenience method FOR TESTS ONLY. TODO: move this out of production code.
func NewNIPostWithChallenge(challenge *types.Hash32, poetRef []byte) *types.NIPost {
	return &types.NIPost{
		Challenge: challenge,
		Post: &types.Post{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PostMetadata: &types.PostMetadata{
			Challenge: poetRef,
		},
	}
}
