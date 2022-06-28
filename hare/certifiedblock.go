package hare

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// CertifiedBlockService allows a node to participate in the building of certified blocks.
// This includes gossiping block signatures and verifying gossiped block signatures upon
// hare termination of every layer.
type BlockCertifiyingService struct {
}

func NewBlockCertifiyingService() (*BlockCertifiyingService, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (bcs *BlockCertifiyingService) Start(ctx context.Context) error {
	return fmt.Errorf("not yet implemented")
}

type CertifiedBlockStore struct {
}

func (cbs *CertifiedBlockStore) StoreCertifiedBlock(cBlock []byte) error {
	return fmt.Errorf("not yet implemented")
}

// CertifiedBlockProvider is an object designed to retrieve one CertifiedBlock
// from a BlockCertifiyingService.
type CertifiedBlockProvider struct {
}

func NewCertifiedBlockProvider(s BlockCertifiyingService, bID types.BlockID) (*CertifiedBlockProvider, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (cbp *CertifiedBlockProvider) WaitForCertifiedBlock() (<-chan struct{}, error) {
	return nil, fmt.Errorf("not implemented yet")
}

func (cbp *CertifiedBlockProvider) GetCertifiedBlock() {

}

// Implementation details below ----

type terminationSignature struct {
	eligibilityProof [64]byte
	blockSignature   [80]byte
}

type certifiedBlock struct {
	blockID               types.BlockID
	terminationSignatures []terminationSignature
}

func (cbs *CertifiedBlockStore) storeBlockSignature(blockID types.BlockID, sig terminationSignature) error {
	return fmt.Errorf("not yet implemented")
}

// BlockSignatureCollector collects gossiped block signatures and groups them by
// block.
type blockSignatureCollectionService struct {
}
