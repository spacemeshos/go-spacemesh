package blockcerts

import (
    "context"
    "github.com/spacemeshos/go-spacemesh/blockcerts/config"
    certtypes "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/codec"
    "github.com/spacemeshos/go-spacemesh/common/types"
    "github.com/spacemeshos/go-spacemesh/hare"
    "github.com/spacemeshos/go-spacemesh/log"
    "github.com/spacemeshos/go-spacemesh/p2p"
    "github.com/spacemeshos/go-spacemesh/p2p/pubsub"
    "github.com/spacemeshos/go-spacemesh/signing"
    "github.com/spacemeshos/go-spacemesh/sql"
    "math"
    "sync"
)

const (
    blockCertifierRole uint32 = math.MaxUint32 - 2        // for Rolacle
    BlockSigTopic             = "trackableBlockSignature" // for gossip

)

// BlockCertifyingService is a long-lived service responsible for participating
// in block-signing upon hare termination.
// Inputs: hareTerminationsCh and GossipHandler.
// Outputs: gossipPublisher and database
type BlockCertifyingService struct {
    // Inputs & Outputs
    hareTerminationsCh <-chan hare.TerminationBlockOutput
    gossipPublisher    pubsub.Publisher
    db                 sql.Executor
    // Dependencies
    rolacle     hare.Rolacle
    config      config.BlockCertificateConfig
    blockSigner signing.Signer
    // Internal
    signatureCache   sigCache
    completedCertsCh chan certtypes.BlockCertificate

    logger log.Logger
}

// NewBlockCertifyingService constructs a new BlockCertifyingService.
func NewBlockCertifyingService(
    hareTerminations <-chan hare.TerminationBlockOutput,
    rolacle hare.Rolacle,
    gossipPublisher pubsub.Publisher,
    blockSigner signing.Signer,
    db sql.Executor,
    config config.BlockCertificateConfig,
    logger log.Logger,
) (*BlockCertifyingService, error) {
    service := &BlockCertifyingService{}
    service.rolacle = rolacle
    service.config = config
    service.hareTerminationsCh = hareTerminations
    service.gossipPublisher = gossipPublisher
    service.blockSigner = blockSigner
    service.db = db
    service.completedCertsCh = make(chan certtypes.BlockCertificate, config.Hdist)

    service.signatureCache = sigCache{
        logger:             logger,
        blockSigsByLayer:   sync.Map{},
        cacheBoundary:      types.NewLayerID(0), // TODO: check this is okay
        cacheBoundaryMutex: sync.RWMutex{},
        signaturesRequired: config.MaxAdversaries + 1,
        completedCertsCh:   service.completedCertsCh,
    }

    service.logger = logger
    return service, nil
}

func (s *BlockCertifyingService) Start(ctx context.Context) error {
    go blockSigningLoop(ctx, s.hareTerminationsCh,
        s.blockSigner, s.config.CommitteeSize, s.rolacle,
        s.gossipPublisher, &s.signatureCache, s.logger,
    )
    go certificateStoringLoop(ctx, s.completedCertsCh, s.db, s.logger)
    return nil
}

func (s *BlockCertifyingService) SignatureCacher() SigCacher {
    return &s.signatureCache // exported functions are thread safe
}

// GossipHandler returns a function that handles incoming BlockSignatureMsg gossip.
func (s *BlockCertifyingService) GossipHandler() pubsub.GossipHandler {
    return func(ctx context.Context, peerID p2p.Peer, bytes []byte,
    ) pubsub.ValidationResult {
        logger := s.logger.WithContext(ctx)
        msg := certtypes.BlockSignatureMsg{}
        err := codec.Decode(bytes, &msg)
        if err != nil {
            logger.Debug("certified block: gossip handler: failed to "+
                "decode message from peer: %s", peerID)
            return pubsub.ValidationReject
        }
        isValid, err := s.rolacle.Validate(ctx,
            msg.LayerID, blockCertifierRole, s.config.CommitteeSize,
            msg.SignerNodeID, msg.SignerRoleProof, msg.SignerCommitteeSeats)
        if err != nil {
            log.Error("hare termination gossip: %w", err)
            return pubsub.ValidationReject
        }
        if !isValid {
            logger.With().Debug("certified block: gossip hander: received "+
                "invalid block signature", msg.BlockID, msg.LayerID, msg.SignerNodeID)
            return pubsub.ValidationReject
        }

        s.SignatureCacher().CacheBlockSignature(ctx, msg)
        return pubsub.ValidationAccept
    }
}
