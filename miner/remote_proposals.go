package miner

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/remote_mocks.go -source=./remote_proposals.go

type proposalService interface {
	Proposal(ctx context.Context, layer types.LayerID, node types.NodeID) (*types.Proposal, uint64, error)
}

type beaconService interface {
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
}

type identityStates interface {
	SetEligibilitiesForEpoch(
		id types.NodeID,
		epoch types.EpochID,
		eligibilities map[types.LayerID][]types.VotingEligibility)
	AddProposal(id types.NodeID, proposals *types.Proposal)
}

type RemoteProposalBuilder struct {
	logger *zap.Logger
	cfg    config

	clock       layerClock
	publisher   pubsub.Publisher
	beaconSvc   beaconService
	proposalSvc proposalService
	signers     struct {
		mu      sync.Mutex
		signers map[types.NodeID]*signerSession
	}
	identityStates identityStates
}

// New creates a struct of block builder type.
func NewRemoteBuilder(
	clock layerClock,
	publisher pubsub.Publisher,
	bcn beaconService,
	prop proposalService,
	layerSize uint32,
	layersPerEpoch uint32,
	logger *zap.Logger,
	is identityStates,
) *RemoteProposalBuilder {
	pb := &RemoteProposalBuilder{
		cfg: config{
			workersLimit:   runtime.NumCPU(),
			activeSet:      DefaultActiveSetPreparation(),
			layerSize:      layerSize,
			layersPerEpoch: layersPerEpoch,
		},
		logger:      logger,
		clock:       clock,
		publisher:   publisher,
		beaconSvc:   bcn,
		proposalSvc: prop,
		signers: struct {
			mu      sync.Mutex
			signers map[types.NodeID]*signerSession
		}{
			signers: map[types.NodeID]*signerSession{},
		},
		identityStates: is,
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
	var (
		eg            errgroup.Group
		current       = pb.clock.CurrentLayer()
		epoch         = current.GetEpoch()
		next          = current + 1
		eligibilities = make(map[types.NodeID]map[types.LayerID][]types.VotingEligibility)
		epochBeacon   = make(map[types.EpochID]types.Beacon) // cache beacon values within an epoch
	)
	pb.logger.Info("started", zap.Inline(&pb.cfg), zap.Uint32("next", next.Uint32()))
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
			if e := current.GetEpoch(); e > epoch {
				eligibilities = make(map[types.NodeID]map[types.LayerID][]types.VotingEligibility)
				epochBeacon = make(map[types.EpochID]types.Beacon)
				epoch = e
			}
			if err := pb.build(ctx, current, eligibilities, epochBeacon); err != nil {
				pb.logger.Warn("failed to build proposal",
					log.ZContext(ctx),
					zap.Uint32("lid", current.Uint32()),
					zap.Error(err),
				)
			}

		}
	}
}

func (pb *RemoteProposalBuilder) build(
	ctx context.Context,
	layer types.LayerID,
	eligibilities map[types.NodeID]map[types.LayerID][]types.VotingEligibility,
	beacons map[types.EpochID]types.Beacon,
) error {
	epoch := layer.GetEpoch()
	pb.signers.mu.Lock()
	signers := maps.Values(pb.signers.signers)
	pb.signers.mu.Unlock()
	var err error
	bcn, ok := beacons[epoch]
	if !ok {
		bcn, err = pb.beaconSvc.Beacon(ctx, epoch)
		if err != nil {
			return fmt.Errorf("beacon: %w", err)
		}
		beacons[epoch] = bcn
	}

	for _, signer := range signers {
		nodeId := signer.signer.NodeID()
		proposal, nonce, err := pb.proposalSvc.Proposal(ctx, layer, nodeId)
		if err != nil {
			pb.logger.Error("get partial proposal", zap.Error(err))
			continue
		}
		if proposal == nil {
			// this node signer isn't eligible this epoch, continue
			pb.logger.Info("node not eligible on this layer. will try later")
			continue
		}

		var proofs map[types.LayerID][]types.VotingEligibility
		if proposal.Ballot.EpochData != nil {
			nodeElig, ok := eligibilities[nodeId]
			if !ok {
				proofs = calcEligibilityProofs(
					signer.signer.VRFSigner(),
					epoch,
					bcn,
					types.VRFPostIndex(nonce),
					proposal.Ballot.EpochData.EligibilityCount,
					pb.cfg.layersPerEpoch,
				)
				eligibilities[nodeId] = proofs
				pb.identityStates.SetEligibilitiesForEpoch(nodeId, epoch, proofs)
			} else {
				proofs = nodeElig
			}
		} else {
			proofs, ok = eligibilities[nodeId]
			if !ok {
				panic("missing node epoch eligibilities")
			}
		}

		eligibilities, ok := proofs[layer]
		if !ok {
			// not eligible in this layer, continue
			pb.logger.Info("node not eligible in this layer, will try later")
			continue
		}

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
		pb.identityStates.AddProposal(nodeId, proposal)
	}
	return nil
}
