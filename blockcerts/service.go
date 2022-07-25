package blockcerts

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"math"
	"sync"
)

const (
	blockCertifierRole = math.MaxInt - 2  // for Rolacle
	blockSigTopic      = "BlockSignature" // for gossip

)

type BlockCertificateConfig struct {
	CommitteeSize int
}
type BlockCertificate struct {
	BlockID               types.BlockID
	LayerID               types.LayerID
	terminationSignatures []BlockCertificateSignature
}
type BlockSignature struct {
	blockID types.BlockID
	BlockCertificateSignature
}
type BlockCertificateSignature struct {
	signerNodeID         types.NodeID
	signerRoleProof      []byte
	signerCommitteeSeats uint16

	blockIDSignature []byte
}
type blockSignatureMsg struct {
	layerID types.LayerID
	BlockSignature
}

// BlockCertifyingService is a long-lived service responsible for participating
// in block-signing upon hare termination.
type BlockCertifyingService struct {
	rolacle          hare.Rolacle
	config           BlockCertificateConfig
	hareTerminations <-chan hare.TerminationBlockOutput
	gossipPublisher  pubsub.Publisher
	blockSigner      signing.Signer
	signatureCache   sigCache
	completedCertsCh chan BlockCertificate
	logger           log.Logger
}

// NewBlockCertifyingService constructs a new BlockCertifyingService.
func NewBlockCertifyingService(
	hareTerminations <-chan hare.TerminationBlockOutput,
	rolacle hare.Rolacle,
	gossipPublisher pubsub.Publisher,
	blockSigner signing.Signer,
	config BlockCertificateConfig,
	logger log.Logger,
) (*BlockCertifyingService, error) {
	service := &BlockCertifyingService{}
	service.rolacle = rolacle
	service.config = config
	service.hareTerminations = hareTerminations
	service.gossipPublisher = gossipPublisher
	service.blockSigner = blockSigner

	service.signatureCache = sigCache{
		logger:             logger,
		blockSigsByLayer:   sync.Map{},
		cacheBoundary:      types.LayerID{},
		cacheBoundaryMutex: sync.RWMutex{},
		completedCertsCh:   service.completedCertsCh,
	}
	service.completedCertsCh = make(chan BlockCertificate, 100)
	//TODO: empirically decide on adequate channel size

	service.logger = logger
	return service, fmt.Errorf("not yet implemented")
}

func (s *BlockCertifyingService) Start(ctx context.Context) error {
	go blockSigningLoop(ctx, s.hareTerminations,
		s.blockSigner, s.config.CommitteeSize, s.rolacle,
		s.gossipPublisher, &s.signatureCache, s.logger,
	)
	return nil
}

func (s *BlockCertifyingService) SignatureCacher() SigCacher {
	return &s.signatureCache // because exported functions are threadsafe
}

// GossipHandler returns a function that handles blockSignatureMsg gossip.
func (s *BlockCertifyingService) GossipHandler() pubsub.GossipHandler {
	return func(ctx context.Context, peerID p2p.Peer, bytes []byte,
	) pubsub.ValidationResult {
		msg := blockSignatureMsg{}
		err := codec.Decode(bytes, msg)
		if err != nil {
			return pubsub.ValidationReject
		}
		isValid, err := s.rolacle.Validate(ctx,
			msg.layerID, blockCertifierRole, s.config.CommitteeSize,
			msg.signerNodeID, msg.signerRoleProof, msg.signerCommitteeSeats)
		if err != nil {
			log.Error("hare termination gossip: %w", err)
			return pubsub.ValidationReject
		}
		if !isValid {
			return pubsub.ValidationReject
		}

		s.SignatureCacher().CacheBlockSignature(ctx,
			msg.layerID, msg.BlockSignature)
		return pubsub.ValidationAccept
	}
}
