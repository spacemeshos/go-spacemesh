package blockcert

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type BlockCertificate struct {
	blockID               types.BlockID
	terminationSignatures []terminationSignature
}

type HareTerminationConfig struct {
	CommitteeSize int
}

// BlockCertifyingService is a long-lived service responsible for verifying &
// caching gossiped block signatures and, when chosen by the oracle, participating
// in block-signing upon hare termination.
type BlockCertifyingService struct {
	rolacle        hare.Rolacle
	config         HareTerminationConfig
	hareTerminated <-chan hare.TerminationOutput
	blockSignersWg sync.WaitGroup
}

// NewBlockCertifyingService constructs a new BlockCertifyingService.
func NewBlockCertifyingService(
	rolacle hare.Rolacle,
	config HareTerminationConfig,
	hareTerminations <-chan hare.TerminationOutput,
) (*BlockCertifyingService, error) {
	service := &BlockCertifyingService{}
	service.rolacle = rolacle
	service.config = config
	service.hareTerminated = hareTerminations
	service.blockSignersWg = sync.WaitGroup{}

	return service, fmt.Errorf("not yet implemented")
}

func (s *BlockCertifyingService) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			panic("cancel not yet implemented")
		case output := <-s.hareTerminated:
			s.blockSignersWg.Add(1)
			if !output.Completed() {

			}
		}

	}
}

// GossipHandler returns a function that
func (s *BlockCertifyingService) GossipHandler() pubsub.GossipHandler {
	return func(ctx context.Context, peerID p2p.Peer, bytes []byte,
	) pubsub.ValidationResult {
		// Decode message as BlockSignature
		// TODO: Refactor main message logic in hare package to not have "optional fields"
		msg := terminationSignature{}
		err := codec.Decode(bytes, msg)
		if err != nil {
			return pubsub.ValidationReject
		}
		// Validate with oracle
		// TODO: remove concept of "round" from oracle
		// The issue is that the VRF "buildKey" method requires a "round number" (i.e. k) as input
		validRoleProof, err := s.rolacle.Validate(ctx, msg.LayerID, hare.terminationRound, s.config.CommitteeSize, msg.senderNodeID, msg.eligibilityProof, msg.eligibilityCount)
		if err != nil {
			go log.Error("hare termination gossip: %w", err)
			return pubsub.ValidationReject
		}
		if !validRoleProof {
			return pubsub.ValidationReject
		}

		// Extract public key from signature & validate

		// Store signature
		store, err := NewCertifiedBlockStore(s)
		if err != nil {
			panic("error not yet handled")
		}
		err = store.storeBlockSignature(msg.BlockID, msg)
		if err != nil {
			panic("error not yet handled")
		}
		panic("not yet implemented")
	}
}

// CertifiedBlockProvider is an object designed to retrieve a single CertifiedBlock
// from a BlockCertifyingService.
type CertifiedBlockProvider struct {
}

func NewCertifiedBlockProvider(service *BlockCertifyingService, store *CertifiedBlockStore, bID types.BlockID) (*CertifiedBlockProvider, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// RegisterBlockCertificateChannel registers a BlockCertificate channel. A
// BlockCertificate can be received once enough block signatures have been
// gathered.
func (cbp *CertifiedBlockProvider) RegisterBlockCertificateChannel(certChannel chan<- *BlockCertificate) {
	defer close(certChannel)
	return
}

type CertifiedBlockStore struct {
}

func NewCertifiedBlockStore(s *BlockCertifyingService) (*CertifiedBlockStore, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (cbs *CertifiedBlockStore) StoreCertifiedBlock(cBlock []byte) error {
	return fmt.Errorf("not yet implemented")
}

// Implementation details below ----

type terminationSignature struct {
	types.BlockID
	types.LayerID
	senderNodeID     types.NodeID
	eligibilityProof []byte
	eligibilityCount uint16
	blockSignature   [80]byte
}

func (cbs *CertifiedBlockStore) storeBlockSignature(blockID types.BlockID, sig terminationSignature) error {
	return fmt.Errorf("not yet implemented")
}

// blockSignatureCollector collects gossiped block signatures and groups them by
// block.
type blockSignatureCollectionService struct {
}
