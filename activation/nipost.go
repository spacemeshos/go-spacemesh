package activation

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/poet/shared"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/kvstore"
)

//go:generate mockgen -package=activation -destination=./nipost_mocks.go -source=./nipost.go PoetProvingServiceClient

// PoetProvingServiceClient provides a gateway to a trust-less public proving service, which may serve many PoET
// proving clients, and thus enormously reduce the cost-per-proof for PoET since each additional proof adds only
// a small number of hash evaluations to the total cost.
type PoetProvingServiceClient interface {
	PowParams(ctx context.Context) (*PoetPowParams, error)
	// Submit registers a challenge in the proving service current open round.
	Submit(ctx context.Context, prefix, challenge []byte, signature types.EdSignature, nodeID types.NodeID, pow PoetPoW) (*types.PoetRound, error)

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID(context.Context) (types.PoetServiceID, error)

	// Proof returns the proof for the given round ID.
	Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, error)
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
	nodeID            types.NodeID
	db                *sql.Database
	postSetupProvider postSetupProvider
	poetProvers       []PoetProvingServiceClient
	poetDB            poetDbAPI
	state             *types.NIPostBuilderState
	log               log.Log
	signer            *signing.EdSigner
	layerClock        layerClock
	poetCfg           PoetConfig
}

type poetDbAPI interface {
	GetProof(types.PoetProofRef) (*types.PoetProof, error)
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

// NewNIPostBuilder returns a NIPostBuilder.
func NewNIPostBuilder(
	nodeID types.NodeID,
	postSetupProvider postSetupProvider,
	poetProvers []PoetProvingServiceClient,
	poetDB poetDbAPI,
	db *sql.Database,
	log log.Log,
	signer *signing.EdSigner,
	poetCfg PoetConfig,
	layerClock layerClock,
) *NIPostBuilder {
	return &NIPostBuilder{
		nodeID:            nodeID,
		postSetupProvider: postSetupProvider,
		poetProvers:       poetProvers,
		poetDB:            poetDB,
		state:             &types.NIPostBuilderState{NIPost: &types.NIPost{}},
		db:                db,
		log:               log,
		signer:            signer,
		poetCfg:           poetCfg,
		layerClock:        layerClock,
	}
}

// UpdatePoETProvers updates poetProver reference. It should not be executed concurrently with BuildNIPoST.
func (nb *NIPostBuilder) UpdatePoETProvers(poetProvers []PoetProvingServiceClient) {
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
func (nb *NIPostBuilder) BuildNIPost(ctx context.Context, challenge *types.NIPostChallenge) (*types.NIPost, time.Duration, error) {
	logger := nb.log.WithContext(ctx)
	// Calculate deadline for waiting for poet proofs.
	// Deadline must fit between:
	// - the end of the current poet round
	// - the start of the next one.
	// It must also accommodate for PoST duration.
	//
	//                                 PoST
	//         ┌─────────────────────┐  ┌┐┌─────────────────────┐
	//         │     POET ROUND      │  │││   NEXT POET ROUND   │
	// ┌────▲──┴──────────────────┬──┴─▲┴┴┴─────────────────▲┬──┴───► time
	// │    │      EPOCH          │    │       EPOCH        ││
	// └────┼─────────────────────┴────┼────────────────────┼┴──────
	//      │                          │                    │
	//  WE ARE HERE                DEADLINE FOR       ATX PUBLICATION
	//                           WAITING FOR POET        DEADLINE
	//                               PROOFS

	pubEpoch := challenge.PublishEpoch
	poetRoundStart := nb.layerClock.LayerToTime((pubEpoch - 1).FirstLayer()).Add(nb.poetCfg.PhaseShift)
	nextPoetRoundStart := nb.layerClock.LayerToTime(pubEpoch.FirstLayer()).Add(nb.poetCfg.PhaseShift)
	poetRoundEnd := nextPoetRoundStart.Add(-nb.poetCfg.CycleGap)
	poetProofDeadline := poetRoundEnd.Add(nb.poetCfg.GracePeriod)

	logger.With().Info("building NIPost",
		log.Time("poet round start", poetRoundStart),
		log.Time("poet round end", poetRoundEnd),
		log.Time("next poet round start", nextPoetRoundStart),
		log.Time("poet proof deadline", poetProofDeadline),
		log.FieldNamed("publish epoch", pubEpoch),
		log.FieldNamed("target epoch", challenge.TargetEpoch()),
	)

	challengeHash := challenge.Hash()
	nb.load(challengeHash)

	if s := nb.postSetupProvider.Status(); s.State != PostSetupStateComplete {
		return nil, 0, errors.New("post setup not complete")
	}

	nipost := nb.state.NIPost

	// Phase 0: Submit challenge to PoET services.
	if len(nb.state.PoetRequests) == 0 {
		now := time.Now()
		if poetRoundStart.Before(now) {
			return nil, 0, fmt.Errorf("%w: poet round has already started at %s (now: %s)", ErrATXChallengeExpired, poetRoundStart, now)
		}

		signature := nb.signer.Sign(signing.POET, challengeHash.Bytes())
		prefix := bytes.Join([][]byte{nb.signer.Prefix(), {byte(signing.POET)}}, nil)
		submitCtx, cancel := context.WithDeadline(ctx, poetRoundStart)
		defer cancel()
		poetRequests := nb.submitPoetChallenges(submitCtx, prefix, challengeHash.Bytes(), signature, nb.signer.NodeID())
		if len(poetRequests) == 0 {
			return nil, 0, &PoetSvcUnstableError{msg: "failed to submit challenge to any PoET", source: ctx.Err()}
		}

		nipost.Challenge = &challengeHash
		nb.state.Challenge = challengeHash
		nb.state.PoetRequests = poetRequests
		nb.persist()
		if err := ctx.Err(); err != nil {
			return nil, 0, fmt.Errorf("submitting challenges: %w", err)
		}
	}

	// Phase 1: query PoET services for proofs
	if nb.state.PoetProofRef == types.EmptyPoetProofRef {
		getProofsCtx, cancel := context.WithDeadline(ctx, poetProofDeadline)
		defer cancel()
		poetProofRef, err := nb.getBestProof(getProofsCtx, &nb.state.Challenge)
		if err != nil {
			return nil, 0, &PoetSvcUnstableError{msg: "getBestProof failed", source: err}
		}
		if poetProofRef == types.EmptyPoetProofRef {
			return nil, 0, &PoetSvcUnstableError{source: ErrPoetProofNotReceived}
		}
		nb.state.PoetProofRef = poetProofRef
		nb.persist()
	}

	// Phase 2: Post execution.
	var postGenDuration time.Duration = 0
	if nipost.Post == nil {
		nb.log.With().Info("starting post execution", log.Binary("challenge", nb.state.PoetProofRef[:]))
		startTime := time.Now()
		proof, proofMetadata, err := nb.postSetupProvider.GenerateProof(ctx, nb.state.PoetProofRef[:])
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
func (nb *NIPostBuilder) submitPoetChallenge(ctx context.Context, poet PoetProvingServiceClient, prefix, challenge []byte, signature types.EdSignature, nodeID types.NodeID) (*types.PoetRequest, error) {
	poetServiceID, err := poet.PoetServiceID(ctx)
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to get PoET service ID", source: err}
	}
	logger := nb.log.WithContext(ctx).WithFields(log.String("poet_id", hex.EncodeToString(poetServiceID.ServiceID)))

	logger.Debug("querying for poet PoW parameters")
	powParams, err := poet.PowParams(ctx)
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to get PoW params", source: err}
	}

	logger.Debug("doing Pow with params: %v", powParams)
	startTime := time.Now()
	nonce, err := shared.FindSubmitPowNonce(ctx, powParams.Challenge, challenge, nodeID.Bytes(), powParams.Difficulty)
	metrics.PoetPowDuration.Set(float64(time.Since(startTime).Nanoseconds()))
	if err != nil {
		return nil, fmt.Errorf("running poet PoW: %w", err)
	}

	logger.Debug("submitting challenge to poet proving service")

	round, err := poet.Submit(ctx, prefix, challenge, signature, nodeID, PoetPoW{
		Nonce:  nonce,
		Params: *powParams,
	})
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to submit challenge to poet service", source: err}
	}

	logger.With().Info("challenge submitted to poet proving service", log.String("round", round.ID))

	return &types.PoetRequest{
		PoetRound:     round,
		PoetServiceID: poetServiceID,
	}, nil
}

// Submit the challenge to all registered PoETs.
func (nb *NIPostBuilder) submitPoetChallenges(ctx context.Context, prefix, challenge []byte, signature types.EdSignature, nodeID types.NodeID) []types.PoetRequest {
	g, ctx := errgroup.WithContext(ctx)
	poetRequestsChannel := make(chan types.PoetRequest, len(nb.poetProvers))
	for _, poetProver := range nb.poetProvers {
		poet := poetProver
		g.Go(func() error {
			if poetRequest, err := nb.submitPoetChallenge(ctx, poet, prefix, challenge, signature, nodeID); err == nil {
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

func (nb *NIPostBuilder) getPoetClient(ctx context.Context, id types.PoetServiceID) PoetProvingServiceClient {
	for _, client := range nb.poetProvers {
		if clientId, err := client.PoetServiceID(ctx); err == nil && bytes.Equal(id.ServiceID, clientId.ServiceID) {
			return client
		}
	}
	return nil
}

func membersContain(members []types.Member, challenge *types.Hash32) bool {
	for _, member := range members {
		if bytes.Equal(member[:], challenge.Bytes()) {
			return true
		}
	}
	return false
}

func (nb *NIPostBuilder) getBestProof(ctx context.Context, challenge *types.Hash32) (types.PoetProofRef, error) {
	proofs := make(chan *types.PoetProofMessage, len(nb.state.PoetRequests))

	var eg errgroup.Group
	for _, r := range nb.state.PoetRequests {
		logger := nb.log.WithContext(ctx).WithFields(log.String("poet_id", hex.EncodeToString(r.PoetServiceID.ServiceID)), log.String("round", r.PoetRound.ID))
		client := nb.getPoetClient(ctx, r.PoetServiceID)
		if client == nil {
			logger.Warning("Poet client not found")
			continue
		}
		round := r.PoetRound.ID
		// Time to wait before querying for the proof
		// The additional second is an optimization to be nicer to poet
		// and don't accidentally ask it to soon and have to retry.
		waitTime := time.Until(r.PoetRound.End.IntoTime()) + time.Second
		eg.Go(func() error {
			logger.With().Info("Waiting till poet round end", log.Duration("wait time", waitTime))
			select {
			case <-ctx.Done():
				return fmt.Errorf("waiting to query proof: %w", ctx.Err())
			case <-time.After(waitTime):
			}

			proof, err := client.Proof(ctx, round)
			switch {
			case errors.Is(err, context.Canceled):
				return fmt.Errorf("querying proof: %w", ctx.Err())
			case err != nil:
				logger.With().Warning("Failed to get proof from Poet", log.Err(err))
				return nil
			}

			if err := nb.poetDB.ValidateAndStore(ctx, proof); err != nil && !errors.Is(err, ErrObjectExists) {
				logger.With().Warning("Failed to validate and store proof", log.Err(err), log.Object("proof", proof))
				return nil
			}

			// We are interested only in proofs that we are members of
			if !membersContain(proof.Members, challenge) {
				logger.With().Warning("poet proof membership doesn't contain the challenge", challenge)
				return nil
			}

			proofs <- proof
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return types.PoetProofRef{}, fmt.Errorf("querying for proofs: %w", err)
	}
	close(proofs)

	var bestProof *types.PoetProofMessage

	for proof := range proofs {
		nb.log.With().Info("Got a new PoET proof", log.Uint64("leafCount", proof.LeafCount))
		if bestProof == nil || bestProof.LeafCount < proof.LeafCount {
			bestProof = proof
		}
	}

	if bestProof != nil {
		ref, err := bestProof.Ref()
		if err != nil {
			return types.PoetProofRef{}, err
		}
		nb.log.With().Info("Selected the best proof", log.Uint64("leafCount", bestProof.LeafCount), log.Binary("ref", ref[:]))
		return ref, nil
	}

	return types.PoetProofRef{}, ErrPoetProofNotReceived
}
