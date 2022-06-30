package hare

import (
	"context"
	"fmt"

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
}

func NewBlockCertifyingService() (*BlockCertifyingService, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (bcs *BlockCertifyingService) Start(ctx context.Context) error {

	return fmt.Errorf("not yet implemented")
}

// CertifiedBlockProvider is an object designed to retrieve a single CertifiedBlock
// from a BlockCertifyingService.
type CertifiedBlockProvider struct {
}

func NewCertifiedBlockProvider(service *BlockCertifyingService, store *CertifiedBlockStore, bID types.BlockID) (*CertifiedBlockProvider, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// GetBlockCertificateChannel returns a BlockCertificate channel. A
// BlockCertificate can be received once enough block signatures have been
// gathered.
func (cbp *CertifiedBlockProvider) GetBlockCertificateChannel() <-chan *BlockCertificate {
	panic("not implemented")
}

type CertifiedBlockStore struct {
}

func NewCertifiedBlockStore() (*CertifiedBlockStore, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (cbs *CertifiedBlockStore) StoreCertifiedBlock(cBlock []byte) error {
	return fmt.Errorf("not yet implemented")
}

// Implementation details below ----

type terminationSignature struct {
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
