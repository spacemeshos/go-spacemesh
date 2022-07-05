package hare

import (
	"context"
	"fmt"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type BlockCertificate struct {
	blockID               types.BlockID
	terminationSignatures []terminationSignature
}

func (bCert *BlockCertificate) IsValid() bool {
	panic("not yet implemented")
}

// BlockCertifyingService allows a node to participate in the building of
// certified blocks. This includes verifying & caching gossiped block signatures
// and, if chosen by the oracle, participating in block-signing upon termination
// of a given layer.
type BlockCertifyingService struct {
	rolacle Rolacle
}

// NewBlockCertifyingService constructs a new BlockCertifyingService.
// It expects to receive the same config as Hare.
func NewBlockCertifyingService(rolacle Rolacle, config config.Config) (*BlockCertifyingService, error) {

	service := &BlockCertifyingService{}
	service.rolacle = rolacle

	return service, fmt.Errorf("not yet implemented")
}

func (s *BlockCertifyingService) Start(ctx context.Context) error {

	return fmt.Errorf("not yet implemented")
}

func (s *BlockCertifyingService) GossipHandler() pubsub.GossipHandler {
	return func(ctx context.Context, peerID peer.ID, bytes []byte) pubsub.ValidationResult {
		// Decode message as BlockSignature
		// TODO: Refactor main message logic in hare package to not have "optional fields"
		msg := terminationSignature{}
		err := codec.Decode(bytes, msg)
		if err != nil {
			// TODO: invalidate gossip message
		}

		// Validate with oracle
		// TODO: remove concept of "round" from oracle
		// The issue is that the VRF "buildKey" method requires a "round number" (i.e. k) as input
		// Maybe have separate oracles? Would probably want to run queries to them in separate goroutines anyways.
		s.rolacle.Validate(ctx, msg.LayerID, terminationRound)

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

func NewCertifiedBlockStore(s BlockCertifyingService) (*CertifiedBlockStore, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (cbs *CertifiedBlockStore) StoreCertifiedBlock(cBlock []byte) error {
	return fmt.Errorf("not yet implemented")
}

// Implementation details below ----

type terminationSignature struct {
	types.BlockID
	types.LayerID
	eligibilityProof [64]byte
	blockSignature   [80]byte
}

func (cbs *CertifiedBlockStore) storeBlockSignature(blockID types.BlockID, sig terminationSignature) error {
	return fmt.Errorf("not yet implemented")
}

// blockSignatureCollector collects gossiped block signatures and groups them by
// block.
type blockSignatureCollectionService struct {
}
