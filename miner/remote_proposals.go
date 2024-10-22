package miner

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"
)

// Opt for configuring ProposalBuilder.
// type Opt func(h *RemoteProposalBuilder)

//// WithLayerSize defines the average number of proposal per layer.
//func WithLayerSize(size uint32) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.layerSize = size
//}
//}

//// WithWorkersLimit configures paralelization factor for builder operation when working with
//// more than one signer.
//func WithWorkersLimit(limit int) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.workersLimit = limit
//}
//}

//// WithLayerPerEpoch defines the number of layers per epoch.
//func WithLayerPerEpoch(layers uint32) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.layersPerEpoch = layers
//}
//}

//func WithMinimalActiveSetWeight(weight []types.EpochMinimalActiveWeight) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.minActiveSetWeight = weight
//}
//}

//// WithLogger defines the logger.
//func WithLogger(logger *zap.Logger) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.logger = logger
//}
//}

//func WithHdist(dist uint32) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.hdist = dist
//}
//}

//func WithNetworkDelay(delay time.Duration) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.networkDelay = delay
//}
//}

//func WithMinGoodAtxPercent(percent int) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.goodAtxPercent = percent
//}
//}

//// WithSigners guarantees that builder will start execution with provided list of signers.
//// Should be after logging.
//func WithSigners(signers ...*signing.EdSigner) Opt {
//return func(pb *RemoteProposalBuilder) {
//for _, signer := range signers {
//pb.Register(signer)
//}
//}
//}

//// WithActivesetPreparation overwrites configuration for activeset preparation.
//func WithActivesetPreparation(prep ActiveSetPreparation) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.cfg.activeSet = prep
//}
//}

//func withAtxSearch(p atxSearch) Opt {
//return func(pb *RemoteProposalBuilder) {
//pb.atxs = p
//}
//}

type nodeService interface {
	Proposal(ctx context.Context, layer types.LayerID, node types.NodeID) (*types.Proposal, uint64, error)
}
type RemoteProposalBuilder struct {
	logger *zap.Logger
	cfg    config

	// db      sql.Executor
	// localdb sql.Executor
	// atxsdata  *atxsdata.Data
	clock     layerClock
	publisher pubsub.Publisher
	nodeSvc   nodeService
	// conState  conservativeState
	// tortoise  votesEncoder
	// syncer    system.SyncStateProvider
	// activeGen *activeSetGenerator
	// atxs      atxSearch

	signers struct {
		mu      sync.Mutex
		signers map[types.NodeID]*signerSession
	}
	shared sharedSession
}

// New creates a struct of block builder type.
func NewRemoteBuilder(
	clock layerClock,
	publisher pubsub.Publisher,
	svc nodeService,
	layerSize uint32,
	layersPerEpoch uint32,
	logger *zap.Logger,
) *RemoteProposalBuilder {
	pb := &RemoteProposalBuilder{
		cfg: config{
			workersLimit:   runtime.NumCPU(),
			activeSet:      DefaultActiveSetPreparation(),
			layerSize:      layerSize,
			layersPerEpoch: layersPerEpoch,
		},
		logger:    logger,
		clock:     clock,
		publisher: publisher,
		nodeSvc:   svc,
		signers: struct {
			mu      sync.Mutex
			signers map[types.NodeID]*signerSession
		}{
			signers: map[types.NodeID]*signerSession{},
		},
	}
	if logger == nil {
		pb.logger = zap.NewNop()
	}
	return pb
}

func (pb *RemoteProposalBuilder) Register(sig *signing.EdSigner) {
	pb.signers.mu.Lock()
	defer pb.signers.mu.Unlock()
	_, exist := pb.signers.signers[sig.NodeID()]
	if !exist {
		pb.logger.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
		pb.signers.signers[sig.NodeID()] = &signerSession{
			signer: sig,
			log:    pb.logger.With(zap.String("signer", sig.NodeID().ShortString())),
		}
	}
}

// Start the loop that listens to layers and build proposals.
func (pb *RemoteProposalBuilder) Run(ctx context.Context) error {
	current := pb.clock.CurrentLayer()
	next := current + 1
	pb.logger.Info("started", zap.Inline(&pb.cfg), zap.Uint32("next", next.Uint32()))
	var eg errgroup.Group
	prepareDisabled := pb.cfg.activeSet.Tries == 0 || pb.cfg.activeSet.RetryInterval == 0
	if prepareDisabled {
		pb.logger.Warn("activeset will not be prepared in advance")
	}
	for {
		select {
		case <-ctx.Done():
			eg.Wait()
			return nil
		case <-pb.clock.AwaitLayer(next):
			current := pb.clock.CurrentLayer()
			if current.Before(next) {
				pb.logger.Info("time sync detected, realigning ProposalBuilder",
					zap.Uint32("current", current.Uint32()),
					zap.Uint32("next", next.Uint32()),
				)
				continue
			}
			next = current.Add(1)
			ctx := log.WithNewSessionID(ctx)

			if current <= types.GetEffectiveGenesis() {
				continue
			}
			fmt.Println("remote proposal builder going to build. layer", current.Uint32())
			if err := pb.build(ctx, current); err != nil {
				pb.logger.Warn("failed to build proposal",
					log.ZContext(ctx),
					zap.Uint32("lid", current.Uint32()),
					zap.Error(err),
				)
			}
		}
	}
}

func (pb *RemoteProposalBuilder) build(ctx context.Context, layer types.LayerID) error {
	pb.signers.mu.Lock()
	signers := maps.Values(pb.signers.signers)
	pb.signers.mu.Unlock()
	fmt.Println("remote proposal builder got some signers", signers)
	for _, signer := range signers {
		proposal, nonce, err := pb.nodeSvc.Proposal(ctx, layer, signer.signer.NodeID())
		if err != nil {
			pb.logger.Error("get partial proposal", zap.Error(err))
		}
		fmt.Println("remote proposal builder got proposal and nonce", proposal, nonce, err)
		if proposal == nil {
			// this node signer isn't eligible this epoch, continue
			pb.logger.Info("node not eligible on this layer. will try next")
			continue
		}

		proofs := calcEligibilityProofs(
			signer.signer.VRFSigner(),
			layer.GetEpoch(),
			proposal.Ballot.EpochData.Beacon,
			types.VRFPostIndex(nonce),
			proposal.Ballot.EpochData.EligibilityCount,
			pb.cfg.layersPerEpoch,
		)

		eligibilities, ok := proofs[layer]
		if !ok {
			// not eligible in this layer, continue
			pb.logger.Info("node not eligible in this layer, will try later")
			continue
		}

		proposal.EligibilityProofs = eligibilities
		proposal.Ballot.Signature = signer.signer.Sign(signing.BALLOT, proposal.Ballot.SignedBytes())
		proposal.Signature = signer.signer.Sign(signing.PROPOSAL, proposal.SignedBytes())
		proposal.MustInitialize()
		pb.logger.Info("did all the proposal stuff nicely, publishing", zap.Inline(proposal))
		if err := pb.publisher.Publish(ctx, pubsub.ProposalProtocol, codec.MustEncode(proposal)); err != nil {
			pb.logger.Error("failed to publish proposal",
				log.ZContext(ctx),
				zap.Uint32("lid", proposal.Layer.Uint32()),
				zap.Stringer("id", proposal.ID()),
				zap.Error(err),
			)
		}
		pb.logger.Info("proposal published successfully")
	}
	return nil
}
