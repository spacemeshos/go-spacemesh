package blockcerts

import (
    "context"
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
    rolacle            hare.Rolacle
    config             certtypes.BlockCertificateConfig
    hareTerminationsCh <-chan hare.TerminationBlockOutput
    gossipPublisher    pubsub.Publisher
    blockSigner        signing.Signer
    signatureCache     sigCache
    completedCertsCh   chan certtypes.BlockCertificate
    db                 sql.Executor
    logger             log.Logger
}

// NewBlockCertifyingService constructs a new BlockCertifyingService.
func NewBlockCertifyingService(
    hareTerminations <-chan hare.TerminationBlockOutput,
    rolacle hare.Rolacle,
    gossipPublisher pubsub.Publisher,
    blockSigner signing.Signer,
    db sql.Executor,
    config certtypes.BlockCertificateConfig,
    logger log.Logger,
) (*BlockCertifyingService, error) {
    service := &BlockCertifyingService{}
    service.rolacle = rolacle
    service.config = config
    service.hareTerminationsCh = hareTerminations
    service.gossipPublisher = gossipPublisher
    service.blockSigner = blockSigner
    service.db = db

    service.signatureCache = sigCache{
        logger:             logger,
        blockSigsByLayer:   sync.Map{},
        cacheBoundary:      types.LayerID{},
        cacheBoundaryMutex: sync.RWMutex{},
        completedCertsCh:   service.completedCertsCh,
    }
    service.completedCertsCh = make(chan certtypes.BlockCertificate, 100)
    // TODO: empirically decide on adequate channel size

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
        msg := certtypes.BlockSignatureMsg{}
        err := codec.Decode(bytes, msg)
        if err != nil {
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
            return pubsub.ValidationReject
        }

        s.SignatureCacher().CacheBlockSignature(ctx, msg)
        return pubsub.ValidationAccept
    }
}
