package activation

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
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
	// Submit registers a challenge in the proving service current open round.
	Submit(ctx context.Context, challenge []byte, signature []byte) (*types.PoetRound, error)

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID(context.Context) (types.PoetServiceID, error)

	GetProof(ctx context.Context, roundID string) (*types.PoetProofMessage, error)
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
	postSetupProvider postSetupProvider
	poetProvers       []PoetProvingServiceClient
	poetDB            poetDbAPI
	state             *types.NIPostBuilderState
	log               log.Log
	signer            signing.Signer
}

type poetDbAPI interface {
	GetProof(types.PoetProofRef) (*types.PoetProof, error)
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

// NewNIPostBuilder returns a NIPostBuilder.
func NewNIPostBuilder(
	minerID types.NodeID,
	postSetupProvider postSetupProvider,
	poetProvers []PoetProvingServiceClient,
	poetDB poetDbAPI,
	db *sql.Database,
	log log.Log,
	signer signing.Signer,
) *NIPostBuilder {
	return &NIPostBuilder{
		minerID:           minerID.Bytes(),
		postSetupProvider: postSetupProvider,
		poetProvers:       poetProvers,
		poetDB:            poetDB,
		state:             &types.NIPostBuilderState{NIPost: &types.NIPost{}},
		db:                db,
		log:               log,
		signer:            signer,
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
func (nb *NIPostBuilder) BuildNIPost(ctx context.Context, challenge *types.PoetChallenge, poetProofDeadline time.Time) (*types.NIPost, time.Duration, error) {
	challengeHash, err := challenge.Hash()
	if err != nil {
		return nil, 0, err
	}
	nb.load(*challengeHash)

	if s := nb.postSetupProvider.Status(); s.State != PostSetupStateComplete {
		return nil, 0, errors.New("post setup not complete")
	}

	nipost := nb.state.NIPost

	// Phase 0: Submit challenge to PoET services.
	if nb.state.PoetRequests == nil {
		challenge, err := codec.Encode(challenge)
		if err != nil {
			return nil, 0, err
		}
		signature := nb.signer.Sign(challenge)
		poetRequests := nb.submitPoetChallenges(ctx, challenge, signature)
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}

		validPoetRequests := make([]types.PoetRequest, 0, len(poetRequests))
		for _, req := range poetRequests {
			if !bytes.Equal(req.PoetRound.ChallengeHash[:], challengeHash[:]) {
				nb.log.With().Info(
					"poet returned invalid challenge hash",
					req.PoetRound.ChallengeHash,
					log.String("poet_id", hex.EncodeToString(req.PoetServiceID)),
				)
			} else {
				validPoetRequests = append(validPoetRequests, req)
			}
		}
		if len(validPoetRequests) == 0 {
			return nil, 0, &PoetSvcUnstableError{msg: "failed to submit challenge to any PoET"}
		}
		nipost.Challenge = challengeHash
		nb.state.Challenge = *challengeHash
		nb.state.PoetRequests = validPoetRequests
		nb.persist()
	}

	// Phase 1: query PoET services for proofs
	if nb.state.PoetProofRef == nil {
		getProofsCtx, cancel := context.WithDeadline(ctx, poetProofDeadline)
		defer cancel()
		poetProofRef, err := nb.getBestProof(getProofsCtx, challengeHash)
		if err != nil {
			return nil, 0, &PoetSvcUnstableError{msg: "getBestProof failed", source: err}
		}
		if poetProofRef == nil {
			return nil, 0, &PoetSvcUnstableError{source: ErrPoetProofNotReceived}
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
		proof, proofMetadata, err := nb.postSetupProvider.GenerateProof(nb.state.PoetProofRef)
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
func (nb *NIPostBuilder) submitPoetChallenge(ctx context.Context, poet PoetProvingServiceClient, challenge []byte, signature []byte) (*types.PoetRequest, error) {
	poetServiceID, err := poet.PoetServiceID(ctx)
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to get PoET service ID", source: err}
	}
	logger := nb.log.WithFields(log.String("poet_id", hex.EncodeToString(poetServiceID)))
	logger.Debug("submitting challenge to poet proving service")

	round, err := poet.Submit(ctx, challenge, signature)
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
func (nb *NIPostBuilder) submitPoetChallenges(ctx context.Context, challenge []byte, signature []byte) []types.PoetRequest {
	g, ctx := errgroup.WithContext(ctx)
	poetRequestsChannel := make(chan types.PoetRequest, len(nb.poetProvers))
	for _, poetProver := range nb.poetProvers {
		poet := poetProver
		g.Go(func() error {
			if poetRequest, err := nb.submitPoetChallenge(ctx, poet, challenge, signature); err == nil {
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
		if clientId, err := client.PoetServiceID(ctx); err == nil && bytes.Equal(id, clientId) {
			return client
		}
	}
	return nil
}

func membersContain(members [][]byte, challenge *types.Hash32) bool {
	for _, member := range members {
		if bytes.Equal(member, challenge.Bytes()) {
			return true
		}
	}
	return false
}

func (nb *NIPostBuilder) getProofWithRetry(ctx context.Context, client PoetProvingServiceClient, roundID string, retryInterval time.Duration) (*types.PoetProofMessage, error) {
	for {
		proof, err := client.GetProof(ctx, roundID)
		switch {
		case err == nil:
			return proof, nil
		case errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNotFound):
			nb.log.With().Debug("Proof not found, retrying", log.Duration("interval", retryInterval))
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("retry was canceled: %w", ctx.Err())
			case <-time.After(retryInterval):
			}
		default:
			return nil, err
		}
	}
}

func (nb *NIPostBuilder) getBestProof(ctx context.Context, challenge *types.Hash32) (types.PoetProofRef, error) {
	proofs := make(chan *types.PoetProofMessage, len(nb.state.PoetRequests))

	var eg errgroup.Group
	for _, r := range nb.state.PoetRequests {
		logger := nb.log.WithFields(log.String("poet_id", hex.EncodeToString(r.PoetServiceID)), log.String("round", r.PoetRound.ID))
		client := nb.getPoetClient(ctx, r.PoetServiceID)
		if client == nil {
			logger.Warning("Poet client not found")
			continue
		}
		round := r.PoetRound.ID
		// Time to wait before quering for the proof
		// The additional second is an optimization to be nicer to poet
		// and don't accidentially ask it to soon and have to retry.
		waitTime := time.Until(r.PoetRound.End.IntoTime()) + time.Second
		eg.Go(func() error {
			logger.With().Info("Waiting till poet round end", log.Duration("wait time", waitTime))
			select {
			case <-ctx.Done():
				logger.With().Info("Waiting interrupted", log.Err(ctx.Err()))
				return ctx.Err()
			case <-time.After(waitTime):
			}
			proof, err := nb.getProofWithRetry(ctx, client, round, time.Second)
			if err != nil {
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
		return nil, fmt.Errorf("querying for proofs failed: %w", err)
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
			return nil, fmt.Errorf("failed to get proof ref: %w", err)
		}
		nb.log.With().Debug("Selected the best proof", log.Uint64("leafCount", bestProof.LeafCount), log.Binary("ref", ref))
		return ref, nil
	}

	return nil, ErrPoetProofNotReceived
}
