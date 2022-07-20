package blockcerts

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type BlockCertificate struct {
	blockID types.BlockID
	layerID types.LayerID

	terminationSignatures []BlockSignature
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

func (cbs *CertifiedBlockStore) storeBlockSignature(blockID types.BlockID, sig BlockSignature) error {
	return fmt.Errorf("not yet implemented")
}

// blockSignatureCollector collects gossiped block signatures and groups them by
// block.
type blockSignatureCollectionService struct {
}
