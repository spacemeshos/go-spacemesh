package activation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/kvstore"
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
	state, err := kvstore.GetNIPostBuilderState(nb.db)
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
	if err := kvstore.AddNIPostBuilderState(nb.db, nb.state); err != nil {
		nb.log.With().Warning("cannot store nipost state", log.Err(err))
	}
}

// NIPostBuilder holds the required state and dependencies to create Non-Interactive Proofs of Space-Time (NIPost).
type NIPostBuilder struct {
	minerID           []byte
	db                *sql.Database
	postSetupProvider PostSetupProvider
	poetProvers       []PoetProvingServiceClient
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
	poetProvers []PoetProvingServiceClient,
	poetDB poetDbAPI,
	db *sql.Database,
	log log.Log,
) *NIPostBuilder {
	return &NIPostBuilder{
		minerID:           minerID.ToBytes(),
		postSetupProvider: postSetupProvider,
		poetProvers:       poetProvers,
		poetDB:            poetDB,
		state:             &types.NIPostBuilderState{NIPost: &types.NIPost{}},
		db:                db,
		log:               log,
	}
}

// updatePoETProver updates poetProver reference. It should not be executed concurrently with BuildNIPoST.
func (nb *NIPostBuilder) updatePoETProvers(poetProvers []PoetProvingServiceClient) {
	// reset the state for safety to avoid accidental erroneous wait in Phase 1.
	nb.state = &types.NIPostBuilderState{
		NIPost: &types.NIPost{},
	}
	nb.poetProvers = poetProvers
	nb.log.With().Info("updated poet proof service clients", log.Int("count", len(nb.poetProvers)))
}

// BuildNIPost uses the given challenge to build a NIPost.
// The process can take considerable time, because it includes waiting for the poet service to
// publish a proof - a process that takes about an epoch.
func (nb *NIPostBuilder) BuildNIPost(ctx context.Context, challenge *types.Hash32, commitmentAtx types.ATXID, poetProofDeadline time.Time) (*types.NIPost, time.Duration, error) {
	nb.load(*challenge)

	if s := nb.postSetupProvider.Status(); s.State != atypes.PostSetupStateComplete {
		return nil, 0, errors.New("post setup not complete")
	}

	nipost := nb.state.NIPost

	// Phase 0: Submit challenge to PoET services.
	if nb.state.PoetRequests == nil {
		poetRequests := nb.submitPoetChallenges(ctx, challenge)
		if len(poetRequests) == 0 {
			return nil, 0, &PoetSvcUnstableError{msg: "failed to submit challenge to any PoET"}
		}
		nipost.Challenge = challenge
		nb.state.Challenge = *challenge
		nb.state.PoetRequests = poetRequests
		nb.persist()
	}

	// Phase 1: receive proofs from PoET services
	if nb.state.PoetProofRef == nil {
		awaitProofsCtx, cancel := context.WithDeadline(ctx, poetProofDeadline)
		defer cancel()
		poetProofRef := nb.awaitPoetProof(awaitProofsCtx, challenge)
		if ctx.Err() != nil {
			return nil, 0, fmt.Errorf("failed to get poet proofs: %w", ErrStopRequested)
		}
		if poetProofRef == nil {
			// Haven't received any poet proof
			if awaitProofsCtx.Err() != nil {
				// Time is up - ATX challenge is expired.
				return nil, 0, fmt.Errorf("failed to get poet proofs: %w", ErrPoetProofDeadlineExpired)
			} else {
				// Time is not up - ATX challenge is NOT expired yet.
				return nil, 0, &PoetSvcUnstableError{msg: "haven't received any PoET proof"}
			}
		}
		nb.state.PoetProofRef = poetProofRef
		nb.persist()
	}

	// Phase 2: Post execution.
	var postGenDuration time.Duration = 0
	if nipost.Post == nil {
		nb.log.With().Info("starting post execution",
			log.Binary("challenge", nb.state.PoetProofRef))
		startTime := time.Now()
		proof, proofMetadata, err := nb.postSetupProvider.GenerateProof(nb.state.PoetProofRef, commitmentAtx)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to execute Post: %v", err)
		}

		postGenDuration = time.Since(startTime)
		nb.log.With().Info("finished post execution", log.Duration("duration", postGenDuration))

		nipost.Post = proof
		nipost.PostMetadata = proofMetadata

		nb.persist()
	}

	nb.log.Info("finished nipost construction")

	nb.state = &types.NIPostBuilderState{
		NIPost: &types.NIPost{},
	}
	nb.persist()
	return nipost, postGenDuration, nil
}

// Submit the challenge to a single PoET.
func submitPoetChallenge(ctx context.Context, logger log.Log, poet PoetProvingServiceClient, challenge *types.Hash32) (*types.PoetRequest, error) {
	poetServiceID, err := poet.PoetServiceID(ctx)
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to get PoET service ID", source: err}
	}

	logger.With().Debug("submitting challenge to poet proving service",
		log.String("poet_id", util.Bytes2Hex(poetServiceID)),
		log.Stringer("challenge", *challenge))

	round, err := poet.Submit(ctx, *challenge)
	if err != nil {
		logger.With().Error("failed to submit challenge to poet proving service",
			log.String("poet_id", util.Bytes2Hex(poetServiceID)),
			log.Stringer("challenge", *challenge),
			log.Err(err))
		return nil, &PoetSvcUnstableError{msg: "failed to submit challenge to poet service", source: err}
	}

	logger.With().Info("challenge submitted to poet proving service",
		log.String("poet_id", util.Bytes2Hex(poetServiceID)),
		log.String("round_id", round.ID),
		log.Stringer("challenge", *challenge))

	return &types.PoetRequest{
		PoetRound:     round,
		PoetServiceID: poetServiceID,
	}, nil
}

// Submit the challenge to all registered PoETs.
func (nb *NIPostBuilder) submitPoetChallenges(ctx context.Context, challenge *types.Hash32) []types.PoetRequest {
	g, ctx := errgroup.WithContext(ctx)
	poetRequestsChannel := make(chan types.PoetRequest, len(nb.poetProvers))
	for _, poetProver := range nb.poetProvers {
		poet := poetProver
		g.Go(func() error {
			if poetRequest, err := submitPoetChallenge(ctx, nb.log, poet, challenge); err == nil {
				poetRequestsChannel <- *poetRequest
			} else {
				nb.log.With().Warning("failed to submit challenge to PoET", log.Err(err))
			}
			return nil
		})
	}
	g.Wait()
	close(poetRequestsChannel)

	poetRequests := make([]types.PoetRequest, 0, len(nb.poetProvers))
	for request := range poetRequestsChannel {
		poetRequests = append(poetRequests, request)
	}
	return poetRequests
}

// awaitPoetProof concurrently waits for proofs from all PoET services that a challenge was submitted to
// until it gets proofs from all PoETs or the context is canceled.
// The best proof is returned or `nil` if proof was not received at all.
func (nb *NIPostBuilder) awaitPoetProof(ctx context.Context, challenge *types.Hash32) types.PoetProofRef {
	incomingPoetProofRefs := make(chan types.PoetProofRef, len(nb.state.PoetRequests))
	g, ctx := errgroup.WithContext(ctx)

	// Spawn workers to concurrently wait for PoET proofs
	for _, poetService := range nb.state.PoetRequests {
		svc := poetService
		g.Go(func() error {
			proofSubscription := nb.poetDB.SubscribeToProofRef(svc.PoetServiceID, svc.PoetRound.ID)
			defer nb.poetDB.UnsubscribeFromProofRef(svc.PoetServiceID, svc.PoetRound.ID)
			select {
			case ref := <-proofSubscription:
				nb.log.With().Debug("Worker got a new PoET proof",
					log.String("poet_id", util.Bytes2Hex(svc.PoetServiceID)),
					log.String("round_id", svc.PoetRound.ID),
					log.Binary("ref", ref),
				)
				// We are interested only in proofs that we are members of
				membership, err := nb.poetDB.GetMembershipMap(ref)
				if err != nil {
					nb.log.With().Panic("failed to fetch membership for poet proof",
						log.Binary("challenge", challenge[:]))
				}
				if membership[*challenge] {
					incomingPoetProofRefs <- ref
				}
			case <-ctx.Done():
			}
			return nil
		})
	}

	result := make(chan types.PoetProofRef)

	// Spawn consumer processing received proofs
	go func() {
		type poetProof struct {
			ref       types.PoetProofRef
			leafCount uint64
		}
		var bestProof *poetProof
		for ref := range incomingPoetProofRefs {
			proof, err := nb.poetDB.GetProof(ref)
			if err != nil {
				nb.log.Panic("Inconsistent state of poetDB. Received poetProofRef which doesn't exist in poetDB.")
			}
			nb.log.With().Info("Got a new PoET proof", log.Uint64("leafCount", proof.LeafCount), log.Binary("ref", ref))

			if bestProof == nil || bestProof.leafCount < proof.LeafCount {
				bestProof = &poetProof{
					ref:       ref,
					leafCount: proof.LeafCount,
				}
			}
		}
		// Send the best proof (if any) and close the channel.
		if bestProof != nil {
			nb.log.With().Debug("Selected the best PoET proof",
				log.Uint64("leafCount", bestProof.leafCount),
				log.Binary("ref", bestProof.ref))
			result <- bestProof.ref
		}
		close(result)
	}()

	// Close the workers -> consumer channel after all workers finished
	g.Wait()
	close(incomingPoetProofRefs)
	return <-result
}
