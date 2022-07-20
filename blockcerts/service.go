package blockcerts

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"math"
	"sync"
)

const (
	blockCertifier = math.MaxInt - 2  // for Rolacle
	blockSigTopic  = "BlockSignature" // for gossip

)

type HareTerminationConfig struct {
	CommitteeSize int
}

// BlockCertifyingService is a long-lived service responsible for participating
// in block-signing upon hare termination.
type BlockCertifyingService struct {
	rolacle          hare.Rolacle
	config           HareTerminationConfig
	hareTerminations <-chan hare.TerminationBlockOutput
	blockSignersWg   sync.WaitGroup
	gossipPublisher  pubsub.Publisher
	blockSigner      signing.Signer
}

// NewBlockCertifyingService constructs a new BlockCertifyingService.
func NewBlockCertifyingService(
	hareTerminations <-chan hare.TerminationBlockOutput,
	rolacle hare.Rolacle,
	gossipPublisher pubsub.Publisher,
	blockSigner signing.Signer,
	config HareTerminationConfig,
) (*BlockCertifyingService, error) {
	service := &BlockCertifyingService{}
	service.rolacle = rolacle
	service.config = config
	service.hareTerminations = hareTerminations
	service.gossipPublisher = gossipPublisher
	service.blockSigner = blockSigner
	service.blockSignersWg = sync.WaitGroup{}
	return service, fmt.Errorf("not yet implemented")
}

func (s *BlockCertifyingService) Start(ctx context.Context) error {
	go s.certifyBlockServiceLoop(ctx)
	return nil
}

func (s *BlockCertifyingService) SignatureCacher() *SigCacher {

}

// GossipHandler returns a function that handles block value messages.
func (s *BlockCertifyingService) GossipHandler() pubsub.GossipHandler {
	return func(ctx context.Context, peerID p2p.Peer, bytes []byte,
	) pubsub.ValidationResult {
		msg := BlockSignature{}
		err := codec.Decode(bytes, msg)
		if err != nil {
			return pubsub.ValidationReject
		}
		isValidProof, err := s.rolacle.Validate(ctx,
			msg.layerID, blockCertifier, s.config.CommitteeSize, msg.senderNodeID,
			msg.eligibilityProof, msg.eligibilityCount)
		if err != nil {
			log.Error("hare termination gossip: %w", err)
			return pubsub.ValidationReject
		}
		if !isValidProof {
			return pubsub.ValidationReject
		}

		// Extract public key from value & validate

		// Store value
		store, err := NewCertifiedBlockStore(s)
		if err != nil {
			panic("error not yet handled")
		}
		err = store.storeBlockSignature(msg.blockID, msg)
		if err != nil {
			panic("error not yet handled")
		}
		panic("not yet implemented")
	}
}

func (s BlockCertifyingService) certifyBlockServiceLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			panic("cancel not yet implemented")

		case hareTermination := <-s.hareTerminations:
			if hareTermination == nil {
				log.Debug("block certification service: " +
					"hare termination channel closed.")
				return
			}
			// 1. check readiness
			ready, err := s.rolacle.IsIdentityActiveOnConsensusView(ctx,
				hareTermination.TerminatingNode(),
				hareTermination.ID(),
			)
			if err != nil {
				log.Error("error checking if active on consensus view")
			}
			if !ready {
				log.Debug("not active on consensus view: " +
					"not eligible to get committee seats.")
				break
			}
			// 2. calculate eligibility
			proof, err := s.rolacle.Proof(ctx, hareTermination.ID(), blockCertifier)
			if err != nil {
				log.Err(errors.Wrap(err, "could not retrieve eligibility proof from oracle"))
				break
			}

			committeeSeatCount, err := s.rolacle.CalcEligibility(ctx, hareTermination.ID(),
				blockCertifier, s.config.CommitteeSize, hareTermination.TerminatingNode(), proof)

			blockIDBytes := hareTermination.BlockID().Bytes()
			blockIDSignature := s.blockSigner.Sign(blockIDBytes)

			blockSig := BlockSignature{
				blockID:          hareTermination.BlockID(),
				layerID:          hareTermination.ID(),
				senderNodeID:     hareTermination.TerminatingNode(),
				eligibilityProof: proof,
				eligibilityCount: committeeSeatCount,
				blockSignature:   blockIDSignature,
			}

			s.blockSignersWg.Add(1)
			if !hareTermination.Completed() {
			}

			msgBytes, err := codec.Encode(blockSig)
			s.gossipPublisher.Publish(ctx, blockSigTopic, msgBytes)
		}

	}
}
