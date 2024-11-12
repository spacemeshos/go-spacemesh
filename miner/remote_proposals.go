package miner

import (
	"context"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type nodeService interface {
	Proposal(ctx context.Context, layer types.LayerID, node types.NodeID) (*types.Proposal, uint64, error)
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
}
type RemoteProposalBuilder struct {
	logger *zap.Logger
	cfg    config

	clock     layerClock
	publisher pubsub.Publisher
	nodeSvc   nodeService
	signers   struct {
		mu      sync.Mutex
		signers map[types.NodeID]*signerSession
	}
	epochEligibilities map[types.EpochID]map[types.NodeID]eligibilities
}

type eligibilities struct {
	slots  uint32
	proofs map[types.LayerID][]types.VotingEligibility
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
		logger:             logger,
		clock:              clock,
		publisher:          publisher,
		nodeSvc:            svc,
		epochEligibilities: make(map[types.EpochID]map[types.NodeID]eligibilities),
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
			if err := pb.build(ctx, current); err != nil {
				pb.logger.Warn("failed to build proposal",
					log.ZContext(ctx),
					zap.Uint32("lid", current.Uint32()),
					zap.Error(err),
				)
			}

			pb.clean(current)
		}
	}
}

func (pb *RemoteProposalBuilder) clean(layer types.LayerID) {
	var vals []types.EpochID
	lim := layer.GetEpoch() - 2
	for k := range pb.epochEligibilities {
		if k <= lim {
			vals = append(vals, k)
		}
	}
	for _, v := range vals {
		delete(pb.epochEligibilities, v)
	}
}

func (pb *RemoteProposalBuilder) build(ctx context.Context, layer types.LayerID) error {
	epoch := layer.GetEpoch()
	pb.signers.mu.Lock()
	signers := maps.Values(pb.signers.signers)
	pb.signers.mu.Unlock()
	for _, signer := range signers {
		nodeId := signer.signer.NodeID()
		proposal, nonce, err := pb.nodeSvc.Proposal(ctx, layer, nodeId)
		if err != nil {
			pb.logger.Error("get partial proposal", zap.Error(err))
			continue
		}
		if proposal == nil {
			// this node signer isn't eligible this epoch, continue
			pb.logger.Info("node not eligible on this layer. will try later")
			continue
		}
		bcn, err := pb.nodeSvc.Beacon(ctx, epoch)
		if err != nil {
			pb.logger.Error("get beacon", zap.Error(err))
			continue
		}
		var (
			elig   uint32
			proofs map[types.LayerID][]types.VotingEligibility
			ok     bool
		)
		if proposal.Ballot.EpochData != nil {
			_, ok := pb.epochEligibilities[epoch]
			if !ok {
				pb.epochEligibilities[epoch] = make(map[types.NodeID]eligibilities)
			}
			nodeElig, ok := pb.epochEligibilities[epoch][nodeId]
			if !ok {
				elig = proposal.Ballot.EpochData.EligibilityCount
				proofs = calcEligibilityProofs(
					signer.signer.VRFSigner(),
					epoch,
					bcn,
					types.VRFPostIndex(nonce),
					elig,
					pb.cfg.layersPerEpoch,
				)
				pb.epochEligibilities[epoch][nodeId] = eligibilities{slots: elig, proofs: proofs}
			} else {
				elig = nodeElig.slots
				proofs = nodeElig.proofs
			}
		} else {
			nodeElig, ok := pb.epochEligibilities[epoch]
			if !ok {
				panic("missing epoch eligibilities")
			}
			eligibilities, ok := nodeElig[nodeId]
			if !ok {
				panic("missing node epoch eligibilities")
			}
			elig = eligibilities.slots
			proofs = eligibilities.proofs
		}

		eligibilities, ok := proofs[layer]
		if !ok {
			// not eligible in this layer, continue
			pb.logger.Info("node not eligible in this layer, will try later")
			continue
		}

		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		rng.Shuffle(len(proposal.TxIDs), func(i, j int) {
			proposal.TxIDs[i], proposal.TxIDs[j] = proposal.TxIDs[j], proposal.TxIDs[i]
		})

		proposal.EligibilityProofs = eligibilities
		proposal.Ballot.Signature = signer.signer.Sign(signing.BALLOT, proposal.Ballot.SignedBytes())
		proposal.Signature = signer.signer.Sign(signing.PROPOSAL, proposal.SignedBytes())
		err = proposal.Initialize()
		if err != nil {
			pb.logger.Error("failed to initialize proposal", zap.Error(err))
			continue
		}
		pb.logger.Info("publishing proposal", zap.Inline(proposal))
		if err := pb.publisher.Publish(ctx, pubsub.ProposalProtocol, codec.MustEncode(proposal)); err != nil {
			pb.logger.Error("failed to publish proposal",
				log.ZContext(ctx),
				zap.Uint32("lid", proposal.Layer.Uint32()),
				zap.Stringer("id", proposal.ID()),
				zap.Error(err),
			)
		}
	}
	return nil
}
