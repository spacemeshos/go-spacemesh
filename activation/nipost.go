package activation

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
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
	Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Member, error)
}

func (nb *NIPostBuilder) loadState(challenge types.Hash32) {
	state, err := loadBuilderState(nb.dataDir)
	if err != nil {
		nb.log.With().Warning("cannot load nipost state", log.Err(err))
		return
	}
	if state.Challenge == challenge {
		nb.state = state
	} else {
		nb.log.Info("discarding stale nipost state")
		nb.state = &types.NIPostBuilderState{Challenge: challenge, NIPost: &types.NIPost{}}
	}
}

func (nb *NIPostBuilder) persistState() {
	if err := saveBuilderState(nb.dataDir, nb.state); err != nil {
		nb.log.With().Warning("cannot store nipost state", log.Err(err))
	}
}

// NIPostBuilder holds the required state and dependencies to create Non-Interactive Proofs of Space-Time (NIPost).
type NIPostBuilder struct {
	nodeID            types.NodeID
	dataDir           string
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
	GetProof(types.PoetProofRef) (*types.PoetProof, *types.Hash32, error)
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

// NewNIPostBuilder returns a NIPostBuilder.
func NewNIPostBuilder(
	nodeID types.NodeID,
	postSetupProvider postSetupProvider,
	poetProvers []PoetProvingServiceClient,
	poetDB poetDbAPI,
	dataDir string,
	lg log.Log,
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
		dataDir:           dataDir,
		log:               lg,
		signer:            signer,
		poetCfg:           poetCfg,
		layerClock:        layerClock,
	}
}

func (nb *NIPostBuilder) DataDir() string {
	return nb.dataDir
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

	logger.With().Info("building nipost",
		log.Time("poet round start", poetRoundStart),
		log.Time("poet round end", poetRoundEnd),
		log.Time("next poet round start", nextPoetRoundStart),
		log.Time("poet proof deadline", poetProofDeadline),
		log.Stringer("publish epoch", pubEpoch),
		log.Stringer("target epoch", challenge.TargetEpoch()),
	)

	challengeHash := challenge.Hash()
	nb.loadState(challengeHash)

	if s := nb.postSetupProvider.Status(); s.State != PostSetupStateComplete {
		return nil, 0, errors.New("post setup not complete")
	}

	// Phase 0: Submit challenge to PoET services.
	now := time.Now()
	if len(nb.state.PoetRequests) == 0 {
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

		nb.state.Challenge = challengeHash
		nb.state.PoetRequests = poetRequests
		nb.persistState()
		if err := ctx.Err(); err != nil {
			return nil, 0, fmt.Errorf("submitting challenges: %w", err)
		}
	}

	// Phase 1: query PoET services for proofs
	if nb.state.PoetProofRef == types.EmptyPoetProofRef {
		if poetProofDeadline.Before(now) {
			return nil, 0, fmt.Errorf("%w: poet proof for pub epoch %d must be available by now (%s)", ErrATXChallengeExpired, challenge.PublishEpoch, now)
		}
		getProofsCtx, cancel := context.WithDeadline(ctx, poetProofDeadline)
		defer cancel()
		poetProofRef, membership, err := nb.getBestProof(getProofsCtx, nb.state.Challenge)
		if err != nil {
			return nil, 0, &PoetSvcUnstableError{msg: "getBestProof failed", source: err}
		}
		if poetProofRef == types.EmptyPoetProofRef {
			return nil, 0, &PoetSvcUnstableError{source: ErrPoetProofNotReceived}
		}
		nb.state.PoetProofRef = poetProofRef
		nb.state.NIPost.Membership = *membership
		nb.persistState()
	}

	// Phase 2: Post execution.
	var postGenDuration time.Duration = 0
	if nb.state.NIPost.Post == nil {
		nb.log.With().Info("starting post execution", log.Binary("challenge", nb.state.PoetProofRef[:]))
		startTime := time.Now()
		proof, proofMetadata, err := nb.postSetupProvider.GenerateProof(ctx, nb.state.PoetProofRef[:])
		if err != nil {
			return nil, 0, fmt.Errorf("failed to generate Post: %v", err)
		}

		postGenDuration = time.Since(startTime)
		nb.log.With().Info("finished post execution", log.Duration("duration", postGenDuration))

		nb.state.NIPost.Post = proof
		nb.state.NIPost.PostMetadata = proofMetadata

		nb.persistState()
	}

	nb.log.Info("finished nipost construction")
	return nb.state.NIPost, postGenDuration, nil
}

// Submit the challenge to a single PoET.
func (nb *NIPostBuilder) submitPoetChallenge(ctx context.Context, poet PoetProvingServiceClient, prefix, challenge []byte, signature types.EdSignature, nodeID types.NodeID) (*types.PoetRequest, error) {
	poetServiceID, err := poet.PoetServiceID(ctx)
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to get PoET service ID", source: err}
	}
	logger := nb.log.WithContext(ctx).WithFields(log.String("poet_id", hex.EncodeToString(poetServiceID.ServiceID)))

	logger.Debug("querying for poet pow parameters")
	powParams, err := poet.PowParams(ctx)
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to get PoW params", source: err}
	}

	logger.Debug("doing pow with params: %v", powParams)
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

// membersContainChallenge verifies that the challenge is included in proof's members.
func membersContainChallenge(members []types.Member, challenge types.Hash32) (uint64, error) {
	for id, member := range members {
		if bytes.Equal(member[:], challenge.Bytes()) {
			return uint64(id), nil
		}
	}
	return 0, fmt.Errorf("challenge is not a member of the proof")
}

func (nb *NIPostBuilder) getBestProof(ctx context.Context, challenge types.Hash32) (types.PoetProofRef, *types.MerkleProof, error) {
	type poetProof struct {
		poet       *types.PoetProofMessage
		membership *types.MerkleProof
	}
	proofs := make(chan *poetProof, len(nb.state.PoetRequests))

	var eg errgroup.Group
	for _, r := range nb.state.PoetRequests {
		logger := nb.log.WithContext(ctx).WithFields(log.String("poet_id", hex.EncodeToString(r.PoetServiceID.ServiceID)), log.String("round", r.PoetRound.ID))
		client := nb.getPoetClient(ctx, r.PoetServiceID)
		if client == nil {
			logger.Warning("poet client not found")
			continue
		}
		round := r.PoetRound.ID
		// Time to wait before querying for the proof
		// The additional second is an optimization to be nicer to poet
		// and don't accidentally ask it to soon and have to retry.
		waitTime := time.Until(r.PoetRound.End.IntoTime()) + time.Second
		eg.Go(func() error {
			logger.With().Info("waiting till poet round end", log.Duration("wait time", waitTime))
			select {
			case <-ctx.Done():
				return fmt.Errorf("waiting to query proof: %w", ctx.Err())
			case <-time.After(waitTime):
			}

			proof, members, err := client.Proof(ctx, round)
			switch {
			case errors.Is(err, context.Canceled):
				return fmt.Errorf("querying proof: %w", ctx.Err())
			case err != nil:
				logger.With().Warning("failed to get proof from poet", log.Err(err))
				return nil
			}

			if err := nb.poetDB.ValidateAndStore(ctx, proof); err != nil && !errors.Is(err, ErrObjectExists) {
				logger.With().Warning("failed to validate and store proof", log.Err(err), log.Object("proof", proof))
				return nil
			}

			membership, err := constructMerkleProof(challenge, members)
			if err != nil {
				logger.With().Warning("failed to construct merkle proof", log.Err(err))
				return nil
			}

			proofs <- &poetProof{
				poet:       proof,
				membership: membership,
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return types.PoetProofRef{}, nil, fmt.Errorf("querying for proofs: %w", err)
	}
	close(proofs)

	var bestProof *poetProof

	for proof := range proofs {
		nb.log.With().Info("got poet proof", log.Uint64("leaf count", proof.poet.LeafCount))
		if bestProof == nil || bestProof.poet.LeafCount < proof.poet.LeafCount {
			bestProof = proof
		}
	}

	if bestProof != nil {
		ref, err := bestProof.poet.Ref()
		if err != nil {
			return types.PoetProofRef{}, nil, err
		}
		nb.log.With().Info("selected the best proof", log.Uint64("leafCount", bestProof.poet.LeafCount), log.Binary("ref", ref[:]))
		return ref, bestProof.membership, nil
	}

	return types.PoetProofRef{}, nil, ErrPoetProofNotReceived
}

func constructMerkleProof(challenge types.Hash32, members []types.Member) (*types.MerkleProof, error) {
	// We are interested only in proofs that we are members of
	id, err := membersContainChallenge(members, challenge)
	if err != nil {
		return nil, err
	}

	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{id: true}).
		WithHashFunc(shared.HashMembershipTreeNode).
		Build()
	if err != nil {
		return nil, fmt.Errorf("creating Merkle Tree: %w", err)
	}
	for _, member := range members {
		if err := tree.AddLeaf(member[:]); err != nil {
			return nil, fmt.Errorf("adding leaf to Merkle Tree: %w", err)
		}
	}
	nodes := tree.Proof()
	nodesH32 := make([]types.Hash32, 0, len(nodes))
	for _, n := range nodes {
		nodesH32 = append(nodesH32, types.BytesToHash(n))
	}
	return &types.MerkleProof{
		LeafIndex: id,
		Nodes:     nodesH32,
	}, nil
}
