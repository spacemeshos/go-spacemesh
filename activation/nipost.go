package activation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"
	postshared "github.com/spacemeshos/post/shared"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

const (
	// Jitter values to avoid all nodes querying the poet at the same time.
	// Note: the jitter values are represented as a percentage of cycle gap.
	//  mainnet cycle-gap: 12h
	//  systest cycle-gap: 30s

	// Minimum jitter value before querying for the proof.
	// Gives the poet service time to generate proof after a round ends (~8s on mainnet).
	//  mainnet -> 8.64s
	//  systest -> 0.36s
	minPoetGetProofJitter = 0.02

	// The maximum jitter value before querying for the proof.
	//  mainnet -> 17.28s
	//  systest -> 0.72s
	maxPoetGetProofJitter = 0.04
)

var ErrInvalidInitialPost = errors.New("invalid initial post")

// NIPostBuilder holds the required state and dependencies to create Non-Interactive Proofs of Space-Time (NIPost).
type NIPostBuilder struct {
	localDB *localsql.Database

	poetProvers    map[string]PoetService
	postService    postService
	logger         *zap.Logger
	poetCfg        PoetConfig
	layerClock     layerClock
	postStates     PostStates
	identityStates types.IdentityStates
	validator      nipostValidator
}

type NIPostBuilderOption func(*NIPostBuilder)

func WithPoetServices(clients ...PoetService) NIPostBuilderOption {
	return func(nb *NIPostBuilder) {
		nb.poetProvers = make(map[string]PoetService, len(clients))
		for _, client := range clients {
			nb.poetProvers[client.Address()] = client
		}
	}
}

func NipostbuilderWithPostStates(ps PostStates) NIPostBuilderOption {
	return func(nb *NIPostBuilder) {
		nb.postStates = ps
	}
}

func NipostbuilderWithIdentityStates(is types.IdentityStates) NIPostBuilderOption {
	return func(nb *NIPostBuilder) {
		nb.identityStates = is
	}
}

// NewNIPostBuilder returns a NIPostBuilder.
func NewNIPostBuilder(
	db *localsql.Database,
	postService postService,
	lg *zap.Logger,
	poetCfg PoetConfig,
	layerClock layerClock,
	validator nipostValidator,
	opts ...NIPostBuilderOption,
) (*NIPostBuilder, error) {
	b := &NIPostBuilder{
		localDB:        db,
		postService:    postService,
		logger:         lg,
		poetCfg:        poetCfg,
		layerClock:     layerClock,
		postStates:     NewPostStates(lg),
		identityStates: types.NewIdentityStateStorage(),
		validator:      validator,
	}

	for _, opt := range opts {
		opt(b)
	}
	return b, nil
}

func (nb *NIPostBuilder) ResetState(nodeId types.NodeID) error {
	if err := nipost.ClearPoetRegistrations(nb.localDB, nodeId); err != nil {
		return fmt.Errorf("clear poet registrations: %w", err)
	}
	if err := nipost.RemoveNIPost(nb.localDB, nodeId); err != nil {
		return fmt.Errorf("remove nipost: %w", err)
	}
	if err := nb.identityStates.Set(nodeId, types.WaitForPoetRoundStart); err != nil {
		return fmt.Errorf("set up node id state: %w", err)
	}
	return nil
}

func (nb *NIPostBuilder) Proof(
	ctx context.Context,
	nodeID types.NodeID,
	challenge []byte,
	postChallenge *types.NIPostChallenge,
) (*types.Post, *types.PostInfo, error) {
	nb.postStates.Set(nodeID, types.PostStateProving)

	started := false
	retries := 0
	for {
		client, err := nb.postService.Client(nodeID)
		if err != nil {
			select {
			case <-ctx.Done():
				if started {
					events.EmitPostFailure(nodeID)
				}
				return nil, nil, ctx.Err()
			case <-time.After(2 * time.Second): // Wait a few seconds and try connecting again
				retries++
				if retries%10 == 0 { // every 20 seconds inform user about lost connection (for remote post service)
					// TODO(mafa): emit event warning user about lost connection
					nb.logger.Warn("post service not connected - waiting for reconnection",
						zap.Stringer("smesherID", nodeID),
						zap.Error(err),
					)
				}
				continue
			}
		}
		if !started {
			events.EmitPostStart(nodeID, challenge)
			started = true
		}

		retries = 0
		// we check whether an initial post is included in the challenge
		// if so, we verify it to still be valid before creating the post
		// e.g. the PoST size might have changed
		if postChallenge != nil && postChallenge.InitialPost != nil {
			info, err := client.Info(ctx)
			if errors.Is(err, ErrPostClientClosed) {
				continue
			} else if err != nil {
				events.EmitPostFailure(nodeID)
				return nil, nil, fmt.Errorf("failed to get post info: %w", err)
			}
			if err := nb.validator.PostV2(ctx,
				nodeID,
				info.CommitmentATX,
				postChallenge.InitialPost,
				postshared.ZeroChallenge,
				info.NumUnits,
			); err != nil {
				return nil, nil, ErrInvalidInitialPost
			}
		}
		post, postInfo, err := client.Proof(ctx, challenge)
		switch {
		case errors.Is(err, ErrPostClientClosed):
			continue
		case err != nil:
			// We don't set the state to idle here, because we will retry up the stack.
			events.EmitPostFailure(nodeID)
			return nil, nil, err
		default: // err == nil
			events.EmitPostComplete(nodeID, challenge)
			nb.postStates.Set(nodeID, types.PostStateIdle)
			return post, postInfo, err
		}
	}
}

// BuildNIPost uses the given challenge to build a NIPost.
// The process can take considerable time, because it includes waiting for the poet service to
// publish a proof - a process that takes about an epoch.
func (nb *NIPostBuilder) BuildNIPost(
	ctx context.Context,
	signer *signing.EdSigner,
	challenge types.Hash32,
	postChallenge *types.NIPostChallenge,
) (*nipost.NIPostState, error) {
	logger := nb.logger.With(log.ZContext(ctx), log.ZShortStringer("smesherID", signer.NodeID()))
	// Note: to avoid missing next PoET round, we need to publish the ATX before the next PoET round starts.
	//   We can still publish an ATX late (i.e. within publish epoch) and receive rewards, but we will miss one
	//   epoch because we didn't submit the challenge to PoET in time for next round.
	//                                 PoST
	//         ┌─────────────────────┐  ┌┐┌─────────────────────┐
	//         │     POET ROUND      │  │││   NEXT POET ROUND   │
	// ┌────▲──┴──────────────────┬──▲──┴┴┴─────────────────▲┬──┴─────────────► time
	// │    │      EPOCH          │  │   PUBLISH EPOCH      ││  TARGET EPOCH
	// └────┼─────────────────────┴──┼──────────────────────┼┴────────────────
	//      │                        │                      │
	//  WE ARE HERE            PROOF BECOMES         ATX PUBLICATION
	//                           AVAILABLE               DEADLINE

	poetRoundStart := nb.layerClock.LayerToTime((postChallenge.PublishEpoch - 1).FirstLayer()).
		Add(nb.poetCfg.PhaseShift)
	poetRoundEnd := nb.layerClock.LayerToTime(postChallenge.PublishEpoch.FirstLayer()).
		Add(nb.poetCfg.PhaseShift).
		Add(-nb.poetCfg.CycleGap)

	// we want to publish before the publish epoch ends or we won't receive rewards
	publishEpochEnd := nb.layerClock.LayerToTime((postChallenge.PublishEpoch + 1).FirstLayer())

	// we want to fetch the PoET proof latest 1 CycleGap before the publish epoch ends
	// so that a node that is setup correctly (i.e. can generate a PoST proof within the cycle gap)
	// has enough time left to generate a post proof and publish
	poetProofDeadline := publishEpochEnd.Add(-nb.poetCfg.CycleGap)

	logger.Info("building nipost",
		zap.Time("poet round start", poetRoundStart),
		zap.Time("poet round end", poetRoundEnd),
		zap.Time("publish epoch end", publishEpochEnd),
		zap.Uint32("publish epoch", postChallenge.PublishEpoch.Uint32()),
	)

	// Stage 0: Submit challenge to PoET services.
	// Deadline: start of PoET round: we will not accept registrations after that
	submittedRegistrations, err := nb.submitPoetChallenges(
		ctx,
		signer,
		poetProofDeadline,
		poetRoundStart, poetRoundEnd, challenge.Bytes(),
	)
	regErr := &PoetRegistrationMismatchError{}
	switch {
	case errors.As(err, &regErr):
		logger.Fatal(
			"None of the poets listed in the config matches the existing registrations. "+
				"Verify your config and local database state.",
			zap.Strings("registrations", regErr.registrations),
			zap.Strings("configured_poets", regErr.configuredPoets),
		)
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("submitting to poets: %w", err)
	}

	// Stage 1: query PoET services for proofs
	if err := nb.identityStates.Set(signer.NodeID(), types.WaitForPoetRoundEnd); err != nil {
		nb.logger.Warn("failed to switch identity state",
			zap.Stringer("smesherID", signer.NodeID()),
			zap.Error(err),
		)
	}

	poetProofRef, membership, err := nipost.PoetProofRef(nb.localDB, signer.NodeID())
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		nb.logger.Warn("cannot get poet proof ref", zap.Error(err))
	}

	if poetProofRef == types.EmptyPoetProofRef {
		now := time.Now()
		// Deadline: the end of the publish epoch minus the cycle gap. A node that is setup correctly (i.e. can
		// generate a PoST proof within the cycle gap) has enough time left to generate a post proof and publish.
		if poetProofDeadline.Before(now) {
			return nil, fmt.Errorf(
				"%w: deadline to query poet proof for pub epoch %d exceeded (deadline: %s, now: %s)",
				ErrATXChallengeExpired,
				postChallenge.PublishEpoch,
				poetProofDeadline,
				now,
			)
		}

		events.EmitPoetWaitProof(signer.NodeID(), postChallenge.PublishEpoch, poetRoundEnd)

		poetProofRef, membership, err = nb.getBestProof(ctx, signer.NodeID(), challenge, submittedRegistrations)
		if err != nil {
			return nil, &PoetSvcUnstableError{msg: "getBestProof failed", source: err}
		}
		if poetProofRef == types.EmptyPoetProofRef {
			return nil, &PoetSvcUnstableError{source: ErrPoetProofNotReceived}
		}
		if err := nipost.UpdatePoetProofRef(nb.localDB, signer.NodeID(), poetProofRef, membership); err != nil {
			nb.logger.Warn("cannot persist poet proof ref", zap.Error(err))
		}
	}

	// Stage 2: Post execution.
	if err := nb.identityStates.Set(signer.NodeID(), types.PostProving); err != nil {
		nb.logger.Warn("failed to switch identity state",
			zap.Stringer("smesherID", signer.NodeID()),
			zap.Error(err),
		)
	}

	nipostState, err := nipost.NIPost(nb.localDB, signer.NodeID())
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		nb.logger.Warn("cannot get nipost", zap.Error(err))
	}
	if nipostState == nil {
		now := time.Now()
		// Deadline: the end of the publish epoch. If we do not publish within
		// the publish epoch we won't receive any rewards in the target epoch.
		if publishEpochEnd.Before(now) {
			return nil, fmt.Errorf(
				"%w: deadline to publish ATX for pub epoch %d exceeded (deadline: %s, now: %s)",
				ErrATXChallengeExpired,
				postChallenge.PublishEpoch,
				publishEpochEnd,
				now,
			)
		}
		postCtx, cancel := context.WithDeadline(ctx, publishEpochEnd)
		defer cancel()

		nb.logger.Info("starting post execution", zap.Binary("challenge", poetProofRef[:]))

		startTime := time.Now()
		proof, postInfo, err := nb.Proof(postCtx, signer.NodeID(), poetProofRef[:], postChallenge)
		if err != nil {
			return nil, fmt.Errorf("failed to generate Post: %w", err)
		}

		postGenDuration := time.Since(startTime)

		nb.logger.Info("finished post execution", zap.Duration("duration", postGenDuration))

		metrics.PostDuration.Set(float64(postGenDuration.Nanoseconds()))
		public.PostSeconds.Set(postGenDuration.Seconds())

		nipostState = &nipost.NIPostState{
			NIPost: &types.NIPost{
				Post:       proof,
				Membership: *membership,
				PostMetadata: &types.PostMetadata{
					Challenge:     poetProofRef[:],
					LabelsPerUnit: postInfo.LabelsPerUnit,
				},
			},
			NumUnits: postInfo.NumUnits,
			VRFNonce: *postInfo.Nonce,
		}
		if err := nipost.AddNIPost(nb.localDB, signer.NodeID(), nipostState); err != nil {
			nb.logger.Warn("cannot persist nipost state", zap.Error(err))
		}
	}

	nb.logger.Info("finished nipost construction")
	return nipostState, nil
}

// withConditionalTimeout returns a context.WithTimeout if the timeout is greater than 0, otherwise it returns
// the original context.
func withConditionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, func() {}
}

// Submit the challenge (register) to a single PoET and returns registration in case of success.
func (nb *NIPostBuilder) submitPoetChallenge(
	ctx context.Context,
	nodeID types.NodeID,
	fetchProofDeadline,
	poetRoundEnd time.Time,
	client PoetService,
	prefix, challenge []byte,
	signature types.EdSignature,
) (nipost.PoETRegistration, error) {
	logger := nb.logger.With(
		log.ZContext(ctx),
		zap.String("poet", client.Address()),
		log.ZShortStringer("smesherID", nodeID),
	)

	logger.Debug("submitting challenge to poet proving service")

	registration := nipost.PoETRegistration{
		ChallengeHash: types.Hash32(challenge),
		Address:       client.Address(),
	}

	round, err := client.Submit(ctx, fetchProofDeadline, prefix, challenge, signature, nodeID)
	if err != nil {
		registration.RoundEnd = poetRoundEnd

		if err := nipost.AddPoetRegistration(nb.localDB, nodeID, registration); err != nil {
			nb.logger.Warn("failed to save poet registration into db",
				zap.Error(err),
				log.ZShortStringer("smesherID", nodeID))
		}
		return nipost.PoETRegistration{}, &PoetSvcUnstableError{msg: "failed to submit challenge to poet service", source: err}
	}

	logger.Info("challenge submitted to poet proving service", zap.String("round", round.ID))

	registration.RoundEnd = round.End
	registration.RoundID = round.ID

	if err := nipost.AddPoetRegistration(nb.localDB, nodeID, registration); err != nil {
		return nipost.PoETRegistration{}, err
	}
	return registration, err
}

// submitPoetChallenges submit the challenge to registered PoETs
// if some registrations are missing and PoET round didn't start.
func (nb *NIPostBuilder) submitPoetChallenges(
	ctx context.Context,
	signer *signing.EdSigner,
	fetchProofDeadline,
	curPoetRoundStartDeadline,
	curPoetRoundEndDeadline time.Time,
	challenge []byte,
) ([]nipost.PoETRegistration, error) {
	// check if some registrations missing or were removed
	nodeID := signer.NodeID()
	registrations, err := nipost.PoetRegistrations(nb.localDB, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get poet registrations from db: %w", err)
	}

	registrationsMap := make(map[string]nipost.PoETRegistration)
	// TODO (Sofia) analyse failed registrations too and if deadline has not expired,
	// try to re-register (requires db txs)
	for _, reg := range registrations {
		if reg.RoundID != "" {
			registrationsMap[reg.Address] = reg
		}
	}

	existingRegistrationsMap := make(map[string]nipost.PoETRegistration)
	var missingRegistrations []PoetService
	for addr, poet := range nb.poetProvers {
		if val, ok := registrationsMap[addr]; ok {
			existingRegistrationsMap[addr] = val
		} else {
			missingRegistrations = append(missingRegistrations, poet)
		}
	}

	misconfiguredRegistrations := make(map[string]struct{})
	for addr := range registrationsMap {
		if _, ok := existingRegistrationsMap[addr]; !ok {
			misconfiguredRegistrations[addr] = struct{}{}
		}
	}

	if len(misconfiguredRegistrations) != 0 {
		nb.logger.Warn(
			"Found existing registrations for poets not listed in the config. Will not fetch proof from them.",
			zap.Strings("registrations_addresses", maps.Keys(misconfiguredRegistrations)),
			log.ZShortStringer("smesherID", nodeID),
		)
	}

	existingRegistrations := maps.Values(existingRegistrationsMap)
	if len(missingRegistrations) == 0 {
		return existingRegistrations, nil
	}

	now := time.Now()
	if curPoetRoundStartDeadline.Before(now) {
		switch {
		case len(existingRegistrations) == 0 && len(registrations) == 0:
			// no existing registration at all, drop current registration challenge
			return nil, fmt.Errorf(
				"%w: poet round has already started at %s (now: %s)",
				ErrATXChallengeExpired,
				curPoetRoundStartDeadline,
				now,
			)
		case len(existingRegistrations) == 0:
			// no existing registration for given poets set
			return nil, &PoetRegistrationMismatchError{
				registrations:   maps.Keys(registrationsMap),
				configuredPoets: maps.Keys(nb.poetProvers),
			}
		default:
			return existingRegistrations, nil
		}
	}

	// send registrations to missing addresses
	signature := signer.Sign(signing.POET, challenge)
	prefix := bytes.Join([][]byte{signer.Prefix(), {byte(signing.POET)}}, nil)

	fmt.Printf("curPoetRoundStartDeadline %v \n", curPoetRoundStartDeadline)

	submitCtx, cancel := context.WithDeadline(ctx, curPoetRoundStartDeadline)
	defer cancel()

	eg, ctx := errgroup.WithContext(submitCtx)
	submittedRegistrationsChan := make(chan nipost.PoETRegistration, len(missingRegistrations))

	for _, client := range missingRegistrations {
		eg.Go(func() error {
			registration, err := nb.submitPoetChallenge(
				ctx, nodeID,
				fetchProofDeadline,
				curPoetRoundEndDeadline,
				client, prefix, challenge, signature,
			)
			if err != nil {
				nb.logger.Warn("failed to submit challenge to poet",
					zap.Error(err),
					log.ZShortStringer("smesherID", nodeID),
				)
			} else {
				submittedRegistrationsChan <- registration
			}
			return nil
		})
	}

	eg.Wait()
	close(submittedRegistrationsChan)

	for registration := range submittedRegistrationsChan {
		existingRegistrations = append(existingRegistrations, registration)
	}

	if len(existingRegistrations) == 0 {
		if curPoetRoundStartDeadline.Before(time.Now()) {
			return nil, ErrATXChallengeExpired
		}
		return nil, &PoetSvcUnstableError{msg: "failed to submit challenge to any PoET", source: ctx.Err()}
	}

	return existingRegistrations, nil
}

// membersContainChallenge verifies that the challenge is included in proof's members.
func membersContainChallenge(members []types.Hash32, challenge types.Hash32) (uint64, error) {
	for id, member := range members {
		if bytes.Equal(member[:], challenge.Bytes()) {
			return uint64(id), nil
		}
	}
	return 0, errors.New("challenge is not a member of the proof")
}

func (nb *NIPostBuilder) getBestProof(
	ctx context.Context,
	nodeID types.NodeID,
	challenge types.Hash32,
	registrations []nipost.PoETRegistration,
) (types.PoetProofRef, *types.MerkleProof, error) {
	type poetProof struct {
		poet       *types.PoetProof
		membership *types.MerkleProof
	}
	proofs := make(chan *poetProof, len(registrations))

	var eg errgroup.Group
	for _, r := range registrations {
		if len(r.RoundID) == 0 {
			continue // skip failed registrations
		}

		logger := nb.logger.With(
			log.ZContext(ctx),
			log.ZShortStringer("smesherID", nodeID),
			zap.String("poet_address", r.Address),
			zap.String("round", r.RoundID),
		)

		client, ok := nb.poetProvers[r.Address]
		if !ok {
			logger.Warn("poet client not found")
			continue
		}

		round := r.RoundID
		waitDeadline := proofDeadline(r.RoundEnd, nb.poetCfg.CycleGap)
		eg.Go(func() error {
			logger.Info("waiting until poet round end", zap.Duration("wait time", time.Until(waitDeadline)))
			select {
			case <-ctx.Done():
				return fmt.Errorf("waiting to query proof: %w", ctx.Err())
			case <-time.After(time.Until(waitDeadline)):
			}

			if err := nb.identityStates.Set(nodeID, types.FetchingProofs); err != nil {
				nb.logger.Warn("failed to switch identity state",
					zap.Stringer("smesherID", nodeID),
					zap.Error(err),
				)
			}

			proof, members, err := client.Proof(ctx, round)
			if err != nil {
				logger.Warn("failed to get proof from poet", zap.Error(err))
				return nil
			}

			membership, err := constructMerkleProof(challenge, members)
			if err != nil {
				logger.Warn("failed to construct merkle proof", zap.Error(err))
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
		nb.logger.Info(
			"got poet proof",
			zap.Uint64("leaf count", proof.poet.LeafCount),
			log.ZShortStringer("smesherID", nodeID),
		)
		if bestProof == nil || bestProof.poet.LeafCount < proof.poet.LeafCount {
			bestProof = proof
		}
	}

	if bestProof != nil {
		ref, err := bestProof.poet.Ref()
		if err != nil {
			return types.PoetProofRef{}, nil, err
		}
		nb.logger.Info(
			"selected the best proof",
			zap.Uint64("leafCount", bestProof.poet.LeafCount),
			zap.Binary("ref", ref[:]),
			log.ZShortStringer("smesherID", nodeID),
		)
		return ref, bestProof.membership, nil
	}

	return types.PoetProofRef{}, nil, ErrPoetProofNotReceived
}

func constructMerkleProof(challenge types.Hash32, members []types.Hash32) (*types.MerkleProof, error) {
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

func randomDurationInRange(min, max time.Duration) time.Duration {
	return min + rand.N(max-min+1)
}

// Calculate the time to wait before querying for the proof
// We add a jitter to avoid all nodes querying for the proof at the same time.
func proofDeadline(roundEnd time.Time, cycleGap time.Duration) (waitTime time.Time) {
	minJitter := time.Duration(float64(cycleGap) * minPoetGetProofJitter / 100.0)
	maxJitter := time.Duration(float64(cycleGap) * maxPoetGetProofJitter / 100.0)
	jitter := randomDurationInRange(minJitter, maxJitter)
	return roundEnd.Add(jitter)
}
