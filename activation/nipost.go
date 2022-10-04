package activation

import (
	"context"
	"errors"
	"fmt"
	"time"

	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/store"
)

//go:generate mockgen -package=mocks -destination=./mocks/nipost.go -source=./nipost.go PoetProvingServiceClient

// PoetProvingServiceClient provides a gateway to a trust-less public proving service, which may serve many PoET
// proving clients, and thus enormously reduce the cost-per-proof for PoET since each additional proof adds only
// a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	// Submit registers a challenge in the proving service current open round.
	Submit(ctx context.Context, challenge types.Hash32) (*types.PoetRound, error)

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID(context.Context) ([]byte, error)
}

func (nb *NIPostBuilder) load(challenge types.Hash32) {
	state, err := store.GetNIPostBuilderState(nb.db)
	if err != nil {
		nb.log.With().Warning("cannot load nipost state", log.Err(err))
		return
	}
	if state.Challenge == challenge {
		nb.state = state
	} else {
		nb.state = &types.NIPostBuilderState{Challenge: challenge, NIPost: &types.NIPost{}}
	}
}

func (nb *NIPostBuilder) persist() {
	if err := store.AddNIPostBuilderState(nb.db, nb.state); err != nil {
		nb.log.With().Warning("cannot store nipost state", log.Err(err))
	}
}

// NIPostBuilder holds the required state and dependencies to create Non-Interactive Proofs of Space-Time (NIPost).
type NIPostBuilder struct {
	minerID           []byte
	db                *sql.Database
	postSetupProvider PostSetupProvider
	poetProver        PoetProvingServiceClient
	poetDB            poetDbAPI
	state             *types.NIPostBuilderState
	log               log.Log
}

type poetDbAPI interface {
	SubscribeToProofRef(poetID []byte, roundID string) chan types.PoetProofRef
	GetMembershipMap(proofRef types.PoetProofRef) (map[types.Hash32]bool, error)
	GetProof(types.PoetProofRef) (*types.PoetProof, error)
	UnsubscribeFromProofRef(poetID []byte, roundID string)
}

// NewNIPostBuilder returns a NIPostBuilder.
func NewNIPostBuilder(
	minerID types.NodeID,
	postSetupProvider PostSetupProvider,
	poetProver PoetProvingServiceClient,
	poetDB poetDbAPI,
	db *sql.Database,
	log log.Log,
) *NIPostBuilder {
	return &NIPostBuilder{
		minerID:           minerID.ToBytes(),
		postSetupProvider: postSetupProvider,
		poetProver:        poetProver,
		poetDB:            poetDB,
		state:             &types.NIPostBuilderState{NIPost: &types.NIPost{}},
		db:                db,
		log:               log,
	}
}

// updatePoETProver updates poetProver reference. It should not be executed concurrently with BuildNIPoST.
func (nb *NIPostBuilder) updatePoETProver(poetProver PoetProvingServiceClient) {
	// reset the state for safety to avoid accidental erroneous wait in Phase 1.
	nb.state = &types.NIPostBuilderState{
		NIPost: &types.NIPost{},
	}
	nb.poetProver = poetProver
	nb.log.With().Info("updated poet proof service client")
}

// BuildNIPost uses the given challenge to build a NIPost. "atxExpired" and "stop" are channels for early termination of
// the building process. The process can take considerable time, because it includes waiting for the poet service to
// publish a proof - a process that takes about an epoch.
func (nb *NIPostBuilder) BuildNIPost(ctx context.Context, challenge *types.Hash32, commitmentAtx types.ATXID, atxExpired chan struct{}) (*types.NIPost, error) {
	nb.load(*challenge)

	if s := nb.postSetupProvider.Status(); s.State != atypes.PostSetupStateComplete {
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

		nb.log.With().Debug("submitting challenge to poet proving service",
			log.String("poet_id", util.Bytes2Hex(nb.state.PoetServiceID)),
			log.Stringer("challenge", poetChallenge))

		round, err := nb.poetProver.Submit(ctx, *poetChallenge)
		if err != nil {
			nb.log.With().Error("failed to submit challenge to poet proving service",
				log.String("poet_id", util.Bytes2Hex(nb.state.PoetServiceID)),
				log.Stringer("challenge", poetChallenge),
				log.Err(err))
			return nil, fmt.Errorf("%w: failed to submit challenge to poet service: %v", ErrPoetServiceUnstable, err)
		}

		nb.log.With().Info("challenge submitted to poet proving service",
			log.String("poet_id", util.Bytes2Hex(nb.state.PoetServiceID)),
			log.String("round_id", round.ID),
			log.Stringer("challenge", poetChallenge))

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
			nb.log.With().Panic("failed to fetch membership for poet proof",
				log.Binary("challenge", nb.state.PoetProofRef)) // TODO: handle inconsistent state
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
		nb.log.With().Info("starting post execution",
			log.Binary("challenge", nb.state.PoetProofRef))
		startTime := time.Now()
		proof, proofMetadata, err := nb.postSetupProvider.GenerateProof(nb.state.PoetProofRef, commitmentAtx)
		if err != nil {
			return nil, fmt.Errorf("failed to execute Post: %v", err)
		}

		nb.log.With().Info("finished post execution",
			log.Duration("duration", time.Since(startTime)))

		nipost.Post = proof
		nipost.PostMetadata = proofMetadata

		nb.persist()
	}

	nb.log.Info("finished nipost construction")

	nb.state = &types.NIPostBuilderState{
		NIPost: &types.NIPost{},
	}
	nb.persist()
	return nipost, nil
}
